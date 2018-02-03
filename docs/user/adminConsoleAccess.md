## Accessing the Couchbase Web Console

To access the Couchbase Web Console, you must enable the ```exposeAdminConsole``` setting within the Couchbase cluster configuration file, for example:
```yaml
kind: "CouchbaseCluster"
spec:
  ....
  exposeAdminConsole: true
```

This creates a NodePort service which exposes the Web Console on each of your nodes. You can get the port that the service is exposing to your nodes by checking the status of the cluster resource ```kubectl describe cbc```:
```
Status:
  Admin Console Port:		30239
  Admin Console Port SSL:	31628
```
The Web Console is now available across all the nodes in your cluster via ```<node_ip>:30239```, where ```<node_ip>``` represents the IP address of a Kubernetes node.
The username and password for Couchbase on port 8091 are set in your secret.yaml file.

If at any time you want to remove access to the Web Console, set the exposeAdminConsole setting to false or remove it from the cluster configuration file.

### Accessing Specific Couchbase Services

By default, the exposed port will forward to any of the pods in your cluster. To access the Web Console of a pod running a specific service such as 'search' or 'query, specify which services to expose by using the ```adminConsoleServices``` setting. For example, to expose only pods running the query service:

```yaml
kind: "CouchbaseCluster"
spec:
  ....
  exposeAdminConsole: true
  adminConsoleServices:
    - query
```


### Accessing Specific Couchbase Pods

To directly access the Web Console of a specific pod, run the following command on the pod you want to expose. For example:

  ```
    kubectl port-forward cb-example-0000 8091:8091
  ```

The Web Console for pod cb-example-0000 will now be available on 127.0.0.1:8091. Note that you will not be able to access other nodes in your cluster as this command will only proxy you through to the specified node.

The user name and password for Couchbase on port 8091 are set in your secret.yaml file.
