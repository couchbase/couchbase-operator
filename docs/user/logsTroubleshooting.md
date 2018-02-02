# Troubleshooting
When troubleshooting the couchbase operator it's important to rule out Kubernetes itself as the root cause of the problem you are experiencing.  See the Kubernetes [troubleshooting guide](https://kubernetes.io/docs/tasks/debug-application-cluster/troubleshooting/) for tips on debugging applications within a Kubernetes cluster.

The following references are also helpful when troubleshooting:
* [Check cluster status and conditions for errors](clusterStatusGuide.md)
  * [Understanding conditions and events](conditionsAndEvents.md)
* [Listing And Describing Resources](listAndDescribe.md)
* [Dealing with node failures](nodeRecovery.md)
* [Operator Configuration, fields and values](operatorConfig.md)


## Full Deployment Logs

To get logs of the operator along with your entire Kubernetes deployment (nodes, pods, services...), run the support script [cb_k8s_support.sh](https://github.com/couchbase/couchbase-operator/blob/master/scripts/support/cb_k8s_support.sh)

    ./scripts/support/cb_k8s_support.sh

This scripts gathers information about your Kubernetes deployment and creates an archive within your current working directory:

     <cwd>/cb-k8s-support-01182018-14_00_18.tgz


Note this script attempts to run ```kubectl top pod``` to gather pod metrics such as cpu & memory.  By default these metrics are empty unless you have heapster deployed within your Kubernetes cluster.  To deploy heapster for gathering pod metrics, run the following commands:
```bash
git clone https://github.com/kubernetes/heapster.git
cd heapster
kubectl create -f deploy/kube-config/influxdb/
```

Then run the support script again to generate logs which include pod metrics.


## Operator Logs

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

## Getting Server Logs

The easiest way to get cbcollect logs is to access the administration console and click the "Collect Logs" button in the Logs tab. You can also deploy a job within Kubernetes to trigger the log collection:

    kubectl create -f example/couchbase-cli-collect-logs.yaml

Once log collection has completed, check the administraton console (Logs -> Collection Info) for the path to the corresponding zip file for each node in the cluster. You can then run a command like the one below against each node in the cluster to collect their logs.

    kubectl cp <namespace>/<pod_name>:<path_to_logs> -c couchbase-server ./logs.zip

An example of what this might look like is below.

    kubectl cp default/cb-example-0000:/opt/couchbase/var/lib/couchbase/tmp/collectinfo-2017-09-28T175135-ns_1@127.0.0.1.zip -c couchbase-server ./logs.zip

Also, refer to [couchbase server troubleshooting](https://developer.couchbase.com/documentation/server/current/troubleshooting/troubleshooting-intro.html) guide for additional information about reporting issues.
