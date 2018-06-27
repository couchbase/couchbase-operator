# Scaling a CouchbaseCluster

Using the Couchbase Operator, you can easily scale clusters and services up and down. The handling of individual nodes is defined in the servers section of the [Couchbase cluster specification](couchbaseClusterConfig.md). Here is an example of a definition for a Couchbase cluster where all nodes run the data, index, search, and query services.

```yaml
servers:
  - size: 3
    name: all_services
    services:
      - data
      - index
      - query
      - search
```

This configuration specifies that the cluster should contain 3 nodes running the data, index, query, and search services and that the data and index path are each set to ```/opt/couchbase/var/lib/couchbase/data```. To scale these servers, you only need to change the ```size``` parameter and then push the new configuration into Kubernetes to increase or decrease the number of nodes in the cluster. To update the configuration in Kubernetes, run the following command:

On Kubernetes:

```bash
kubectl replace -f <path to config>
```

On OpenShift:

```bash
oc replace -f <path to config>
```

Alternatively you can also edit the configuration of a Couchbase cluster directly by running the command below.

On Kubernetes:

```bash
kubectl edit cbc <name of cluster>
```

On OpenShift:

```bash
oc edit cbc <name of cluster>
```

This command pulls the current configuration for the cluster from Kubernetes and opens it up in an editor. You can then edit the configuration, and upon saving and closing the file, the updated configuration is automatically pushed back into Kubernetes.

One thing you will notice when scaling a cluster is that the servers section takes a list of server configurations. The configuration is setup this way so that services can be scaled independently. For example, if your Couchbase cluster contains 3 nodes running the data service, one node running the query service, and one node running the index service, then the servers section of your configuration would look like this:

```yaml
servers:
  - size: 3
    name: data_services
    services:
      - data
  - size: 1
    name: index_services
    services:
      - index
  - size: 1
    name: query_services
    services:
      - query
```

This configuration creates a 5-node Couchbase cluster with different services running on each pod. If you see that your queries are taking longer than usual, you can scale up your query service independent of the other services by changing the size of the server specification that contains the query service. For example, if you update the size from 1 to 2 and push the new configuration into Kubernetes, the Couchbase Operator will automatically scale up the query service.

## The serverName

The ```serverName``` in the servers section is a very important parameter that is used to track a group of similarly deployed pods. This name is used internally by the operator to track servers of a given type. Changing this name will cause pods that have been deployed in the server group to be removed from the cluster and the operator will deploy new pods tagged with the new ```serverName``` even if no other parameters in the server group have been changed. As a result, we recommend that users never change the ```serverName``` for a particular server group except in the scenario defined below.

#### Changing a Provisioned Node's Services

If you've used Couchbase Server in the past then you have likely found that once a Couchbase Server node is joined to a cluster, the services running on that node cannot change. The Couchbase Operator allows users to edit the configuration in a way that allows easily changing the services by automating the removal of the old nodes and adding in the new nodes with the desired services. To do this, you can change both the ```serverName``` and services parameters during the same configuration update.
