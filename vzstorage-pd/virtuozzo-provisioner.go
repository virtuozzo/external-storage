/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"syscall"

	"github.com/golang/glog"
	"github.com/kubernetes-incubator/external-storage/lib/controller"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/dustin/go-humanize"
	"github.com/virtuozzo/goploop-cli"
	"github.com/virtuozzo/ploop-flexvol/vstorage"
)

const (
	parentProvisionerAnn = "vzFSParentProvisioner"
	vzShareAnn           = "vzShare"
)

type vzFSProvisioner struct {
	// Kubernetes Client. Use to retrieve secrets with Virtuozzo Storage credentials
	client kubernetes.Interface
}

func newVzFSProvisioner(client kubernetes.Interface) controller.Provisioner {
	return &vzFSProvisioner{
		client: client,
	}
}

var _ controller.Provisioner = &vzFSProvisioner{}

const provisionerDir = "/export/virtuozzo-provisioner/"
const mountDir = provisionerDir + "mnt/"

func prepareVstorage(options map[string]string, clusterName string, clusterPassword string) error {
	mount := mountDir + clusterName
	mounted, _ := vstorage.IsVstorage(mount)
	if mounted {
		return nil
	}

	if err := os.MkdirAll(mount, 0755); err != nil {
		return err
	}

	v := vstorage.Vstorage{clusterName}
	p, _ := v.Mountpoint()
	if p != "" {
		return syscall.Mount(p, mount, "", syscall.MS_BIND, "")
	}

	if err := v.Auth(clusterPassword); err != nil {
		return err
	}
	if err := v.Mount(mount); err != nil {
		return err
	}

	return nil
}

func createPloop(mount string, options map[string]string) error {
	var (
		volumePath, deltasPath, volumeID, size string
	)

	for k, v := range options {
		switch k {
		case "volumePath":
			volumePath = v
		case "deltasPath":
			deltasPath = v
		case "volumeID":
			volumeID = v
		case "size":
			size = v
		case "vzsReplicas":
		case "vzsFailureDomain":
		case "vzsEncoding":
		case "vzsTier":
		case "kubernetes.io/readwrite":
		case "kubernetes.io/fsType":
		default:
		}
	}

	if volumePath == "" {
		return fmt.Errorf("volumePath isn't specified")
	}

	if deltasPath == "" {
		deltasPath = volumePath
	}

	if volumeID == "" {
		return fmt.Errorf("volumeID isn't specified")
	}

	if size == "" {
		return fmt.Errorf("size isn't specified")
	}

	// get a human readable size from the map
	bytes, _ := humanize.ParseBytes(size)

	// ploop driver takes kilobytes, so convert it
	volumeSize := bytes / 1024

	// create ploop deltas path
	if err := os.MkdirAll(path.Join(mount, deltasPath), 0755); err != nil {
		return err
	}

	ploopPath := path.Join(mount, volumePath, options["volumeID"])
	// add .image suffix to handle case when deltasPath == volumePath
	deltaPath := path.Join(mount, deltasPath, options["volumeID"] + ".image")
	// Create the ploop volume
	_, err := ploop.PloopVolumeCreate(ploopPath, volumeSize, deltaPath)
	if err != nil {
		return err
	}

	for k, v := range options {
		attr := ""
		switch k {
		case "vzsReplicas":
			attr = "replicas"
		case "vzsTier":
			attr = "tier"
		case "vzsEncoding":
			attr = "encoding"
		case "vzsFailureDomain":
			attr = "failure-domain"
		}
		if attr != "" {
			cmd := "vstorage"
			args := []string{"set-attr", "-R", ploopPath,
				fmt.Sprintf("%s=%s", attr, v)}
			err = exec.Command(cmd, args...).Run()
		}

		if err != nil {
			os.RemoveAll(ploopPath)
			return fmt.Errorf("Unable to set %s to %s: %v", attr, v, err)
		}
	}

	return nil
}

func (p *vzFSProvisioner) patchSecret(oldSecret, newSecret *v1.Secret) error {
	oldData, err := json.Marshal(oldSecret)
	if err != nil {
		glog.Errorf("failed to marshal secret %s: %v", newSecret.Name, err)
		return err
	}
	newData, err := json.Marshal(newSecret)
	if err != nil {
		glog.Errorf("failed to marshal secret patch %s: %v", newSecret.Name, err)
		return err
	}

	patchBytes, err := strategicpatch.CreateTwoWayMergePatch(oldData, newData, v1.Secret{})
	if err != nil {
		glog.Errorf("failed to create patch for secret %s: %v", newSecret.Name, err)
		return err
	}
	glog.Infof("Secret %s patch: %s", newSecret.Name, string(patchBytes))

	_, err = p.client.Core().Secrets(newSecret.ObjectMeta.Namespace).Patch(newSecret.Name, types.StrategicMergePatchType, patchBytes)
	return err
}

func removePloop(mount string, options map[string]string) error {
	ploopPath := path.Join(mount, options["volumePath"], options["volumeID"])
	vol, err := ploop.PloopVolumeOpen(ploopPath)
	if err != nil {
		return err
	}
	glog.Infof("Delete: %s", ploopPath)
	return vol.Delete()
}

// Provision creates a storage asset and returns a PV object representing it.
func (p *vzFSProvisioner) Provision(options controller.VolumeOptions) (*v1.PersistentVolume, error) {
	modes := options.PVC.Spec.AccessModes
	if len(modes) == 0 {
		// if AccessModes field is absent, ReadWriteOnce is used by default
		modes = append(modes, v1.ReadWriteOnce)
	} else {
		if len(modes) != 1 || modes[0] != v1.ReadWriteOnce {
			return nil, fmt.Errorf("Virtuozzo flexvolume provisioner supports only ReadWriteOnce access mode")
		}
	}
	capacity := options.PVC.Spec.Resources.Requests[v1.ResourceName(v1.ResourceStorage)]
	bytes := capacity.Value()

	if options.PVC.Spec.Selector != nil {
		return nil, fmt.Errorf("claim Selector is not supported")
	}
	share := fmt.Sprintf("kubernetes-dynamic-pvc-%s", options.PVC.UID)

	glog.Infof("Add %s %s", share, humanize.Bytes(uint64(bytes)))

	storageClassOptions := map[string]string{}
	for k, v := range options.Parameters {
		storageClassOptions[k] = v
	}

	storageClassOptions["volumeID"] = share
	storageClassOptions["size"] = fmt.Sprintf("%d", bytes)
	secretName := storageClassOptions["secretName"]
	delete(storageClassOptions, "secretName")

	secret, err := p.client.Core().Secrets(options.PVC.Namespace).Get(secretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	name := string(secret.Data["clusterName"][:len(secret.Data["clusterName"])])
	password := string(secret.Data["clusterPassword"][:len(secret.Data["clusterPassword"])])
	if err := prepareVstorage(storageClassOptions, name, password); err != nil {
		return nil, err
	}

	if err := createPloop(mountDir+name, storageClassOptions); err != nil {
		return nil, err
	}

	finalizer := fmt.Sprintf("virtuozzo.com/%s-pv", uuid.NewUUID())
	storageClassOptions["clusterName"] = name
	storageClassOptions["finalizer"] = finalizer
	pv := &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: options.PVName,
			Annotations: map[string]string{
				parentProvisionerAnn: *provisionerID,
				vzShareAnn:           share,
			},
		},
		Spec: v1.PersistentVolumeSpec{
			PersistentVolumeReclaimPolicy: options.PersistentVolumeReclaimPolicy,
			AccessModes:                   modes,
			Capacity: v1.ResourceList{
				v1.ResourceName(v1.ResourceStorage): options.PVC.Spec.Resources.Requests[v1.ResourceName(v1.ResourceStorage)],
			},
			PersistentVolumeSource: v1.PersistentVolumeSource{
				FlexVolume: &v1.FlexVolumeSource{
					Driver:    "virtuozzo/ploop",
					SecretRef: &v1.LocalObjectReference{Name: secretName},
					Options:   storageClassOptions,
				},
			},
		},
	}

	newSecret := *secret
	idx := -1
	for i, f := range newSecret.Finalizers {
		if f == finalizer {
			idx = i
			break
		}
	}
	if idx == -1 {
		newSecret.Finalizers = append(newSecret.Finalizers, finalizer)
		if err = p.patchSecret(secret, &newSecret); err != nil {
			glog.Errorf("Failed to update finalizers in secret: %s", secretName)
			if e := removePloop(mountDir+name, storageClassOptions); e != nil {
				err = fmt.Errorf("Add finalizer error: %v; cleanup ploop-volume error: %v", err, e)
			}
			return nil, err
		}
	}
	glog.Infof("successfully created virtuozzo storage share: %s", share)
	return pv, nil
}

// Delete removes the storage asset that was created by Provision represented
// by the given PV.
func (p *vzFSProvisioner) Delete(volume *v1.PersistentVolume) error {
	ann, ok := volume.Annotations[parentProvisionerAnn]
	if !ok {
		return errors.New("Parent provisioner name annotation not found on PV")
	}
	if ann != *provisionerID {
		return &controller.IgnoredError{"parent provisioner name annotation on PV does not match ours"}
	}
	share, ok := volume.Annotations[vzShareAnn]
	if !ok {
		return errors.New("vz share annotation not found on PV")
	}

	secretName := volume.Spec.PersistentVolumeSource.FlexVolume.SecretRef.Name
	options := volume.Spec.PersistentVolumeSource.FlexVolume.Options

	secret, err := p.client.Core().Secrets(volume.Spec.ClaimRef.Namespace).Get(secretName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	name := string(secret.Data["clusterName"][:len(secret.Data["clusterName"])])
	password := string(secret.Data["clusterPassword"][:len(secret.Data["clusterPassword"])])
	mount := mountDir + name
	if err := prepareVstorage(options, name, password); err != nil {
		return err
	}

	if err = removePloop(mount, options); err != nil {
		return err
	}

	defer glog.Infof("successfully delete virtuozzo storage share: %s", share)

	newSecret := *secret
	finalizer, ok := options["finalizer"]
	if !ok {
		glog.Warningf("Unable to find finalizer in flexvolume %s options", volume.Name)
		return nil
	}
	idx := -1
	for i, f := range newSecret.Finalizers {
		if f == finalizer {
			idx = i
			break
		}
	}
	if idx == -1 {
		glog.Warningf("Cannot find finalizer %s in secret %s: %v", finalizer, secretName)
		return nil
	}

	newSecret.Finalizers = append(newSecret.Finalizers[:idx], newSecret.Finalizers[idx+1:]...)
	if err = p.patchSecret(secret, &newSecret); err != nil {
		glog.Warningf("Failed to update finalizers in secret %s: %v", secretName, err)
	}

	return nil
}

var (
	master          = flag.String("master", "", "Master URL")
	kubeconfig      = flag.String("kubeconfig", "", "Absolute path to the kubeconfig")
	provisionerID   = flag.String("id", "", "Unique provisioner id")
	provisionerName = flag.String("name", "virtuozzo.com/virtuozzo-storage", "Unique provisioner name")
)

func main() {
	flag.Parse()
	flag.Set("logtostderr", "true")
	if *provisionerID == "" {
		glog.Fatalf("You should provide unique provisioner name!")
	}

	var config *rest.Config
	var err error
	if *master != "" || *kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags(*master, *kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}

	if err != nil {
		glog.Fatalf("Failed to create config: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Failed to create client: %v", err)
	}

	// The controller needs to know what the server version is because out-of-tree
	// provisioners aren't officially supported until 1.5
	serverVersion, err := clientset.Discovery().ServerVersion()
	if err != nil {
		glog.Fatalf("Error getting server version: %v", err)
	}

	// Create the provisioner: it implements the Provisioner interface expected by
	// the controller
	vzFSProvisioner := newVzFSProvisioner(clientset)

	// Start the provision controller which will dynamically provision Virtuozzo Storage PVs
	pc := controller.NewProvisionController(clientset,
		*provisionerName,
		vzFSProvisioner,
		serverVersion.GitVersion,
	)

	pc.Run(wait.NeverStop)
}
