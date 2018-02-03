# Scaling a CouchbaseCluster

Using the Couchbase Operator, you can easily scale clusters and services up and down. The handling of individual nodes is defined in the servers section of the [Couchbase cluster specification](couchbaseClusterConfig.md). Here is an example of a definition for a cluster where all nodes run the data, index, search, and query services.

```yaml
servers:
  - size: 3
    name: all_services
    services:
      - data
      - index
      - query
      - search
    dataPath: /opt/couchbase/var/lib/couchbase/data
    indexPath: /opt/couchbase/var/lib/couchbase/data
```

This configuration specifies that the cluster should contain 3 nodes running the data, index, query, and search services and that the data and index path are each set to /opt/couchbase/var/lib/couchbase/data. To scale these servers, you only need to change the ```size``` parameter and then push the new configuration into Kubernetes to increase or decrease the number of nodes in the cluster. To update the configuration in Kubernetes, run the following command:

```bash
kubectl replace -f <path to config>
```

Alternatively you can also edit the configuration of a Couchbase cluster directly by running the command below.

```bash
kubectl edit cbc <name of cluster>
```

This command pulls the current configuration for the cluster from Kubernetes and opens it up in an editor. You can then edit the configuration, and upon saving and closing the file, the updated configuration is automatically pushed back into Kubernetes.

One thing you will notice when scaling a cluster is that the servers section takes a list of server configurations. The configuration is setup this way so that services can be scaled independently. For example, if your Couchbase cluster contains 3 nodes running the data service, one node running the query service, and one node running the index service, then the servers section of your configuration would look like this:

```yaml
servers:
  - size: 3
    name: data_services
    services:
      - data
    dataPath: /opt/couchbase/var/lib/couchbase/data
    indexPath: /opt/couchbase/var/lib/couchbase/data
  - size: 1
    name: index_services
    services:
      - index
    dataPath: /opt/couchbase/var/lib/couchbase/data
    indexPath: /opt/couchbase/var/lib/couchbase/data
  - size: 1
    name: query_services
    services:
      - query
    dataPath: /opt/couchbase/var/lib/couchbase/data
    indexPath: /opt/couchbase/var/lib/couchbase/data
```

This configuration creates a 5-nodes cluster with different services running on each pod. If you see that your queries are taking longer than usual, you can scale up your query service independent of the other services by changing the size of the server specification containing the query service. For example, update the size from 1 to 2 and push the new configuration into Kubernetes. The Couchbase Operator will automatically scale up the query service.
