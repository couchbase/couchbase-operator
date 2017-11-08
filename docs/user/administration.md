# Administration

## Accessing the Administration Console:

To access the administration console you can run the following command on any pod, for example:

    kubectl port-forward cb-example-0000 8091:8091

This command will make the Administration available on 127.0.0.1:8091. Note that you will not be able to access other nodes in your cluster since this command will only proxy you through to the specified node.

## Getting Server Logs

The easiest way to get cbcollect logs is to access the administration console and click the "Collect Logs" button in the Logs tab. This will give you the path to the corresponding zip file for each node in the cluster. You can then run a command like the one below against each node in the cluster to collect their logs.

    kubectl cp <namespace>/<pod_name>:<path_to_logs> -c couchbase-server ./logs.zip

An example of what this might look like is below.

    kubectl cp default/cb-example-0000:/opt/couchbase/var/lib/couchbase/tmp/collectinfo-2017-09-28T175135-ns_1@127.0.0.1.zip -c couchbase-server ./logs.zip
