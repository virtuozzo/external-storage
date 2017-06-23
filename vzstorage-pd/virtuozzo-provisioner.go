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
	"time"

	"github.com/golang/glog"
	"github.com/kubernetes-incubator/external-storage/lib/controller"
	"github.com/kubernetes-incubator/external-storage/lib/leaderelection"
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
	resyncPeriod              = 15 * time.Second
	provisionerName           = "kubernetes.io/virtuozzo-storage"
	exponentialBackOffOnError = false
	failedRetryThreshold      = 5
	parentProvisionerAnn      = "vzFSParentProvisioner"
	vzShareAnn                = "vzShare"
	leasePeriod               = leaderelection.DefaultLeaseDuration
	retryPeriod               = leaderelection.DefaultRetryPeriod
	renewDeadline             = leaderelection.DefaultRenewDeadline
	termLimit                 = leaderelection.DefaultTermLimit
)

type provisionOutput struct {
	Path string `json:"path"`
}

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

const ProvisionerDir = "/export/virtuozzo-provisioner/"
const MountDir = ProvisionerDir + "mnt/"

func prepareVstorage(options map[string]string, clusterName string, clusterPassword string) error {
	mount := MountDir + clusterName
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
		volumePath, volumeId, size string
	)

	for k, v := range options {
		switch k {
		case "volumePath":
			volumePath = v
		case "volumeId":
			volumeId = v
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

	if volumeId == "" {
		return fmt.Errorf("volumeId isn't specified")
	}

	if size == "" {
		return fmt.Errorf("size isn't specified")
	}

	// get a human readable size from the map
	bytes, _ := humanize.ParseBytes(size)

	// ploop driver takes kilobytes, so convert it
	volume_size := bytes / 1024

	ploop_path := mount + "/" + options["volumePath"] + "/" + options["volumeId"]

	// make the base directory where the volume will go
	err := os.MkdirAll(ploop_path, 0700)
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
			args := []string{"set-attr", "-R", ploop_path,
				fmt.Sprintf("%s=%s", attr, v)}
			err = exec.Command(cmd, args...).Run()
		}

		if err != nil {
			os.RemoveAll(ploop_path)
			return fmt.Errorf("Unable to set %s to %s: %v", attr, v, err)
		}
	}

	// Create the ploop volume
	cp := ploop.CreateParam{Size: volume_size, File: ploop_path + "/" + options["volumeId"]}
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

	storage_class_options := map[string]string{}
	for k, v := range options.Parameters {
		storage_class_options[k] = v
	}

	storage_class_options["volumeId"] = share
	storage_class_options["size"] = fmt.Sprintf("%d", bytes)
	secretName := storage_class_options["secretName"]
	delete(storage_class_options, "secretName")

	secret, err := p.client.Core().Secrets(options.PVC.Namespace).Get(secretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	name := string(secret.Data["clusterName"][:len(secret.Data["clusterName"])])
	password := string(secret.Data["clusterPassword"][:len(secret.Data["clusterPassword"])])
	if err := prepareVstorage(storage_class_options, name, password); err != nil {
		return nil, err
	}
	defer syscall.Unmount(MountDir+name, syscall.MNT_DETACH)

	if err := createPloop(MountDir+name, storage_class_options); err != nil {
		return nil, err
	}

	pv := &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: options.PVName,
			Annotations: map[string]string{
				parentProvisionerAnn: *provisionerId,
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
					Options:   storage_class_options,
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
	if ann != *provisionerId {
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
	mount := MountDir + name
	if err := prepareVstorage(options, name, password); err != nil {
		return err
	}
	defer syscall.Unmount(mount, syscall.MNT_DETACH)

	path := mount + "/" + options["volumePath"] + "/" + options["volumeId"]
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
	provisionerId = flag.String("name", "", "Unique provisioner name")
)

func main() {
	flag.Parse()
	flag.Set("logtostderr", "true")
	if *provisionerId == "" {
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
