# Deleting a CouchbaseCluster

You can delete a cluster either by using the cluster configuration file that you created the cluster with, or by deleting the cluster directly.

## Deleting a Cluster Using the Cluster Configuration File

To delete a cluster using the cluster configuration file, say `my-cluster.yaml`, run the following command:

On Kubernetes:

```bash
kubectl delete -f my-cluster.yaml
```

On OpenShift:

```bash
oc delete -f my-cluster.yaml
```

## Deleting a Cluster Directly

To delete a cluster directly, say `my-cluster`, run the following command:

On Kubernetes:

```bash
kubectl delete couchbasecluster my-cluster
```

On OpenShift:

```bash
oc delete couchbasecluster my-cluster
```
