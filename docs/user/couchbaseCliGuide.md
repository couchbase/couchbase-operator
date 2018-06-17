# Accessing the Couchbase CLI

The Couchbase command-line tool `couchbase-cli` can be used to interact with the cluster for performing one off administration tasks such as creating RBAC users and initiating cbcollect info tasks. If your clusters Web Console is exposed, you can use the CLI tool from any machine that can access the IP address of your Kubernetes cluster. Alternatively, couchbase-cli can be run as a Kubernetes job within the Kubernetes cluster.

## couchbase-cli to Web Console

Prerequisites:

* See [Accessing the Couchbase Web Console](adminConsoleAccess.md) for exposing the Web Console.
* Download the couchbase-cli to your client machine:  https://github.com/couchbase/couchbase-cli

Use the `describe` command to retrieve the Web Console port:

On Kubernetes:

```console
$ kubectl describe cbc cb-example |  grep "Admin Console Port:"
   Admin Console Port:		32486
```

On OpenShift:

```console
$ oc describe cbc cb-example |  grep "Admin Console Port:"
   Admin Console Port:		32486
```

You can now use couchbase-cli to administer the cluster.  For example, adding an RBAC user:

```console
$ ./couchbase-cli user-manage -c 192.168.99.100:32486 -u Administrator -p password --rbac-username default --rbac-password password --roles admin --auth-domain local --set
SUCCESS: RBAC user set
```

Note that the cluster is being managed by the Couchbase Operator.  Therefore, certain administration tasks such as adding or removing nodes and buckets can be reverted by the Operator.

## couchbase-cli as Kubernetes Job

The most secure way to use the couchbase-cli with your Kubernetes cluster is to run as batch jobs. This option allows you to hide the user name and password of your cluster by allowing the use of Secrets. For example, you can run a collect info job using the same secret provided in the cluster spec:

```console
$ kubectl create -f https://packages.couchbase.com/kubernetes/0.8.1-beta2/couchbase-cli-collect-logs.yaml
job "collect-info" created

# check status
$ kubectl get job collect-info
NAME           DESIRED   SUCCESSFUL   AGE
collect-info   1         1            10s
```

On OpenShift, use the following command:

```console
$ oc create -f https://packages.couchbase.com/kubernetes/0.8.1-beta2/couchbase-cli-collect-logs.yaml
```
