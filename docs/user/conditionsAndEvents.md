# Status Conditions and Events

The actions of the Couchbase Operator and the state of the cluster are communicated using the standard Kubernetes convention.

In Kubernetes, you can normally view more information about an object using the `kubectl describe` command which displays, among other things, the [Events](https://kubernetes.io/docs/api-reference/v1.8/#event-v1-core) and [Conditions](https://kubernetes.io/docs/api-reference/v1.8/#podcondition-v1-core) associated with the resource.
Similarly, the Couchbase Operator exposes the events and conditions for each CouchbaseCluster custom resource.

## Events

The lifecycle of a CouchbaseCluster includes the following events (in no particular order):

- A new Couchbase node was added to the cluster.
- A Couchbase node was removed from the cluster.
- A rebalance has started.
- Adding a node to the cluster failed.
- A bucket was created.
- A bucket was deleted.
- A bucket was modified.
- The Web Console service was created.
- The Web Console service was deleted.

## Conditions

The CouchbaseCluster conditions and their statuses are defined in the following list:

- Available
  - True: All members are up and all vBuckets are available.
  - False: One or more members are down and some vBuckets are unavailable.
- Balanced
  - True: The vBuckets are evenly distributed across the cluster.
  - False: The vBuckets are not evenly distributed across the cluster.
  - Unknown: The status is currently unknown.
- Scaling
  - True: Scaling from current members size X to spec.size Y.
  - False: Reason for failure (e.g., No more nodes to place member due to anti-affinity.)
  - Not present
- BucketManage
  - False: Creating, editing, or deleting a bucket failed.
  - Not present
- ConfigManage
  - False: A configuration change failed.
  - Not present
