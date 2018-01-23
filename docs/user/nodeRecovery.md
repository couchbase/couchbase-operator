# Node Recovery

The Couchbase Operator has the ability to detect node failures, rebalance out bad nodes, and bring the cluster back up to the desired capacity. This all happens automatically and without the need for user intervention in most cases. In the typical case when a node becomes unresponsive the Operator will wait for Couchbase Server to recognize the node as being down and fail it over. Couchbase waits for the node to be down for a specific amount of time before failing over the node and this failover timeout can be set in the Couchbase clusters Kubernetes configuration.

Once the node has been failed over the Operator will detect the down node, mark it for removal, create a new pod running Couchbase, and rebalance. This will remove the down node from the cluster and add a new node into the cluster in order to bring the cluster back to the desired size.

### What happens if multiple nodes fail?

Couchbase auto-failover will currently only failover one node in the cluster at a time. This means that if multiple nodes fail then some user intervention will be needed. In this scenario the user will be required to manually failover all down nodes. The operator will then take care of adding new nodes back into the cluster. The easiest way to failover down nodes is to log into the Couchbase Administration Console and click the failover button on each down node.

After the nodes have been failed over the user must check to see if the amount of failures resulted in any loss of data. If data was lost then the user should take steps to recover the data by restoring  from a backup or setting up and XDCR replication from a remote cluster.

In the future the operator will be able to handle multiple node failures and be able to automatically restore data from backups in a data loss scenario.

### Why can't we use a Persistent Volume and move the data to another pod?

The current operator doesn't support the use of Persistent Volumes. In the future we will support Persistent Volumes and allow recovery options where we mount the Persistent Volume of the failed pod to a new pod and then use delta recovery to re-add the node to the cluster.
