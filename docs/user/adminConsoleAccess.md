## Accessing the Administration Console

The administration console can be accessed by enabling the ```exposeAdminConsole``` setting within the Couchbase clusters YAML spec, for example:
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
The administration console is now available across all the nodes in your cluster via ```<node_ip>:30239```, where ```<node_ip>``` represents the ip address of a Kubernetes node.
The username and password for Couchbase on port 8091 are set in your secret.yaml file.

If at any time you want to remove access to the administrator console, set the exposeAdminConsole setting to false or remove it from the cluster spec.

### Accessing specific Couchbase services

By default, the exposed port will forward to any of the pods in your cluster. To access the administration console of a pod running a specific service such as 'search' or 'query, specify which services to expose by using the ```adminConsoleServices``` setting.  For example, to expose only pods running the query service:

```yaml
kind: "CouchbaseCluster"
spec:
  ....
  exposeAdminConsole: true
  adminConsoleServices:
    - query
```


### Accessing specific Couchbase pods

To directly access the administration console of a specific pod you can run the following command on the pod you want to expose, for example:

    kubectl port-forward cb-example-0000 8091:8091

This command will make the Administration console for pod cb-example-0000 available on 127.0.0.1:8091. Note that you will not be able to access other nodes in your cluster since this command will only proxy you through to the specified node.

The username and password for Couchbase on port 8091 are set in your secret.yaml file.
