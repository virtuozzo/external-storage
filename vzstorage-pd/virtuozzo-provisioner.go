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
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/golang/glog"
	"github.com/kubernetes-incubator/external-storage/lib/controller"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/dustin/go-humanize"
	"github.com/kolyshkin/goploop-cli"
	"github.com/virtuozzo/ploop-flexvol/vstorage"
)

const (
	provisionerName      = "virtuozzo.com/virtuozzo-storage"
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
		volumePath, volumeID, size string
	)

	for k, v := range options {
		switch k {
		case "volumePath":
			volumePath = v
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

	ploopPath := mount + "/" + options["volumePath"] + "/" + options["volumeID"]

	// make the base directory where the volume will go
	err := os.MkdirAll(ploopPath, 0700)
	if err != nil {
		return err
	}

	for k, v := range options {
		var err error
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

	// Create the ploop volume
	cp := ploop.CreateParam{Size: volumeSize, File: ploopPath + "/" + options["volumeID"]}
	if err := ploop.Create(&cp); err != nil {
		return err
	}

	return nil
}

// Provision creates a storage asset and returns a PV object representing it.
func (p *vzFSProvisioner) Provision(options controller.VolumeOptions) (*v1.PersistentVolume, error) {
	capacity := options.PVC.Spec.Resources.Requests[v1.ResourceName(v1.ResourceStorage)]
	bytes := capacity.Value()

	if options.PVC.Spec.Selector != nil {
		return nil, fmt.Errorf("claim Selector is not supported")
	}
	share := fmt.Sprintf("kubernetes-dynamic-pvc-%s", uuid.NewUUID())

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
	defer syscall.Unmount(mountDir+name, syscall.MNT_DETACH)

	if err := createPloop(mountDir+name, storageClassOptions); err != nil {
		return nil, err
	}

	storageClassOptions["clusterName"] = name
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
			AccessModes:                   options.PVC.Spec.AccessModes,
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
	defer syscall.Unmount(mount, syscall.MNT_DETACH)

	path := mount + "/" + options["volumePath"] + "/" + options["volumeID"]
	glog.Infof("Delete: %s", path)
	err = os.RemoveAll(path)
	if err != nil {
		return err
	}

	glog.Infof("successfully delete virtuozzo storage share: %s", share)

	return nil
}

var (
	master        = flag.String("master", "", "Master URL")
	kubeconfig    = flag.String("kubeconfig", "", "Absolute path to the kubeconfig")
	provisionerID = flag.String("name", "", "Unique provisioner name")
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
		provisionerName,
		vzFSProvisioner,
		serverVersion.GitVersion,
	)

	pc.Run(wait.NeverStop)
}
