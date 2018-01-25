## Getting Server Logs

The easiest way to get cbcollect logs is to access the administration console and click the "Collect Logs" button in the Logs tab. You can also deploy a job within kubernetes to trigger the log collection:

    kubectl create -f example/couchbase-cli-collect-logs.yaml

Once log collection has completed, check the administraton console (Logs -> Collection Info) for the path to the corresponding zip file for each node in the cluster. You can then run a command like the one below against each node in the cluster to collect their logs.

    kubectl cp <namespace>/<pod_name>:<path_to_logs> -c couchbase-server ./logs.zip

An example of what this might look like is below.

    kubectl cp default/cb-example-0000:/opt/couchbase/var/lib/couchbase/tmp/collectinfo-2017-09-28T175135-ns_1@127.0.0.1.zip -c couchbase-server ./logs.zip

## Getting Deployment Logs

To get logs of the entire kubernetes deployment (nodes, pods, services...), run the support script [cb_k8s_support.sh](https://github.com/couchbase/couchbase-operator/blob/master/scripts/support/cb_k8s_support.sh)

    ./scripts/support/cb_k8s_support.sh

This scripts gathers information about your kubernetes deployment and creates an archive within your current working directory:

     <cwd>/cb-k8s-support-01182018-14_00_18.tgz

Note this script attempts to run ```kubectl top pod``` to gather pod metrics such as cpu & memory.  By default these metrics are empty unless you have heapster deployed within your kubernetes cluster.  To deploy heapster for gathering pod metrics, run the following commands:
```bash
git clone https://github.com/kubernetes/heapster.git
cd heapster
kubectl create -f deploy/kube-config/influxdb/
```

Then run the support script again to generate logs which include pod metrics.
