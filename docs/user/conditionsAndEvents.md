# Status Conditions and Events

To make it easier for users to understand and debug the couchbase-operator, the actions of the operator and the state of the cluster are communicated to the user in the standard Kubernetes convention.

In Kubernetes users will normally view more information about an object via `kubectl describe` which displays, among other things, the [Events](https://kubernetes.io/docs/api-reference/v1.8/#event-v1-core) and [Conditions](https://kubernetes.io/docs/api-reference/v1.8/#podcondition-v1-core) associated with the resource.

Similarly the couchbase-operator exposes the Events and Conditions for each CouchbaseCluster Custom Resource.

## Events
The following types of Events and their specific instances are common in the lifecycle of an CouchbaseCluster:

- A new Couchbase node was added to the cluster
- A Couchbase node was removed from the cluster
- A rebalance has started
- Adding a node to the cluster failed
- A bucket was created
- A bucket was deleted
- A bucket was modified
- The Administration Console service was created
- The Administration Console service was deleted

## Conditions

The Couchbase cluster condition and its statuses are defined as:

- Available
  - True: All members are up and all VBuckets are available
  - False: One or more members are down and someVBuckets are unavailable
- Balanced
  - True: VBuckets are evenly distributed across the cluster
  - False: VBuckets are not evenly distributed across the cluster
  - Unknown: The status is currently unknown
- Scaling
  - True: Scaling from current members size X to spec.size Y
  - False: Reason for failure (e.g no more nodes to place member due to anti-affinity)
  - Not present
- BucketManage
  - False: Creating, editing, or deleting a bucket failed
  - Not present
- ConfigManage
  - False: A configuration change failed
  - Not present
