# Troubleshooting
When troubleshooting the couchbase operator it's important to rule out kubernetes itself as the root cause of the problem you are experiencing.  See the kubernetes [troubleshooting guide](https://kubernetes.io/docs/tasks/debug-application-cluster/troubleshooting/) for tips on debugging applications within a kubernetes cluster.

The following references are also helpful when troubleshooting:
* [Check cluster status and conditions for errors](clusterStatusGuide.md)
  * [Understanding conditions and events](conditionsAndEvents.md)
* [Listing And Describing Resources](listAndDescribe.md)
* [Dealing with node failures](nodeRecovery.md)
* [Operator Configuration, fields and values](operatorConfig.md)


## Operator Troubleshooting

The couchbase operator generates logs that can be very helpful when troubleshooting your deployment.  Using kubectl, the operator logs can be printed to stdout:

```console
# Get name of operator pod
$ kubectl get po -lname=couchbase-operator
NAME                                  READY     STATUS    RESTARTS   AGE
couchbase-operator-1917615544-h20bm   1/1       Running   0          20h

# Get logs
$ kubectl logs couchbase-operator-1917615544-h20bm
time="2018-01-23T22:56:34Z" level=info msg="Obtaining resource lock" module=main
time="2018-01-23T22:56:34Z" level=info msg="Starting event recorder" module=main
time="2018-01-23T22:56:34Z" level=info msg="Attempting to be elected the couchbase-operator leader" module=main
time="2018-01-23T22:56:51Z" level=info msg="I'm the leader, attempt to start the operator" module=main
time="2018-01-23T22:56:51Z" level=info msg="Creating the couchbase-operator controller" module=main
```

The following messages indicate the operator is unable to reconcile your cluster into a desired state:
* Logs with level=error
* Operator is unable to get cluster state after N retries

Logs for operator and all pods can be gathered using the support script.
```console
$ ./scripts/support/cb_k8s_support.sh
```

This scripts gathers information about your kubernetes deployment and creates an archive within your current working directory:
```
$ tar -xf cb-k8s-support-01182018-14_00_18.tgz
$ cd cb-k8s-support-01182018-14_00_18
$ tail containers/default/couchbase-operator-1917615544-g765d/output.log
2018-01-18T21:00:14.992396463Z time="2018-01-18T21:00:14Z" level=info msg="Finish reconciling" cluster-name=cb-example module=cluster
2018-01-18T21:00:22.996438871Z time="2018-01-18T21:00:22Z" level=info msg="Start reconciling" cluster-name=cb-example module=cluster
2018-01-18T21:00:23.033646507Z time="2018-01-18T21:00:23Z" level=info msg="Finish reconciling" cluster-name=cb-example module=cluster
```

## Couchbase Server Troubleshooting

When the couchbase operator is functioning properly, but the couchbase cluster needs some troubleshooting, then refer to the [getting server logs](https://github.com/couchbase/couchbase-operator/blob/master/docs/user/administrationGuide.md#getting-server-logs) section from the administration guide.  Also, refer to [couchbase server troubleshooting](https://developer.couchbase.com/documentation/server/current/troubleshooting/troubleshooting-intro.html) guide for additional information about reporting issues.

