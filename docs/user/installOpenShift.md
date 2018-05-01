## Installing on OpenShift

In this setup guide we will walk through the recommended procedure for setting up the Couchbase Operator in an OpenShift project. This guide assumes that you are installing on a new OpenShift cluster, that you have access to a user with cluster-admin privileges (`system:admin` in this guide), and that you have a standard user without cluster-admin privileges (`developer` in this guide). If you have an existing system and need to do a custom setup, then you should be able to modify a few of the parameters in the various commands and configuration files in order to install the Couchbase Operator.

### Create an OpenShift Project

Before installing the Couchbase Operator into OpenShift, you should create a project with the `developer` user. For this guide we will create a project called `operator-example`, and later we will make sure that the `developer` user has the ability to manage the Couchbase Operator and Couchbase clusters in this project. To create a project, run the following commands:

```bash
$ oc login -u developer
$ oc new-project operator-example
$ oc logout
```

### CRD and ClusterRole Creation

The next step in setting up the Couchbase Operator is to install the CouchbaseCluster Custom Resource Definition (CRD). The Couchbase Operator can do this for you automatically, but in OpenShift it's better to do it manually since installing CRDs is a cluster-level operation and requires cluster-level permissions that should not be given to typical users.

To install the CRD, log in as the system:admin user and run the following command:

```bash
$ oc login -u system:admin
$ oc create -f https://packages.couchbase.com/kubernetes/0.8.0-beta2/openshift/crd.yaml
```

*Note*: You only need to install the CRD once. After that, the Couchbase Operator can be created in any OpenShift project.

Once the CRD is installed, you need to create a role that allows the Operator to access the resources it needs to run. Since the Couchbase Operator might run in many different projects, it is best to create a *ClusterRole* because you can assign that role to a *ServiceAccount* in any OpenShift project.

To create the ClusterRole for the Couchbase Operator, run the following command as the system:admin user:

```bash
$ oc create -f https://packages.couchbase.com/kubernetes/0.8.0-beta2/openshift/cluster-role-sa.yaml
```

*Note*: You only need to create the ClusterRole once.

Next, since the `developer` user needs to be able to create and delete the Couchbase cluster, you need to create another ClusterRole that gives permissions to handle management of the CouchbaseCluster resource.

Run the following command to create the ClusterRole for `developer`:

```bash
oc create -f https://packages.couchbase.com/kubernetes/0.8.0-beta2/openshift/cluster-role-user.yaml
```

### Setting Up RBAC for an OpenShift Project

After the ClusterRole is created, you need to create a *ServiceAccount* in the project where you are installing the Couchbase Operator, and then assign the ClusterRole to that ServiceAccount using a *RoleBinding*. (A ServiceAccount is used as opposed to a User for the Couchbase Operator because ServiceAccounts are meant to be used for processes running in OpenShift.)

```bash
$ oc create serviceaccount couchbase-operator --namespace operator-example
$ oc create rolebinding couchbase-operator --clusterrole couchbase-operator --serviceaccount operator-example:couchbase-operator
```

Couchbase Server containers must be run by the root user, so you need to set a policy on the ServiceAccount to allow Couchbase Server pods to be started by the Couchbase Operator. Use the following command to set the policy for the couchbase-operator ServiceAccount:

```bash
$ oc adm policy add-scc-to-user anyuid system:serviceaccount:operator-example:couchbase-operator
```

Now that the ServiceAccount that the Couchbase Operator uses to run has been set up, you need to allow the `developer` user to be able to manage the CouchbaseCluster resource in the `operator-example` project.

```bash
$ oc create rolebinding couchbasecluster --clusterrole couchbasecluster --user developer --namespace operator-example
```

If you want the `developer` user to be able to manage CouchbaseCluster objects in all projects, then you can run the following command instead:

```bash
oc create clusterrolebinding couchbasecluster --clusterrole couchbasecluster --user developer
```

*Note*: To set up a ServiceAccount for the operator in another OpenShift project, repeat the steps above.

### Starting the Operator

At this point you can now create the Couchbase Operator with the `developer` user in the `operator-example` project. To start the Couchbase Operator, run the following command:

```bash
$ oc create -f https://packages.couchbase.com/kubernetes/0.8.0-beta2/openshift/operator.yaml
```

Running this command downloads the Couchbase Operator Docker image that is specified in the ```operator.yaml``` file and creates a *deployment* which manages a single instance of the Couchbase Operator. The Couchbase Operator uses a deployment so that it can restart if the pod it's running in dies.

After you run the ```oc create``` command, it generally takes less than 1 minute for OpenShift to deploy the Operator and for the Operator to be ready to run. Using the following commands, you can check the status of the Operator and see the status of your deployment.

```bash
$ oc get deployments -l app=couchbase-operator
```

If you run this command immediately after the operator is deployed, the output will look something like this:

```bash
NAME                 DESIRED   CURRENT   UP-TO-DATE   AVAILABLE   AGE
couchbase-operator   1         1         1            0           10s
```

In this case, the deployment is called "couchbase-operator". The DESIRED field in the output shows that this deployment will create 1 pod running the Couchbase Operator. The CURRENT field shows that 1 Couchbase Operator pod has been created. However, the AVAILABLE field indicates that that pod is not ready yet since its value is 0 and not 1. This means that the Operator is still establishing a connection to the Kubernetes master node to allow it to get updates on CouchbaseCluster objects. Once the Operator has completed this task it will be able to start managing Couchbase clusters and the status will be shown as AVAILABLE.

You should continue to poll the status of the Operator until your output looks similar to the following output:

```bash
NAME                 DESIRED   CURRENT   UP-TO-DATE   AVAILABLE   AGE
couchbase-operator   1         1         1            1           47s
```
You can also verify that the Couchbase Operator has come up successfully using the following command:

```bash
oc get pods -l app=couchbase-operator
```

If the Couchbase Operator is up and running successfully, the command returns an output where the READY field shows 1/1, such as:

```bash
NAME                                  READY   STATUS   RESTARTS   AGE
couchbase-operator-1917615544-t5mhp   1/1     Running  0          57s
```

You can also check the logs to confirm that the Couchbase Operator is up and running. Look for the message: "starting couchbaseclusters controller".

```bash
$ oc logs couchbase-operator-1917615544-t5mhp
time="2018-04-25T03:01:56Z" level=info msg="Obtaining resource lock" module=main
time="2018-04-25T03:01:56Z" level=info msg="Starting event recorder" module=main
time="2018-04-25T03:01:56Z" level=info msg="Attempting to be elected the couchbase-operator leader" module=main
time="2018-04-25T03:02:13Z" level=info msg="I'm the leader, attempt to start the operator" module=main
time="2018-04-25T03:02:13Z" level=info msg="Creating the couchbase-operator controller" module=main
time="2018-04-25T03:02:13Z" level=info msg="Event(v1.ObjectReference{Kind:\"Endpoints\", Namespace:\"default\", Name:\"couchbase-operator\", UID:\"9b86c750-47e7-11e8-866e-080027b2a68d\", APIVersion:\"v1\", ResourceVersion:\"23482\", FieldPath:\"\"}): type: 'Normal' reason: 'LeaderElection' couchbase-operator-75ddfdbdb5-bz7ng became leader" module=event_recorder
time="2018-04-25T03:02:13Z" level=info msg="starting couchbaseclusters controller"
```

## Uninstall

Uninstalling the Couchbase Operator is a two-step process.

1. Delete the Couchbase Operator

   You can delete the Couchbase Operator as the `developer` or `system:admin` user.

   ```bash
   $ kubectl delete deployment couchbase-operator
   ```

   *Note*: Deleting the Couchbase Operator from a namespace will not remove or affect any Couchbase pods in your Kubernetes cluster.

2. Delete the CRD

    You can only delete the CRD as the `system:admin` user.

    Make sure the Couchbase Operator has been deleted from all other OpenShift projects in the cluster before you delete the CRD. Once the Couchbase Operator has been deleted, run the following command to delete the CRD:

   ```bash
   $ kubectl delete crd couchbaseclusters.couchbase.database.couchbase.com
   ```
