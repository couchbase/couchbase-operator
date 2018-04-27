## Installing on Kubernetes With RBAC

In this setup guide we will walk through the recommended procedure for setting up the Couchbase Operator in a Kubernetes cluster with RBAC enabled. This guide assumes that you are installing on a new Kubernetes cluster, but if you have an existing system and need to do a custom setup then you should be able to modify a few of the parameters in the various commands and configuration files in order to install the Couchbase Operator.

### Creating a ClusterRole

The first step to installing the Couchbase Operator is creation of a ClusterRole that allows the Operator to access of the resources it needs to run. Since the Couchbase Operator might run in many different namespaces it is best to create a ClusterRole because we will be able to assign that role to a ServiceAccount in any namespace. To create the ClusterRole for the Couchbase Operator you can run the command below. Also note that this role only needs to be created once.

```bash
$ kubectl create -f https://packages.couchbase.com/kubernetes/0.8.0/k8s-rbac/cluster-role.yaml
```

### Creating a ServiceAccount

After the ClusterRole is created we need to define a ServiceAccount in the namespace where we want to install the Couchbase Operator and then assign the ClusterRole we just created to the ServiceAccount using a RoleBinding. In this guide we will use the `default` namespace to create our ServiceAccount.

```bash
$ kubectl create serviceaccount couchbase-operator --namespace default
$ kubectl create clusterrolebinding couchbase-operator --clusterrole couchbase-operator --serviceaccount default:couchbase-operator
```

*Note:* If you would prefer to use a RoleBinding instead of a ClusterRoleBinding then the CRD needs to be installed separately from the Couchbase Operator. This can be accomplished by removing the -create-crd argument from the operator.yaml file and uploading the CouchbaseCluster CRD separately.

### Starting the Operator

Now that we have setup a service account with the appropriate permissions we can start the Couchbase Operator by runnning the command below.

```bash
$ kubectl create -f https://packages.couchbase.com/kubernetes/0.8.0/k8s-rbac/operator.yaml
```

Running this command will download the Couchbase Operator Docker image specified in the operator.yaml file and create a deployment which manages a single instance of the Couchbase Operator. We use a deployment because we want the Couchbase Operator to be able to restart if the pod it's running in dies.

After you run the ```kubectl create``` command above, it generally takes less than 1 minute for Kubernetes to deploy the Operator and for the Operator to be ready to run. Using one of the following commands, you can check the status of the Operator and see the status of your deployment.

```bash
$ kubectl get deployments -l app=couchbase-operator
```

If you run this command immediately after the operator is deployed, the output will look something like this:

```bash
NAME                 DESIRED   CURRENT   UP-TO-DATE   AVAILABLE   AGE
couchbase-operator   1         1         1            0           10s
```

In this case, the deployment is called "couchbase-operator". The DESIRED field in the output shows that this deployment will create 1 pod running the Couchbase Operator. The CURRENT field shows that 1 Couchbase Operator pod has been created. However, the AVAILABLE field indicates that that pod is not ready yet as its value is 0 and not 1. This means that the Operator is atill establishing a connection to the Kubernetes master node to allow it to get updates on CouchbaseCluster objects. Once the Operator has completed this task it will be able to start managing Couchbase clusters and the status will be shown as AVAILABLE. You should continue to poll the status of the Operator until your output looks similar to the following output:

```bash
NAME                 DESIRED   CURRENT   UP-TO-DATE   AVAILABLE   AGE
couchbase-operator   1         1         1            1           47s
```
You can also verify that the Couchbase Operator has come up successfully using the following command:

```bash
oc get pods -l app=couchbase-operator
```

If the Couchbase Operator is up and running successfully, the command returns an output where the status is Running and Ready is 1/1, such as:

```bash
NAME                                  READY   STATUS   RESTARTS   AGE
couchbase-operator-1917615544-t5mhp   1/1     Running  0          57s
```

If you examine the logs using the following command, the message: "CRD initialized, listening for events... module=controller" indicates that the Couchbase Operator is up and running.

```bash
$ oc logs couchbase-operator-1917615544-t5mhp
time="2018-04-25T03:01:56Z" level=info msg="Obtaining resource lock" module=main
time="2018-04-25T03:01:56Z" level=info msg="Starting event recorder" module=main
time="2018-04-25T03:01:56Z" level=info msg="Attempting to be elected the couchbase-operator leader" module=main
time="2018-04-25T03:02:13Z" level=info msg="I'm the leader, attempt to start the operator" module=main
time="2018-04-25T03:02:13Z" level=info msg="Creating the couchbase-operator controller" module=main
time="2018-04-25T03:02:13Z" level=info msg="Event(v1.ObjectReference{Kind:\"Endpoints\", Namespace:\"default\", Name:\"couchbase-operator\", UID:\"9b86c750-47e7-11e8-866e-080027b2a68d\", APIVersion:\"v1\", ResourceVersion:\"23482\", FieldPath:\"\"}): type: 'Normal' reason: 'LeaderElection' couchbase-operator-75ddfdbdb5-bz7ng became leader" module=event_recorder
time="2018-04-25T03:02:13Z" level=info msg="CRD initialized, listening for events..." module=controller
time="2018-04-25T03:02:13Z" level=info msg="starting couchbaseclusters controller"
```

## Uninstall

Uninstalling the Couchbase Operator is a two-step process. The Operator running in a particular namespace can be uninstalled by deleting it.

```bash
$ kubectl delete deployment couchbase-operator
```

The CRD should also be deleted to complete the uninstall, but this should only be done once all instances of the Couchbase Operator have been removed from the cluster. Once all Couchbase Operator instances have been remove the following command should be run.

```bash
$ kubectl delete crd couchbaseclusters.couchbase.database.couchbase.com
```
