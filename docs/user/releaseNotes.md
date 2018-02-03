# v0.7 [Beta] Release Notes

The Couchbase Operator is a Kubernetes native integration that enables you to automate the management of common Couchbase tasks such as the configuration, creation, and scaling of Couchbase clusters.
The Couchbase Operator 0.7 [Beta] was released in February 2018.

We consider beta releases to have some rough edges and bugs and hence are intended for development purposes only.
Since the release is still under active development, you can have a big impact on the final version of the product and its documentation by providing feedback and observations.

## Limitations

The Beta release has the following limitations:

- There is no CRD validation at this time, so the Couchbase configurations uploaded to Kubernetes are not verified by the Couchbase Operator.
- The Couchbase Operator can only failover one node at a time in the cluster during auto-failover. This means that if multiple nodes fail, some manual intervention is needed.
- The Couchbase Operator does not support the use of Persistent Volumes.

## Known Issues

Issue | Description
--- | ---
K8S-172 | The index storage mode cannot be changed.

K8S-169 | There is no CRD validation, so the Couchbase configurations uploaded to Kubernetes are not validated. An incorrect configuration can cause all kinds of problems, including potentially crashing the Operator.

K8S-155 | The Couchbase Operator logs show error messages while the cluster is being built. These are harmless and do not impact the cluster creation.

K8S-119 | If a cluster is scaled up beyond the available memory, rebalance fails and the cluster becomes unavailable.

K8S-112 | A rebalance started event is posted after the actual rebalance operation is completed. And no event is posted to indicate that the operation has completed.

K8S-92 | Changing the `cluster-name` field in the example/couchbase-cluster.yaml file causes the cluster get into an inconsistent state.

K8S-80 | The Couchbase Web Console continues to display a node even after it has been removed from the cluster.
