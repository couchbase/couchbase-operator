# Prerequisites And Setup

In order to run the Couchbase Operator all you need is a running Kubernetes or OpenShift cluster. We support the following releases:

* Kubernetes 1.7+
* OpenShift 3.7+

If you do not have access to a Kubenetes cluster and you plan on using the Couchbase Operator for development then we recommend using either MiniKube (Single node Kubernetes) or MiniShift (Single Node OpenShift). Both of these products are both much easier to install and deploy compared to setting up and running an actual Kubernetes or Openshift cluster.

Preparing the cluster to run the Couchbase operator may require setting up the proper RBAC settings in your Kubernetes cluster. Before moving forward we recommend reading deployment guides for our various deployment options.

* [Kubernetes](rbacKubernetes.md)
* [OpenShift](rbacOpenshift.md)

## Setup

Before you can start deploying Couchbase clusters on either Kubernetes or OpenShift you must install the Couchbase Operator into your Kubernetes/Openshift deployment. This can be done by running the command below:

```bash
$ kubectl create -f https://packages.couchbase.com/kubernetes/beta/operator.yaml
```

Running this command will download the Couchbase Operator docker image and create a deployment which manages a single instance of the Couchbase Operator. We use a deployment becuase we want the Couchbase Operator to be restarted if the pod it's running in dies. When the Couchbase Operator pod is started for the first time it will register the CouchbaseCluster CRD with Kubernetes and register a controller which listens for updates to CouchbaseCluster configurations.

>Note: This command will create the Couchbase Operator in the default namespace. If you need to deploy the Operator in another namespace then download this file and modify the metadata.namespace field to reflect the namespace you want to install the operator in.

After you run the ```kubectl create``` command it generally takes less than 1 minute for Kubernetes to deploy the Operator and for the Operator to be ready to run. We can check the status of the operator using kubectl to see the status of our deployment. This can be done by running the command below.

```bash
$ kubectl get deployments
```

If you run this command immediately after you run the ```kubectl create``` command then the output will look something like this.

```bash
NAME                 DESIRED   CURRENT   UP-TO-DATE   AVAILABLE   AGE
couchbase-operator   1         1         1            0           10s
```

Our deployment is the "couchbase-operator" deployment and from the output above we can see in the DESIRED field that this deployment will create 1 pod running the Couchbase Operator. The CURRENT field tells us that 1 Couchbase Operator pod has been created, but the AVAILABLE field tells us that that pod is not ready yet since its value is 0 and not 1. This means the operator is currently ensuring its CRD is registered with Kubernetes and is establishing a connection to the Kubernetes master to allow it to get updates on CouchbaseCluster objects. Once the Operator is finished with these tasks it will start managing Couchbase Cluster and be marked as AVAILABLE. You should continue to poll the status of the Operator until your output looks similar to the output below.

```bash
NAME                 DESIRED   CURRENT   UP-TO-DATE   AVAILABLE   AGE
couchbase-operator   1         1         1            1           47s
```

## Uninstall

To uninstall the Couchbase Operator run the following commands.

```bash
$ kubectl delete -f https://packages.couchbase.com/kubernetes/beta/operator.yaml
$ kubectl delete crd couchbaseclusters.couchbase.database.couchbase.com
```

Uninstalling the Couchbase Operator will not remove or affect any Couchbase pods in your Kubernetes cluster.