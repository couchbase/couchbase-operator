# Upgrading A Couchbase Cluster

The Couchbase operator provides support to automatically upgrade a running cluster.  It performs a rolling upgrade one node at a time.  First a new node is created with the same pod configuration as the chosen node to upgrade, thus maintaining the same services e.g. data, index etc.  Data is rebalanced across the cluster so that the old node is evacuated and ejected from the cluster and the new node takes the load as data is transferred.  By manintaining N+1 nodes during the process we are able to handle all queries seemlessly without any performance degrdation.  Finally the selected upgrade node is deleted.

The upgrade process is currently uninterruptable.  Any changes to the cluster geometry or definition of new buckets will not take effect until the upgrade has been completed.

Upgrades must be upgrades, going from a lower version to a higher version.  Upgrades cannot be performed across multiple major versions e.g. 5.0.1 to 6.4.5 is valid whereas 5.0.1 to 7.3.2 is not.  The final constraint is that upgrades cannot change the release edition e.g. an enterprise cluster must be upgraded to another enterprise version, likewise with community.

Due to the constraints on versioning this feature cannot be used on non-semantically versioned container images e.g. couchbase/server:latest will result in an error.

## Initiating an Upgrade

Simply edit the cluster specification and change the version string to a valid container.  Instructions for updating your specification can be found [here](updatingCouchbase.md)

## Logging and Monitoring

You can monitor the progress of an upgrade via Kubernetes resource status and events:

```console
$ kubectl describe couchbasecluster cb-example
```

The status object shows the version the cluster is being upgraded to, the node that is upgrading and its upgraded replacement and the list of nodes ready to be upgraded.

```console
Status:
  Upgrade:
    Ready Nodes:
      cb-example-0001
      cb-example-0002
    Target Version:  enterprise-5.1.0
    Upgraded Node:   cb-example-0003
    Upgrading Node:  cb-example-0000
```

The events object shows members being created/deleted and data being rebalanced.  From the timestamps in the event stream you can calculate how long each node upgrade takes, then combining with the number of nodes remaining in the status you can easily calculate the expected duration of the entire operation.

```console
Events:
  Type    Reason            Age   From                                 Message
  ----    ------            ----  ----                                 -------
  Normal  UpgradeStarted    20m   couchbase-operator-789c895556-7gbf7  Started upgrade to 'enterprise-5.1.0'
  Normal  NewMemberAdded    19m   couchbase-operator-789c895556-7gbf7  New member cb-example-0003 added to cluster
  Normal  MemberRemoved     18m   couchbase-operator-789c895556-7gbf7  Existing member cb-example-0000 removed from the cluster
  Normal  RebalanceStarted  18m   couchbase-operator-789c895556-7gbf7  A rebalance has been started to balance data across the cluster
  Normal  NewMemberAdded    18m   couchbase-operator-789c895556-7gbf7  New member cb-example-0004 added to cluster
  Normal  RebalanceStarted  17m   couchbase-operator-789c895556-7gbf7  A rebalance has been started to balance data across the cluster
  Normal  MemberRemoved     17m   couchbase-operator-789c895556-7gbf7  Existing member cb-example-0001 removed from the cluster
  Normal  NewMemberAdded    16m   couchbase-operator-789c895556-7gbf7  New member cb-example-0005 added to cluster
  Normal  RebalanceStarted  15m   couchbase-operator-789c895556-7gbf7  A rebalance has been started to balance data across the cluster
  Normal  MemberRemoved     15m   couchbase-operator-789c895556-7gbf7  Existing member cb-example-0002 removed from the cluster
  Normal  UpgradeFinished   15m   couchbase-operator-789c895556-7gbf7  Finished upgrade to 'enterprise-5.1.0'
```
