# Deploying Couchbase

Deploying a Couchbase cluster requires creating a configuration file that describes what the cluster should look like. Like all Kubernetes configurations Couchbase clusters are defined using either YAML or JSON (YAML is preferred by Kubernetes) and then pushed into Kubernetes. Below is the definition of a sample definition of a Couchbase cluster.

```yaml
apiVersion: couchbase.database.couchbase.com/v1beta1
kind: CouchbaseCluster
metadata:
  name: cb-example
spec:
  baseImage: couchbase/server
  version: enterprise-5.0.1
  authSecret: cb-example-auth
  exposeAdminConsole: true
  cluster:
    dataServiceMemoryQuota: 256
    indexServiceMemoryQuota: 256
    searchServiceMemoryQuota: 256
    indexStorageSetting: memory_optimized
    autoFailoverTimeout: 30
  buckets:
    - name: default
      type: couchbase
      memoryQuota: 128
      replicas: 1
      ioPriority: high
      evictionPolicy: fullEviction
      conflictResolution: seqno
      enableFlush: true
      enableIndexReplica: false
  servers:
    - size: 3
      name: all_services
      services:
        - data
        - index
        - query
        - search
      dataPath: /opt/couchbase/var/lib/couchbase/data
      indexPath: /opt/couchbase/var/lib/couchbase/data
```

By looking at this configuration it should be easy to see that it defines a cluster with:

* Cluster Name: cb-example (metadata.name)
* Couchbase Version: 5.0.1 (spec.version)
* Buckets: 1 bucket named default (spec.buckets)
* Size: 3 node cluster with all services on each node (spec.servers)
* Auth Secret: cb-example-auth

One thing that's important to note is the authSecret field. The Couchbase Operator makes use of Kubernetes Secrets in order to create and manage the Couchbase super-user credentials. As a result the authSecret must refer to secret that contains both a username and a password field. For convience we provide a sample secret that can be pushed into your Kubernetes cluster. The secret sets the username to "Administator" and the password to "password". To load this secret into your Kubernetes cluster run the command below.

```bash
$ kubectl create -f https://packages.couchbase.com/kubernetes/beta/secret.yaml
secret "cb-example-auth" created
```

We can now push the Couchbase configuration to Kubernetes. Once we push it the Couchbase Operator will automatically begin creating the cluster. To push the Couchbase configuration run the command below.

```bash
$ kubectl create -f https://packages.couchbase.com/kubernetes/beta/couchbase-cluster.yaml
couchbasecluster "cb-example" created
```

After you run this command the Couchbase Operator will begin creating your cluster. Depending on what is defined in the configuration this can take some time. You can track the creation of the cluster using the ```kubectl describe``` command. We detail how to use this command and what the output means in the [Displaying Information about a Couchbase Cluster](clusterStatusGuide.md) section.

Once the cluster has been provisioned you will notice that various pods, a service, and a couchbasecluster have been created. The configuration above was for a three node cluster so you should expect three pods to be created. The Couchbase operator also creates an internal headless service that can be used by applications deployed inside the same Kubernetes namespace to connect to the Couchbase cluster deployed. A couchbasecluster object is also created for the cluster and can be used to get health and status information about the cluster. For more details on how to inspect these objects see [Listing and describing pods, services, and couchbaseclusters](listAndDescribe.md)

For further details on what each field in the CouchbaseCluster configuration does as well as a list of all available fields see [CouchbaseCluster Configuration](couchbaseClusterConfig.md).
