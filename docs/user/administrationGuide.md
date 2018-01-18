# Administration Guide

## Accessing the Administration Console

The administration console can be accessed by enabling the ```exposeAdminConsole``` setting within the couchbase clusters yaml spec, for example:
```yaml
kind: "CouchbaseCluster"
spec:
  ....
  exposeAdminConsole: true
```

This creates a NodePort service which exposes the administrator console on each of your nodes.  You can get the port that the service is exposing to your nodes by checking the status of the cluster resource ```kubectl describe cbc```:
```
Status:
  Admin Console Port:		30239
  Admin Console Port SSL:	31628
```
The administration console is now available across all the nodes in your cluster via ```<node_ip>:30239```, where ```<node_ip>``` represents the ip address of a kubernetes node.
The username and password for Couchbase on port 8091 are set in your secret.yaml file.

If at any time you want to remove access to the administrator console, set the exposeAdminConsole setting to false or remove it from the cluster spec.

 ### Accessing specific couchbase services

By default, the exposed port will forward to any of the pods in your cluster. To access the administration console of a pod running a specific service such as 'search' or 'query, specify which services to expose by using the ```adminConsoleServices``` setting.  For example, to expose only pods running the query service:

```yaml
kind: "CouchbaseCluster"
spec:
  ....
  exposeAdminConsole: true
  adminConsoleServices:
    - query
```


### Accessing specific couchbase pods

To directly access the administration console of a specific pod you can run the following command on the pod you want to expose, for example:

    kubectl port-forward cb-example-0000 8091:8091

This command will make the Administration console for pod cb-example-0000 available on 127.0.0.1:8091. Note that you will not be able to access other nodes in your cluster since this command will only proxy you through to the specified node.

The username and password for Couchbase on port 8091 are set in your secret.yaml file.

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
