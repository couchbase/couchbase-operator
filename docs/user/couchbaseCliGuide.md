# Couchbase Client

The Couchbase command-line tool `couchbase-cli` can be used to interact with the cluster for performing one-off administration tasks such as creating rbac users and initiating cbcollect info tasks.  If your clusters administration console is exposed you can use the cli tool from any machine that can access the IP address of your kubernetes cluster.  Alternertavily couchbase-cli can be ran as a kubernetes job within the kubernetes cluster.

**couchbase-cli to administration console**

Requires:
* See [administrationGuide](administrationGuide.md) for exposing the administration console.
* Download the couchbase-cli to your client machine:  https://github.com/couchbase/couchbase-cli

Use describe command to get the administration console port:

```console
$ kubectl describe cbc cb-example |  grep "Admin Console Port:"
   Admin Console Port:		32486
```

Now couchbase-cli can be used with to administer the cluster.  For example, adding an rbac user:
```console
$ ./couchbase-cli user-manage -c 192.168.99.100:32486 -u Administrator -p password --rbac-username default --rbac-password password --roles admin --auth-domain local --set
SUCCESS: RBAC user set
```
Note that the cluster is being managed by the couchbase-operator.  Therefore, certain administration tasks such as add/remove nodes and buckets will be reverted by the operator.


**couchbase-cli as kubernetes job**

The most secure way to use the couchbase-cli with your kubernetes cluster is to run as batch jobs.  This option allows you to hide the username and password of your cluster by allowing the use of secrets.  For example, you can run a collect info job using the same secret provided in the cluster spec:

```console
$ kubectl create -f https://github.com/couchbase/couchbase-operator/blob/master/example/couchbase-cli-create-user.yaml
job "collect-info" created

# check status
$ kubectl get job collect-info
NAME           DESIRED   SUCCESSFUL   AGE
collect-info   1         1            10s
```
