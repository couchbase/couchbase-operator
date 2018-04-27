**DISCLAIMER:**  The following automation should only be used in development clusters since it involves granting privileges to the operator that aren't ideal in a production OpenShift cluster.

### OpenShift Templates

OpenShift offers an application catalog which provides templated deployment of applications into projects.  By using templates to deploy applications, common configurations can be parameterized to customize instances of your application directly from the user interface.

### Bootstrapping OpenShift For Couchbase

Since templates can be imported using the standard `oc` cli tool, we've created a bootstrap script to assist with importing couchbase templates into your OpenShift cluster.  Note that the bootstrap will also create a utility project named `admin-ct-ctl`.  This utility project consists of a controller that detects when a new couchbase cluster is being created from your template and ensures that there is an instance of the couchbase-operator within your clusters project.

```console
$ wget https://packages.couchbase.com/kubernetes/0.8.0/k8s/openshift-bootsrap.sh
$ chmod +x openshift-bootsrap.sh
$ ./openshift-bootsrap.sh
```

Refreshing the application catalog page should reveal a new couchbase template in the catalog.  Click the couchbase template to deploy a cluster into a new or existing project.  Upon completion you will see a link to the admin console of the cluster being created.  This link is also available in the clusters project via Resources -> Routes for future referencing.

### Troubleshooting/FAQ

In general, it's important to pay attention to the output from the bootstrap script as any failures there indicate that something has gone wrong.

**The template doesn't appear in the catalog >>**  Templates are stored in the openshift namespace.  Try listing the templates and ensuring that it exists:
```bash
oc get template -n openshift | grep couchbase
```
If the template is listed there then you may need to wait for the catalog to sync.  If not, then you may need to debug your [template service broker](https://docs.openshift.com/container-platform/3.6/architecture/service_catalog/template_service_broker.html) for any issues.

**Couchbase Cluster Isn't starting >>**  Make sure that the couchbase-operator was deployed in your project.  You should see it in the overview page of your project within the OpenShift UI.  If the operator exists, refer to the documentation about [logs and troubleshooting](logsTroubleshooting.md) of the operator.  If the operator does not exist, then check the `admin-ct-ctl` project.  As previously mentioned, this is the project that is responsible for deploying the operator into your project.  Opening the admin-ct-ctl project should show a deployment named `couchbase-operator-admin-controller`.  Check the logs of its Pod for debugging any errors that may occur.  If you do not see the deployment there, then it's possible that you did not have permissions to create new projects, double check the output of the bootstrap script to confirm.


**Routing to Admin Console isn't working >>**  In some cases it can take a few minutes for the couchbase pods to start which will delay access to the Admin Console.  Therefore, it's best to check that the cluster pods are indeed up and running before assuming its availability.  If the pods are running, then it's possible that the IP being routed to is not actually publicly accessible, or is associated with the wrong interface.  Note that the IP address of the route is based on the response from `oc config`. If it has gathered the wrong IP address you will need to edit the route to your cluster via Resources -> Routes.
