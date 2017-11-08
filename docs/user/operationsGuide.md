# Operations Guide

## Install the Couchbase Operator

OPTIONAL: If you are using a private registry, make sure to deploy the registrykey secret first.  Create the 'registrykey' secret:

```bash
$ kubectl apply -f example/registrykeysecret.yaml
```

Create deployment: In order to run a couchbase cluster you'll first need to deploy a custom resource definition
which will allow for the creation of a 'couchbasecluster' type within the kubernetes cluster.

```bash
$ kubectl create -f example/deployment.yaml
```

Create the 'cb-example-auth' secret

```bash
$ kubectl apply -f example/secret.yaml
```

Now objects with "kind: CouchbaseCluster" can be created.  Create a cluster:

```bash
$ kubectl apply -f example/couchbase-cluster.yaml
```

couchbase operator will automatically create a Kubernetes Custom Resource Definition (CRD):

```bash
$ kubectl get couchbasecluster
NAME         KIND
cb-example   CouchbaseCluster.v1beta1.couchbase.database.couchbase.com
```
