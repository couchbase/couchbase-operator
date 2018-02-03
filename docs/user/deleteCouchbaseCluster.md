# Deleting a Couchbase Cluster

To delete a Couchbase Cluster you can either use the configuration file that you created the cluster with or you can delete the cluster directly. If your configuration file is named my-cluster.yaml then you can delete the cluster by running the following command.

    kubectl delete -f my-cluster.yaml

If your cluster is named my-cluster then you can also delete the cluster by running the following command.

    kubectl delete couchbasecluster my-cluster
