# Prerequisites and Setup

In order to run the Couchbase Operator all you need is a running Kubernetes or OpenShift cluster. We support the following releases:

* Kubernetes 1.8+
* OpenShift 3.9+

If you do not have access to a Kubernetes cluster and plan on using the Couchbase Operator for development, we recommend using either MiniKube (single-node Kubernetes cluster) or MiniShift (single-node OpenShift cluster). Both these products are much easier to install and deploy when compared to setting up and running an actual Kubernetes or OpenShift cluster.
For installation instructions, see [MiniKube](https://kubernetes.io/docs/tasks/tools/install-minikube/) or [MiniShift](https://docs.openshift.org/latest/minishift/getting-started/installing.html).

Preparing the cluster to run the Couchbase Operator may require setting up proper RBAC settings in your Kubernetes cluster. Before moving forward, you should read the deployment guides for for the various supported platforms.

* [Installing in Kubernetes](installKubernetes.md)
* [Installing in Kubernetes (without RBAC)](installKubernetesNoRBAC.md)
* [Installing in OpenShift](installOpenShift.md)
* [Bootstrapping OpenShift](bootstrapOpenShift.md)
  * *Bootstrap automation should only be used in development clusters since it involves granting privileges to the operator that aren't ideal in a production OpenShift cluster.*

For more information about how the Couchbase Operator Deployment is defined, as well as documentation about the fields that you might want to customize, see the [Couchbase Operator Configuration](operatorConfig.md).
