## Kubernetes RBAC

The Couchbase Operator needs some special permissions in order to interact with the Kubernetes master. These permissions need to be set for each namespace that the Couchbase Operator is used in. Note in the example below the namespace is "default".

First download the RBAC role creation files [here](https://s3.amazonaws.com/packages.couchbase.com/kubernetes/beta/rbac.zip) and unzip them. Then run the command below.

```bash
$ ./create_roles.sh --namespace=default
```

This script will create a cluster role and a cluster role binding that will allow the Couchbase Operator to properly start up. It turns out that the Operator needs some non-standard permissions to create CRD's and watch for events on CouchbaseCluster objects and this allows us to do that.

> *Note:* If you are using MiniKube then you don't need to set RBAC rules because RBAC is disabled by default.