# Virtuozzo Volume Provisioner for Kubernetes 1.5+

Using Virtuozzo Storage and Ploop devices

# Test instruction

* Build virtuozzo-provisioner

```bash
make
```

* Build and install the ploop-flexvol driver

https://github.com/avagin/ploop-flexvol

* Start Kubernetes local cluster

* Start Virtuozzo provisioner

Assume kubeconfig is at `/root/.kube`:

```bash
./virtuozzo-storage -master=http://127.0.0.1:8080
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


# Known limitations
