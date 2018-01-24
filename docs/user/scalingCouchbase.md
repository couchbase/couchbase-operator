# Ad-hoc Scaling

The Couchbase Operator allows users to easily scale clusters and services up and down. The handling of individual nodes is defined in the servers section of the Couchbase cluter specification. Below is an example of a definition for a cluster where all nodes run the data, index, search and query services.

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

This configuration specifies that the cluster should contain 3 nodes running the data, index, query, and search services and that the data and index path are each set to /opt/couchbase/var/lib/couchbase/data. To scale these servers the user only needs to change the ```size``` parameter and then push the new configration into Kubernetes to increase or decrease the number of nodes in the cluster. Updating the configuration in Kubernetes can be done by running the following command.

    kubectl replace -f <path to config>

Alternatively you can also edit the configuration of a Couchbase cluster directly by running the command below.

    kubectl edit cbc <name of cluster>

This will pull the current configuration for the cluster from Kubernetes and open it up in an editor. You can then edit the configuration and upon saving and closing the file the edited configuration is automatically pushed back into Kubernetes.

One thing you will notice when scaling a cluster is that the servers section takes a list of server configurations. The configuration is setup this way to allow users to scale services independently. For example, if your Couchbase cluster should contain 3 server running the data service, one server running the query service, and one server running the index service then the servers section of your configuration might look like the one below.

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

This would create a 5 node cluser with different services running on each pod. If you find that your queries are becoming slow then you can scale up your query service independently of the other services by changing the size of the server specification containing the query service. We can for example change it from 1 to 2 and push the new configuration into Kubernetes and the Operator will automatically scale up the query service.