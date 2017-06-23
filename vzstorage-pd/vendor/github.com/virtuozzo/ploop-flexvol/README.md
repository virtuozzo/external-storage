# ploop-flexvol

A [FlexVolume](https://github.com/kubernetes/kubernetes/blob/master/examples/volumes/flexvolume/README.md) driver for kubernetes which allows you to mount [Ploop](https://openvz.org/Man/ploop.8) volumes to your kubernetes pods.

## Status

Kubernetes FlexVolumes are currently in Alpha state, so this plugin is as well. Use it at your own risk.

## Using

### Build

```
mkdir -p src/github.com/virtuozzo
ln -s ../../../ src/github.com/virtuozzo/ploop-flexvol
export GOPATH=`pwd`
cd src/github.com/virtuozzo/ploop-flexvol
make
```

### Installing

In order to use the flexvolume driver, you'll need to install it on every node you want to use ploop on in the kubelet `volume-plugin-dir`. By default this is `/usr/libexec/kubernetes/kubelet-plugins/volume/exec/`

You need a directory for the volume driver vendor, so create it:

```
mkdir -p /usr/libexec/kubernetes/kubelet-plugins/volume/exec/virtuozzo~ploop
```

Then drop the binary in there:

```
mv ploop /usr/libexec/kubernetes/kubelet-plugins/volume/exec/virtuozzo~ploop/ploop
```

You can now use ploops as usual!

### Pod Config

An example pod config would look like this:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: nginx-ploop
spec:
  containers:
  - name: nginx
    image: nginx
    volumeMounts:
    - name: test
      mountPath: /data
    ports:
    - containerPort: 80
  nodeSelector:
    os: parallels # make sure you label your nodes to be ploop compatible 
  volumes:
  - name: test
    flexVolume:
      driver: "virtuozzo/ploop" # this must match your vendor dir
      options:
        volumeId: "golang-ploop-test"
        size: "10G"
        volumePath: "/vstorage/storage_pool/kubernetes"
```

This will mount a block device from a ploop image located at `/vstorage/storage_pool/kubernetes/golang-ploop-test` directory.

You can verify the ploop volume was mounted by finding the node where your pod was scheduled by running `ploop list`:

```
# ploop list
ploop18115  /vstorage/storage_pool/kubernetes/golang-ploop-test/golang-ploop-test
```

#### Options
* **volumePath**

  a path to a virtuozzo storage directory where ploop image is located
* **volumeId**

   an unique name for a ploop image
* **size**=[0-9]*[KMG]

   size of the volume

* **vzsReplicas**=normal[:min]|/X

     Replication level specification:

     _normal_   The number of replicas to maintain.

     _minimum_  The minimum number of replicas required to write a file (optional).

     _/X_       Write tolerance (normal-minimum). The number of replicas allowed to go offline
                 provided that the client is still allowed to write the file.

     The number of replicas must be in the range 1-64.

* **vzsEencoding**=M+N[/X]

     Encoding specification:

     _M_   The stripe-depth.

     _N_   The number of parity blocks.

     _X_   The write tolerance. The number of replicas allowed to go offline
                 provided that the client is still allowed to write the file.

* **vzsFailureDomain**=disk|host|rack|row|room

    Failure domain for file replicas.

    This parameter controls how replicas are distributed across CSs in the cluster:

    _disk_ - place no more then 1 replica per disk/CS

    _host_ - place no more then 1 replica per host (default)

    _rack_ - place no more then 1 replica per RACK

    _row_  - place no more then 1 replica per ROW

    _room_ - place no more then 1 replica per ROOM

* **vzsTier**=0-3

     Storage tier for file replicas.

### Logging

By default, ploop-flexvol redirects all logging data to the systemd-journald
service. If you want to use another way to collect logging data, you can create
a wrapper script. It has to redirect stdout to the 3 descriptor and execute the
plugin binary according with the following rules:

```
./ploop wrapper [glog flags] -- ploop [plugin options]
```

Here is an example to save logging data into a file:
```
#!/bin/sh

exec 3>&1

`dirname $0`/ploop.bin wrapper -logtostderr -- ploop "$@" &>> /var/log/ploop-flexvol.log

```
