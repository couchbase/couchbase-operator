# Cluster Status

Kubernetes users often need to check the status of various objects (eg. Pods, Deployments, and StatefulSets) that have been deployed inside their Kubernetes clusters. Status checking is done by "describing" one of the objects in the cluster using ```kubectl describe``` command. This command outputs the configuration for the specified object, a status section, and an event section. The status section varies depending on the object type and shows various health metrics related to the object described. The events sections is printed last and shows major events that have happened during the life of the object.

Since the Couchbase Operator registers a "CouchbaseCluster" CRD with Kubernetes it allows Kuberentes to know about Couchbase clusters natively. This means that the CouchbaseCluster becomes another object that can be described to get the configuration, status, and events specific to a particular Couchbase cluster.

To describe a Couchbase cluster named "cb-example" run the following command.

```bash
kubectl describe couchbasecluster cb-example
```

In the command above note that the object name is "couchbasecluster". You can also use the shorthand name "cbc" as the object name to save some typing.

```bash
kubectl describe cbc cb-example
```

Below is an example of the output of the ```kubectl describe``` command. The various parts of the output and their significance will be discussed in more detail in the sections that follow.

```yaml
Name:		cb-example
Kind:		CouchbaseCluster
Spec:   ...
Status:
  Admin Console Port:		30239
  Admin Console Port SSL:	31628
  Buckets:
    Default:
      Conflict Resolution:	seqno
      Enable Flush:		true
      Enable Index Replica:	false
      Eviction Policy:		fullEviction
      Io Priority:		high
      Memory Quota:		128
      Name:			default
      Replicas:			1
      Type:			couchbase
  Cluster Id:			9ae7fe4634e0360cf9c9245cb4ebb27b
  Conditions:
    ...
    Message:			Data is equally distributed across all nodes in the cluster
    Reason:			Cluster is balanced
    Status:			True
    Type:			Balanced
  Control Paused:		false
  Current Version:		enterprise-5.0.1
  Members:
    Ready:
      cb-example-0000
      cb-example-0001
      cb-example-0002
  Phase:		Running
  Reason:
  Size:			3
  Events:
  FirstSeen	LastSeen	Count	From			Type		Reason			Message
  ---------	--------	-----	----			--------	------			-------
  21m		21m		1	couchbase-operator	Normal		NewMemberAdded		New member cb-example-0000 added to cluster

```

# Status And Events

Often when the status spec of the schema is updated, an associated event is generated which details the reason why the change occurred.  For instance, increasing the size of the cluster causes an add member event to appear in the Events section of the status result.  The following details the information provided in the status response along with any events associated with value updates.

### Admin Console

    Admin Console Port: 30239
    Admin Console Port SSL:	31628

Ports used for exposing the Couchbase cluster's administration console.  This section of the status is only visible when the ```exposeAdminConsole``` setting of the cluster spec is enabled.  See [admin console access guide](adminConsoleAccess.md) for information about how to expose and access the the administration console.

Enabling and disabling the ```exposeAdminConsole``` setting produces the following events, respectively:

    Events:
      ...  ServiceCreated		Service for admin console `cb-example-ui` was created
      ...  ServiceDeleted		Service for admin console `cb-example-ui` was deleted

### Buckets

    Buckets:
      Default:
        Memory Quota:		128
        Name:			default
        ...

List of buckets currently active within the Couchbase cluster.  The values of the bucket status are the same as the values provided in the bucket spec.  Refer to [couchbaseClusterConfig](couchbaseClusterConfig.md) for info about the bucket spec. When buckets from the spec are added, updated, or removed the following events are generated, respectively:

    Events:
      ... BucketCreated		A new bucket `default` was created
      ... BucketEdited		Bucket `default` was edited
      ... BucketDeleted		Bucket `default` was deleted

The bucket section of the status only reflects valid changes that have been made to the cluster.  In the case of attempts to update bucket section with an invalid spec, the following message will be seen in the ```Conditions``` section of the Status:

    Conditions:
      Message:	Bucket: default cannot change (default) bucket param='conflictResolution' from 'seqno' to 'timestamp'
      Reason:		Bucket edit failed
      Status:		False
      Type:			ManageBuckets

To resolve errors resulting from invalid updates, edit the cluster spec according to the message described in the Condition and apply the updated spec.

### Cluster ID

    Cluster Id:			9ae7fe4634e0360cf9c9245cb4ebb27b

The cluster identifier as provided by the Couchbase cluster.  This value is provided directly by the Couchbase cluster and is therefore never updated by any changes to the cluster spec.


### Members

    Members:
      Ready:
        cb-example-0000
        cb-example-0001
        cb-example-0002


Members represent pods that are managed by the couchbase-operator.  All members in the Ready section represent pods that make up the Couchbase cluster.  The status of cluster members is directly affected by the value of ```Size``` in the cluster spec.  When `Size` is increased a member add event is generated, followed by rebalance:

    Events:
      ... NewMemberAdded		New member cb-example-0003 added to cluster
      ... RebalanceStarted	A rebalance has been started to balance data across the cluster

Removing a member generates the expected removal event, followed by a rebalance:

    Events:
      ... RebalanceStarted	A rebalance has been started to balance data across the cluster
      ... MemberRemoved		Existing member cb-example-0003 removed from the cluster

Note: It is also possible for a MemberRemoved event to be generated when an auto-failover occurs and a member is replaced by a new member.

The ```Conditions``` section of the status is also updated as the operator works to resolve changes to cluster spec size or failures.  Important condition to check when scaling are:
 * Balanced: Denoting whether data is equally distributed across all nodes in the cluster
 * Available: Denoting whether all members are up and all VBuckets are available
 * Scaling: Denoting whether cluster is currently scaling

See [conditionsAndEvents](conditionsAndEvents.md) for more information about these conditions and their statuses.

### Phase

    Phase:		Running

The current phase of the cluster.  Can either of the following:
*  Creating: When cluster is first deployed
*  Running:  After the ```couchbasecluster``` resource object is available and first orchestrator pod is running
*  Failed: When failure occurs either within Creation or Running phase of the cluster
*  None: Initial state denoted by an empty string


### Size

    Size:			3

The size of the Couchbase cluster.  When this value is changed in the cluster spec, this value is also updated as members are added to the Couchbase cluster.  If errors occur while the operator is attempting to conform the cluster to the desired size then the ```Conditions``` section should be checked for information about the current state of the cluster.  See the [admin console access](adminConsoleAccess.md) for information about how to manually access the cluster and collect logs when additional troubleshooting is needed when scaling your cluster.


### Conditions

    Conditions:
      ...
      Message:			Data is equally distributed across all nodes in the cluster
      Reason:			Cluster is balanced
      Status:			True
      Type:			Balanced

List of conditions reflecting the current state of the Couchbase cluster.  Each Condition item is denoted by a ```Type``` along with an associated ```Status```.  The various types of conditions statuses are documented in [conditionsAndEvent](conditionsAndEvents.md).

### Events

    Events:
      29m		29m		1	couchbase-operator-1917615544-j1mg8			Normal		NewMemberAdded		New member cb-example-0003 added to cluster
      29m		29m		1	couchbase-operator-1917615544-j1mg8			Normal		RebalanceStarted	A rebalance has been started to balance data across the cluster
      51m		51m		1	couchbase-operator-1917615544-j1mg8			Normal		BucketEdited		Bucket `default` was edited

Events generated during cluster reconciliation.  The last 10 events are recorded and time stamped as the operator works to reconcile the cluster to its desired cluster state.  The types of events that can occur throughout the lifecycle of a couchbasecluster are documented in [conditionsAndEvent](conditionsAndEvents.md).
