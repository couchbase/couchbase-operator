# Logs and Troubleshooting

This section provides information on how to diagnose and troubleshoot problems with the Couchbase Operator or your deployment.

When troubleshooting the Couchbase Operator, it is important to rule out Kubernetes itself as the root cause of the problem you are experiencing.  See the Kubernetes [Troubleshooting Guide](https://kubernetes.io/docs/tasks/debug-application-cluster/troubleshooting/) for tips on debugging applications within a Kubernetes cluster.

The following references are also helpful when troubleshooting:
* [Cluster Status and Conditions](clusterStatusGuide.md)
  * [Understanding Conditions and Events](conditionsAndEvents.md)
* [Listing And Describing Resources](listAndDescribe.md)
* [Node Recovery](nodeRecovery.md)
* [Operator Configuration](operatorConfig.md)

## Full Deployment Logs

To get logs of the Operator along with your entire Kubernetes deployment (nodes, pods, services...), run the following script: [cb_k8s_support.sh](https://packages.couchbase.com/kubernetes/beta/couchbase-kubernetes-support.sh.sh)
```bash
wget https://packages.couchbase.com/kubernetes/beta/couchbase-kubernetes-support.sh
chmod +x couchbase-kubernetes-support.sh
./couchbase-kubernetes-support.sh
```

This scripts gathers information about your Kubernetes deployment and creates an archive within your current working directory:
```bash
<cwd>/cb-k8s-support-01182018-14_00_18.tgz
```

This script runs ```kubectl top pod``` to gather pod metrics such as CPU and memory.  By default, these metrics are empty unless you have ```heapster``` deployed alongside your cluster.  Run the following commands to deploy ```heapster``` within kubernetes environment:
```bash
git clone https://github.com/kubernetes/heapster.git
cd heapster
kubectl create -f deploy/kube-config/influxdb/
kubectl create -f deploy/kube-config/rbac/heapster-rbac.yaml
```
For openshift refer to install guide for [cluster metrics.](https://docs.openshift.org/latest/install_config/cluster_metrics.html).

Then run the support script, ```cb_k8s_support.sh```, again to generate logs which include pod metrics.

## Operator Logs

The Couchbase Operator generates logs that can help troubleshoot your deployment.  Using ```kubectl``` or ```oc```, you can choose to print the Operator logs to stdout.
On Kubernetes:
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
On OpenShift:
```console
# Get name of operator pod
$ oc get po -lname=couchbase-operator
```

Watch for the following messages which indicate that the Operator is unable to reconcile your cluster into a desired state:
* Logs with level=error
* Operator is unable to get cluster state after N retries

## Getting Couchbase Server Logs

The easiest way to get ```cbcollect``` logs is to access the Couchbase Web Console and click the **Collect Logs** button in the Logs tab. You can also deploy a job within Kubernetes to trigger log collection:
On Kubernetes:
```bash
kubectl create -f https://packages.couchbase.com/kubernetes/beta/couchbase-cli-collect-logs.yaml
```
On OpenShift:
```bash
    oc create -f example/couchbase-cli-collect-logs.yaml
```
Once log collection has completed, check the Couchbase Web Console > Logs tab > Collection Info for the path to the corresponding zip file for each node in the cluster. You can then run a command like the following for each node in the cluster to collect their logs.
On Kubernetes:
```bash
kubectl cp <namespace>/<pod_name>:<path_to_logs> -c couchbase-server ./logs.zip
```
On OpenShift:
```bash
    oc cp <namespace>/<pod_name>:<path_to_logs> -c couchbase-server ./logs.zip
```
Here is an example command to collect the logs for node `cb-example-0000`.
On Kubernetes:
```bash
kubectl cp default/cb-example-0000:/opt/couchbase/var/lib/couchbase/tmp/collectinfo-2017-09-28T175135-ns_1@127.0.0.1.zip -c couchbase-server ./logs.zip
```
On OpenShift:
```bash
    oc cp default/cb-example-0000:/opt/couchbase/var/lib/couchbase/tmp/collectinfo-2017-09-28T175135-ns_1@127.0.0.1.zip -c couchbase-server ./logs.zip
```
## See Also##

Refer to the [Couchbase Server Troubleshooting](https://developer.couchbase.com/documentation/server/current/troubleshooting/troubleshooting-intro.html) guide for additional information about reporting issues.
