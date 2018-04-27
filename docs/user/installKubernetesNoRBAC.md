## Installing on Kubernetes Without RBAC

In this setup guide we will walk through the recommended procedure for setting up the Couchbase Operator in a Kubernetes cluster without RBAC enabled. This guide is recommended mainly for development clusters or MiniKube. Running a Kubernetes cluster without RBAC is not recommended in production

### Starting the Operator

To install the Couchbase Operator run the command below.

```bash
$ kubectl create -f https://packages.couchbase.com/kubernetes/0.8.0/k8s/operator.yaml
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
