# CouchbaseCluster Configuration

Below is a sample Couchbase cluster configuration showing all configuration parameters:

```yaml
apiVersion: couchbase.database.couchbase.com/v1beta1
kind: CouchbaseCluster
metadata:
  name: cb-example
  namespace: default
spec:
  baseImage: couchbase/server
  version: enterprise-5.0.1
  paused: false
  antiAffinity: true
  authSecret: my-secret
  exposeAdminConsole: true
  adminConsoleServices:
    - data
  cluster:
    dataServiceMemoryQuota: 256
    indexServiceMemoryQuota: 256
    searchServiceMemoryQuota: 256
    indexStorageSetting: default
    autoFailoverTimeout: 30
  buckets:
    - name: default
      type: couchbase
      memoryQuota: 1024
      replicas: 1
      ioPriority: high
      eviction-policy: value-eviction
      conflictResolution: seqno
      enableFlush: true
      enableIndexReplica: false
  servers:
    - size: 1
      name: all_services
      services:
        - data
        - index
        - query
        - search
      dataPath: /opt/couchbase/var/lib/couchbase/data
      indexPath: /opt/couchbase/var/lib/couchbase/data
      pod:
        couchabseEnv:
          ENV1: value
        resources:
          limits:
            cpu: 4GHz
            memory: 8GiB
            storage: 100GiB
          requests:
            cpu: 2GHz
            memory: 8GiB
            storage: 50GiB
        labels:
          couchbase_services: all
        nodeSelector:
          instanceType: large
        tolerations:
          key: app
          operator: Equal
          value: cbapp
          effect: NoSchedule
        automountServiceAccountToken: false
```

## Top-Level Definitions

Below are the top level parameters for a Couchbase configuration:

```yaml
apiVersion: couchbase.database.couchbase.com/v1beta1
kind: CouchbaseCluster
metadata:
  name: cb-example
  namespace: default
spec:
...
```

**apiversion**

This field allows us to specify the API Version for our integration. As our integration changes over time we can change the API Version whenever we add new features. For any given release we will specify the API Versions supported by that operator in our documentation. We will also always recommend that users upgrade to the latest API Version when possible. This field is always required in the configuration.

**kind**

Specifies that this Kubernetes configuration will use our custom Couchbase controller to manage the cluster. This field will always be set to “CouchbaseCluster” and it is always required to be in the configuration.

**metadata**

Allows setting a name for the cluster and a namespace that the cluster should be deployed in. A name is highly recommended, but not required. If a name is not given then a name will be generated. The namespace is also not required and will default to the “default” namespace if not provided.

**spec**

Contains the specification for how the Couchbase cluster will be deployed.

## Spec: The Top-Level

Defines top level parameters related to a Couchbase Cluster deployment.

```yaml
...
  baseImage: couchbase/server
  version: enterprise-5.0.1
  paused: false
  antiAffinity: true
  authSecret: my-auth-secret
  exposeAdminConsole: true
  adminConsoleServices:
    - data
...
```

**baseImage**

Specifies the image, without the version number, that should be used. This value should always be set to “couchbase/server” and is required.

**version**

The version number of Couchbase Server to install. This should match an available version number in the Couchbase docker repository. It may never be changed to a lower version number that the version that is currently running. This field is required in the configuration.

**paused**

Specifies whether or not the operator is currently managing this cluster. This parameter should generally be set to true, but may be set to false when the user decides to make manual changes to the cluster. By disabling the operator users can change the cluster configuration without having to worry about the Operator reverting changes. Before re-enabling the Operator the user should make sure that the Kubernetes configuration matches the cluster configuration.

**antiAffinity**

Anti-affinity allows the user to specify whether or not two Pods in this cluster can be deployed on the same Kubernetes Node. In a production setting this parameter should always be set to true in order to reduce the chance of data-loss if a Kubernetes Node crashes. This parameter defaults to true if it is not set.

**authSecret**

The authSecret specifies the name of a Kubernetes Secret that should be used as for the username and password of the Couchbase super-user. This field is required.

**exposeAdminConsole**

Specifies whether or not the Couchbase Administration Console should be exposed externally. Exposing the Administration Console is done using a NodePort service and the port for the service can be found in the describe output when describing this cluster. This parameter may be changed while the cluster is running and the Operator will create/destroy the NodePort service as appropriate.

**adminConsoleServices**

When the Couchbase Administration Console is exposed the pod that is connected to is chosen based on the client IP address. Since the Admin Console may display slightly different features based on the services running on the particular pod connected to we allow the user to specify that to exposed port will only connect to pods that contain the services specified for this parameter. A list of one or more services should be provided. If no services are provided then all node can be connected to.

## Spec: The Cluster Settings

Allows the user to specify various Couchbase cluster settings. This section is not meant to encompass every setting that is configurable on Couchbase. Cluster settings not mentioned here can be managed manually.

```yaml
...
  cluster:
    dataServiceMemoryQuota: 4096
    indexServiceMemoryQuota: 1024
    searchServiceMemoryQuota: 2048
    indexStorageType: plasma
    autoFailoverTimeout: 30
...
```

**dataServiceMemoryQuota**

The amount of memory to assign the the data service if present on a specific Couchbase node in MiB. This parameter defaults to 256MiB if it is not set.

**indexServiceMemoryQuota**

The amount of memory to assign the the index service if present on a specific Couchbase node in MiB. This parameter defaults to 256MiB if it is not set.

**searchServiceMemoryQuota**

The amount of memory to assign the the search service if present on a specific Couchbase node in MiB. This parameter defaults to 256MiB if it is not set.

**indexStorageType**

Specifies the backend storage type to use for the index service. This field can either be set to “plasma” or “memory-optimized”. If the cluster already contains a Couchbase Server instance running the index service then this parameter cannot be changed until all instances running the index service are removed.

**autoFailoverTimeout**

Specifies the auto-failover timeout in seconds. The Operator relies on the Couchbase cluster to auto-failover nodes before removing them so setting this to an appropriate value is important.

## Spec: Bucket Settings

This section allows the user to define one or more buckets that should be created in the cluster. If an already specified bucket is removed from the configuration then the operator will delete that bucket.

```yaml
...
  buckets:
    - name: default
      type: couchbase
      memoryQuota: 1024
      replicas: 1
      ioPriority: high
      evictionPolicy: value-eviction
      conflictResolution: seqno
      enableFlush: true
      enableIndexReplica: false
...
```

**name**

The name of the bucket. This parameter is required when specifying a bucket.

**type**

The type of bucket to create. This parameter can be set to “couchbase”, “ephemeral”, or “memcached”. If it is not specified it defaults to “couchbase”.

**memoryQuota**

The amount of memory to allocate to this bucket in MiB. If this parameter is not specified the it defaults to 100MiB.

**replicas**

The number of replica that should be created for this bucket. This value may be set between 0 and 3 inclusive. If it is not set then this parameter default to 1. Note that this parameter has no effect for the memcached bucket type.

**ioPriority**

Sets the IO priority of background threads for this bucket. This field can either be set to “high” or “low”. If this parameter is not specified then it defaults to “high”.

**evictionPolicy**

The memory-cache eviction policy for this bucket. This option is valid for Couchbase and Ephemeral buckets only.

Couchbase buckets support either "valueOnly" or "fullEviction". Specifying the "valueOnly" policy means that each key stored in this bucket must be kept in memory. This is the default policy: using this policy improves performance of key-value operations, but limits the maximum size of the bucket. Specifying the "fullEviction" policy means that performance is impacted for key-value operations, but the maximum size of the bucket is unbounded.

Ephemeral buckets support either "noEviction" or "nruEviction". Specifying "noEviction" means that the bucket will not evict items from the cache if the cache is full: this type of eviction policy should be used for in-memory database use-cases. Specifying "nruEviction" means that items not recently used will be evicted from memory, when all memory in the bucket is used: this type of eviction policy should be used for caching use-cases.

**conflictResolution**

Specifies this bucket's conflict resolution mechanism; which is to be used if a conflict occurs during Cross Data-Center Replication (XDCR). Sequence-based and timestamp-based mechanisms are supported.

Sequence-based conflict resolution selects the document that has been updated the greatest number of times since the last sync: for example, if one cluster has updated a document twice since the last sync, and the other cluster has updated the document three times, the document updated three times is selected; regardless of the specific times at which updates took place.

Timestamp-based conflict resolution selects the document with the most recent timestamp: this is only supported when all clocks on all cluster-nodes have been fully synchronized.

**enableFlush**

Specifies whether or not to enable the flush command on this bucket. This parameter defaults to false if it is not specified. This parameter only affects Couchbase and Ephemeral buckets.

**enableIndexReplicas**

Specifies whether or not to enable view index replicas for this bucket. This parameter defaults to false if it is not specified. This parameter only affects Couchbase buckets.

## Spec: Servers

Users must specify at least one, but possibly multiple node specifications. A node specification is used to allow multi-dimensional scaling of a Couchbase cluster with Kubernetes.

```yaml
...
  servers:
  - size: 1
    name: all_services
    services:
      - data
      - index
      - query
      - search
    dataPath: /opt/couchbase/var/lib/couchbase/data
    indexPath: /opt/couchbase/var/lib/couchbase/data
    pod:
      couchabseEnv:
        CB_ENV_VAR: value
      resources:
        limits:
          cpu: 4GHz
          memory: 8GiB
          storage: 100GiB
        requests:
          cpu: 2GHz
          memory: 8GiB
          storage: 50GiB
      labels:
        couchbase_services: all
      nodeSelector:
        instanceType: large
      tolerations:
        key: app
        operator: Equal
        value: cbapp
        effect: NoSchedule
```

**size**

Specifies how many nodes of this type should be in the cluster. This allows the user to scale up different parts of the cluster as necessary. If this parameter is changed at runtime the Operator will automatically scale the cluster.

**name**

Gives a name for this group of servers. The name must be unique in comparison to the other server names. It is required and cannot be changed.

**services**

A list of services that should be run on nodes of this type. Users can specify data, index, query, and search in the list. At least one service must be specified and all clusters must contain at least one node specification that includes the data service.

**dataPath**

Specifies the path where data from the data service should be stored. This parameter is required.

**indexPath**

Specifies the path were data created by indexes should be stored. This parameter is required.

### The Pod Policy

The pod policy defines settings that apply to all Pods deployed with this node configuration. A Pod always contains a single running instance of Couchbase Server.

```yaml
...
    podPolicy:
      couchabseEnv:
        CB_ENV_VAR: value
      resources:
        limits:
          cpu: 4GHz
          memory: 8GiB
          storage: 100GiB
        requests:
          cpu: 2GHz
          memory: 8GiB
          storage: 50GiB
      labels:
        couchbase_services: all
      nodeSelector:
        instanceType: large
      tolerations:
        key: app
        operator: Equal
        value: cbapp
        effect: NoSchedule
```

**couchbaseEnv**

Allows the user to specify environment variables that should be set when the pod is started as key-value pairs. The key in this case is the environment variable name and the value is the value the the environment variable should hold. This section is optional.

#### Resources

```yaml
...
    resources:
      limits:
        cpu: 2GHz
        memory: 8GiB
        storage: 100GiB
      requests:
        cpu: 1.5GHz
        memory: 8GiB
        storage: 50GiB
...
```

**limits**

Allows the user to reserve resources on a specific node. The limits section defines the maximum amount of CPU, memory, and storage the pods created in this node specification will reserve.

**requests**

Allows the user to reserve resources on a specific node. The requests section defines the minimum amount of CPU, memory, and storage the pods created in this node specification will reserve.

See the Kubernetes documentation for further information about [managing compute resources](https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/).

**labels**

Labels are key-value pairs that are attached to objects in Kubernetes. They are intended to specify identifying attributes of objects that are meaningful to the user and do not directly imply semantics to the core system. Labels can be used to organize and select subsets of objects. They do not need to be unique across multiple objects. This section is optional.

Labels added in this section will apply to all pods added in this cluster. Note that by default we add the following labels to each pod which should not be overridden.

    app:couchbase
    couchbase_cluster:<metadata:name>

In this example the label would be couchbase_cluster:cb-example
couchbase_node: <metadata:name>-<gen node id>

In this example the label for the first pod would couchbase_node:cb-example-0000

See the Kubernetes documentation about [labels](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/) for more information.

**nodeSelector**

Specifies a key-value map of constraints on node placement for pods. For a pod to be eligible to run on a node, the node must have each of the indicated key-value pairs as labels (it can have additional labels as well). If this section is not specified then Kubernetes will place the pod on any available node. See the Kubernetes documentation about [label selectors](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors) for more details.

**tolerations**

Specifies conditions upon which a node should not be selected when deploying a pod. This example says that any node with a label app:cbapp should not be allowed to run the pod defined in this node specification. You might do this if you have nodes dedicated for running an application using Couchbase where you don’t want the database and application to be running on the same node. See the Kubernetes documentation on [taints and tolerations](https://kubernetes.io/docs/concepts/configuration/taint-and-toleration/) for more information on setting tolerations. The tolerations section is optional.
