# Virtuozzo Volume Provisioner for Kubernetes 1.5+

Using Virtuozzo Storage and Ploop devices

# Test instruction

* Build the ploop-flexvol driver and copy binary file to virtuozzo-storage provisioner directory

https://github.com/avagin/ploop-flexvol

* Build virtuozzo-provisioner and container image

```bash
make
docker build -t virtuozzo-storage .
```

* Start Kubernetes local cluster

* Start Virtuozzo provisioner

Assume kubeconfig is at `/root/.kube` and vstorage mounted on all cluster nodes in /mnt/vstorage:

```bash
docker run -tid -v /mnt/vstorage/kube/:/mnt/vstorage/kube/ -v /root/.kube:/kube --privileged --net=host virtuozzo-storage
```

* Create a Virtuozzo Storage Class

```bash
kubectl create -f class.yaml
```

* Create a claim

```bash
kubectl create -f claim.yaml
```

* Create a Pod using the claim

```bash
kubectl create -f test-pod.yaml
```

# Ploop options

A storage class parameters pass as ploop options to the ploop-flexvol driver.

# Known limitations
Vstorage must be mounted manually on all cluster nodes
