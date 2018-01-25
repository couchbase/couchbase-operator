# Updating a Couchbase Cluster

Changes made to a cluster should never be done manually since the operator will attempt to revert the changes made if they do not match what is in the configuration. The cluster configuration should be updated using ```kubectl``` and we recommend using either the ```kubectl replace``` or ```kubectl edit``` command.

To use ```kubectl replace``` first edit a field in the Couchbase cluster configuration which is stored on your local disk and then run the command below.

    kubectl replace -f <path to updated config>

You can also edit the Couchbase cluster configuration directly using the ```kubectl edit``` command. If your cluster name is "cb-example"" then you can use the following command to edit the configuration

    kubectl edit cbc cb-example

This will open a text editor with the current cb-example configuration. You can then make edits and save and close the text editor. Upon closing the text editor the edits will be pushed back to Kubernetes.

It should also be noted that some fields in the Couchbase cluster configuration cannot be edited after the cluster is created. We have noted these fields in the [Couchbase Configuration](couchbaseClusterConfig.md) documentation. In the future Kubernetes will allow us to validate these fields and catch mistakes, but unfortuantley this validation feature is  still a Kubernetes alpha feature.