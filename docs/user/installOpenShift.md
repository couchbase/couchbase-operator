## Installing on OpenShift

In this setup guide we will walk through the recommended procedure for setting up the Couchbase Operator in an OpenShift project. This guide assumes that you are installing on a new OpenShift cluster, that you have access to a user with cluster-admin privileges (we use `system:admin` in this guide), and you have a standard user without cluster-admin privileges  (we use `developer` in this guide). If you have an existing system and need to do a custom setup then you should be able to modify a few of the parameters in the various commands and configuration files in order to install the Couchbase Operator.

### Create an OpenShift Project

Before installing the Couchbase Operator into OpenShift you should create a project with the `developer` user. For this guide we will create a project called `operator-example` and later we will make sure that the `developer` user has the ability to manage the Couchbase Operator and Couchbase clusters in this project. To create a project run the following commands.

```bash
$ oc login -u developer
$ oc new-project operator-example
$ oc logout
```

### CRD and ClusterRole Creation

The first step to setup the Couchbase Operator is installing the CouchbaseCluster Custom Resource Definition (CRD). The Couchbase Operator can do this for you automatically, but in OpenShift it's better to do it manually since installing CRDs is a cluster-level operation and requires cluster-level permissions that should not be given to typical users. Installing the CRD also only needs to be done once and then the Couchbase Operator can be created in any OpenShift project. To install the CRD log in as the system:admin user and run the command below:

```bash
$ oc login -u system:admin
$ oc create -f https://packages.couchbase.com/kubernetes/0.8.0/openshift/crd.yaml
```

Once the CRD is installed we need to create a role with the specific permissions that the Couchbase Operator needs to run. Since the Couchbase Operator might run in many different projects it is best to create a ClusterRole because we will be able to assign that role to a ServiceAccount in any OpenShift project. To create the ClusterRole for the Couchbase Operator you can run the command below as the system:admin user. Also note that this role only needs to be created once.

```bash
$ oc create -f https://packages.couchbase.com/kubernetes/0.8.0/openshift/cluster-role-sa.yaml
```

We also need to allow our `developer` user to be able to create and delete Couchbase cluster so we need to create another ClusterRole that give permissions to handle managing CouchbaseCluster resource. This can be done with the command below:

```bash
oc create -f https://packages.couchbase.com/kubernetes/0.8.0/openshift/cluster-role-user.yaml
```

### Setting Up RBAC For An OpenShift Project

Next we need to create a ServiceAccount in the OpenShift project where we want to create the Couchbase Operator and then assign the ClusterRole we just created to a ServiceAccount using a RoleBinding. We use a ServiceAccount as opposed to a User for the Couchbase Operator because ServiceAccounts are meant to be used for processes running in OpenShift. Earlier we created the `operator-example` project with the `developer` user so we will create the ServiceAccount in this project. We can do this by running the commands below.

```bash
$ oc create serviceaccount couchbase-operator --namespace operator-example
$ oc create rolebinding couchbase-operator --clusterrole couchbase-operator --serviceaccount operator-example:couchbase-operator
```

Couchbase Server containers also require are run with the root user so we need to set a policy on the ServiceAccount to allow Couchbase Server pods to be started by the Couchbase Operator. This can be added to the couchbase-operator ServiceAccount by running the following command..

```bash
$ oc adm policy add-scc-to-user anyuid system:serviceaccount:operator-example:couchbase-operator
```

Now that the ServiceAccount the Couchabse Operator uses to run has been set up we need to allow the `developer` user to be able to manage the CouchbaseClusters resource in the `operator-example` ptoject. This can be done by running the command below.

```bash
$ oc create rolebinding couchbasecluster --clusterrole couchbasecluster --user developer --namespace operator-example
```

If you want the `developer` user to be able to manage CouchbaseCluster objects in all projects then the command below can be run instead.

```bash
oc create clusterrolebinding couchbasecluster --clusterrole couchbasecluster --user developer
```

*Note: To setup a ServiceAccount for the operator in another OpenShift project repeat the steps above.*

### Starting the Operator

Creating the Couchbase Operator can be done with the `developer` user in the `operator-example` project. To start the Couchbase Operator run the commands below:

```bash
$ oc create -f https://packages.couchbase.com/kubernetes/0.8.0/openshift/operator.yaml
```

Running this command will download the Couchbase Operator Docker image specified in the operator.yaml file and create a deployment which manages a single instance of the Couchbase Operator. We use a deployment because we want the Couchbase Operator to be able to restart if the pod it's running in dies.

After you run the ```oc create``` command above, it generally takes less than 1 minute for OpenShift to deploy the Operator and for the Operator to be ready to run. Using one of the following commands, you can check the status of the Operator and see the status of your deployment.

```bash
$ oc get deployments -l app=couchbase-operator
```

If you run this command immediately after the operator is deployed, the output will look something like this:

```bash
NAME                 DESIRED   CURRENT   UP-TO-DATE   AVAILABLE   AGE
couchbase-operator   1         1         1            0           10s
```

In this case, the deployment is called "couchbase-operator". The DESIRED field in the output shows that this deployment will create 1 pod running the Couchbase Operator. The CURRENT field shows that 1 Couchbase Operator pod has been created. However, the AVAILABLE field indicates that that pod is not ready yet as its value is 0 and not 1. This means that the Operator is atill establishing a connection to the OpenShift master node to allow it to get updates on CouchbaseCluster objects. Once the Operator has completed this task it will be able to start managing Couchbase clusters and the status will be shown as AVAILABLE. You should continue to poll the status of the Operator until your output looks similar to the following output:

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

If you examine the logs using the following command, the message: "starting couchbaseclusters controller"" indicates that the Couchbase Operator is up and running.

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

Uninstalling the Couchbase Operator is a two-step process. The Operator running in a particular namespace can be uninstalled by deleting it and this can be done with either the `developer` or `system:admin` user. Uninstalling the Couchbase Operator in a namespace will not remove or affect any Couchbase pods in your Kubernetes cluster.

```bash
$ oc delete deployment couchbase-operator
```

The CRD on the other hand must be removed by the `system:admin` user. When removing the CRD you should be sure that the Couchbase Operator has been uninstalled from all other OpenShift projects in the cluster.

```bash
$ oc delete crd couchbaseclusters.couchbase.database.couchbase.com
```
