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
	"time"

	"github.com/golang/glog"
	"github.com/kubernetes-incubator/external-storage/lib/controller"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/resource"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/types"
	"k8s.io/client-go/pkg/util/uuid"
	"k8s.io/client-go/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/avagin/ploop-flexvol/volume"
)

const (
	resyncPeriod              = 15 * time.Second
	provisionerName           = "kubernetes.io/virtuozzo-storage"
	exponentialBackOffOnError = false
	failedRetryThreshold      = 5
	provisionerIDAnn          = "vzFSProvisionerIdentity"
	vzShareAnn                = "vzShare"
)

type provisionOutput struct {
	Path string `json:"path"`
}

type vzFSProvisioner struct {
	// Kubernetes Client. Use to retrieve Ceph admin secret
	client kubernetes.Interface
	// Identity of this vzFSProvisioner, generated. Used to identify "this"
	// provisioner's PVs.
	identity types.UID
}

func newVzFSProvisioner(client kubernetes.Interface) controller.Provisioner {
	return &vzFSProvisioner{
		client:   client,
		identity: uuid.NewUUID(),
	}
}

var _ controller.Provisioner = &vzFSProvisioner{}

// Provision creates a storage asset and returns a PV object representing it.
func (p *vzFSProvisioner) Provision(options controller.VolumeOptions) (*v1.PersistentVolume, error) {
	var (
		capacity resource.Quantity
		labels   map[string]string
	)
	volumePath, err := p.parseParameters(options.Parameters)
	if err != nil {
		return nil, err
	}

	capacity = options.PVC.Spec.Resources.Requests[v1.ResourceName(v1.ResourceStorage)]
	bytes := capacity.Value()

	if options.PVC.Spec.Selector != nil && options.PVC.Spec.Selector.MatchExpressions != nil {
		return nil, fmt.Errorf("claim Selector.matchExpressions is not supported")
	}
	share := fmt.Sprintf("kubernetes-dynamic-pvc-%s", uuid.NewUUID())

	glog.Infof("Add %s %s %s", volumePath, share, capacity.Value())

	if options.PVC.Spec.Selector != nil && options.PVC.Spec.Selector.MatchLabels != nil {
		labels = options.PVC.Spec.Selector.MatchLabels
	}

	ploop_options := map[string]string{
		"volumePath": volumePath,
		"volumeId":   share,
		"size":       fmt.Sprintf("%d", bytes),
	}

	if labels != nil {
		for k, v := range labels {
			switch k {
			case "vzsReplicas":
				fallthrough
			case "vzsTier":
				ploop_options[k] = v
			default:
				glog.Infof("Skip %s = %s", k, v)
			}
		}
	}

	if err := volume.Create(ploop_options); err != nil {
		return nil, err
	}

	pv := &v1.PersistentVolume{
		ObjectMeta: v1.ObjectMeta{
			Name: options.PVName,
			Annotations: map[string]string{
				provisionerIDAnn: string(p.identity),
				vzShareAnn:       share,
			},
			Labels: labels,
		},
		Spec: v1.PersistentVolumeSpec{
			PersistentVolumeReclaimPolicy: options.PersistentVolumeReclaimPolicy,
			AccessModes:                   options.PVC.Spec.AccessModes,
			Capacity: v1.ResourceList{
				v1.ResourceName(v1.ResourceStorage): options.PVC.Spec.Resources.Requests[v1.ResourceName(v1.ResourceStorage)],
			},
			PersistentVolumeSource: v1.PersistentVolumeSource{
				FlexVolume: &v1.FlexVolumeSource{
					Driver:  "jaxxstorm/ploop",
					Options: ploop_options,
				},
			},
		},
	}

	glog.Infof("successfully created virtuozzo storage share: %s", share)

	return pv, nil
}

func (p *vzFSProvisioner) parseParameters(parameters map[string]string) (string, error) {
	var (
		volumePath string
	)

	for k, v := range parameters {
		switch k {
		case "volumePath":
			volumePath = v
		default:
			return "", fmt.Errorf("invalid option %q", k)
		}
	}

	if volumePath == "" {
		return "", fmt.Errorf("missing volumePath")
	}

	return volumePath, nil
}

// Delete removes the storage asset that was created by Provision represented
// by the given PV.
func (p *vzFSProvisioner) Delete(volume *v1.PersistentVolume) error {
	ann, ok := volume.Annotations[provisionerIDAnn]
	if !ok {
		return errors.New("identity annotation not found on PV")
	}
	if ann != string(p.identity) {
		return &controller.IgnoredError{"identity annotation on PV does not match ours"}
	}
	share, ok := volume.Annotations[vzShareAnn]
	if !ok {
		return errors.New("vz share annotation not found on PV")
	}

	options := volume.Spec.PersistentVolumeSource.FlexVolume.Options
	path := options["volumePath"] + "/" + options["volumeId"]
	glog.Infof("Delete: %s", path)
	err := os.RemoveAll(path)
	if err != nil {
		return err
	}

	glog.Infof("successfully delete virtuozzo storage share: %s", share)

	return nil
}

var (
	master     = flag.String("master", "", "Master URL")
	kubeconfig = flag.String("kubeconfig", "", "Absolute path to the kubeconfig")
)

func main() {
	flag.Parse()
	flag.Set("logtostderr", "true")

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

	// Start the provision controller which will dynamically provision cephFS
	// PVs
	pc := controller.NewProvisionController(clientset, resyncPeriod, provisionerName, vzFSProvisioner, serverVersion.GitVersion, exponentialBackOffOnError, failedRetryThreshold, 2*resyncPeriod, resyncPeriod, resyncPeriod/2, 2*resyncPeriod)

	pc.Run(wait.NeverStop)
}
