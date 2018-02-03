# Prerequisites and Setup

In order to run the Couchbase Operator all you need is a running Kubernetes or OpenShift cluster. We support the following releases:

* Kubernetes 1.7+
* OpenShift 3.7+

If you do not have access to a Kubernetes cluster and plan on using the Couchbase Operator for development, we recommend using either MiniKube (single-node Kubernetes cluster) or MiniShift (single-node OpenShift cluster). Both these products are much easier to install and deploy when compared to setting up and running an actual Kubernetes or Openshift cluster.

Preparing the cluster to run the Couchbase Operator may require setting up proper RBAC settings in your Kubernetes cluster. Before moving forward, we recommend reading the deployment guides for our various deployment options.

* [Kubernetes RBAC](rbacKubernetes.md)
* [OpenShift RBAC](rbacOpenshift.md)

## Setup

Before you can start deploying Couchbase clusters on either Kubernetes or OpenShift, you must install the Couchbase Operator into your Kubernetes/Openshift deployment. To do so, run the following command:

```bash
$ kubectl create -f https://packages.couchbase.com/kubernetes/beta/operator.yaml
```

Running this command will download the Couchbase Operator docker image and create a deployment which manages a single instance of the Couchbase Operator. We use a deployment because we want the Couchbase Operator to be able to restart if the pod it's running in dies. When the Couchbase Operator pod is started for the first time, it registers the CouchbaseCluster Custom Resource Definition(CRD) with Kubernetes and registers a controller which listens for updates to the CouchbaseCluster configurations.

>Note: This command creates the Couchbase Operator in the default namespace. To deploy the Operator in another namespace, download this file and modify the metadata.namespace field to reflect the namespace you want to install the Operator in.

After you run the ```kubectl create``` command, it generally takes less than 1 minute for Kubernetes to deploy the Operator and for the Operator to be ready to run. You can check the status of the Operator using ```kubectl``` to see the status of your deployment by running the following command:

```bash
$ kubectl get deployments
```

If you run this command immediately after you run the ```kubectl create``` command, then the output will look something like this:

```bash
NAME                 DESIRED   CURRENT   UP-TO-DATE   AVAILABLE   AGE
couchbase-operator   1         1         1            0           10s
```

In this case, the deployment is called "couchbase-operator". The DESIRED field in the output shows that this deployment will create 1 pod running the Couchbase Operator. The CURRENT field shows that 1 Couchbase Operator pod has been created. However, the AVAILABLE field indicates that that pod is not ready yet as its value is 0 and not 1. This means that the Operator is currently ensuring its CRD is registered with Kubernetes and is establishing a connection to the Kubernetes master to allow it to get updates on CouchbaseCluster objects. Once the Operator is finished with these tasks, it will start managing the Couchbase cluster and the status will be shown as AVAILABLE. You should continue to poll the status of the Operator until your output looks similar to the following output:

```bash
NAME                 DESIRED   CURRENT   UP-TO-DATE   AVAILABLE   AGE
couchbase-operator   1         1         1            1           47s
```

## Uninstall

To uninstall the Couchbase Operator run the following commands:

```bash
$ kubectl delete -f https://packages.couchbase.com/kubernetes/beta/operator.yaml
$ kubectl delete crd couchbaseclusters.couchbase.database.couchbase.com
```

Uninstalling the Couchbase Operator will not remove or affect any Couchbase pods in your Kubernetes cluster.
