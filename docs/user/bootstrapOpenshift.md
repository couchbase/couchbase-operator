# Using OpenShift Templates with Couchbase Operator

### DISCLAIMER
The following automation should only be used in development clusters since it involves granting privileges to the operator that aren't ideal in a production OpenShift cluster.

### About OpenShift Templates

OpenShift offers an application catalog that provides templated deployment of applications into projects. By using templates to deploy applications, common configurations can be parameterized so that you can customize instances of your application directly from the user interface.

## Bootstrapping OpenShift for Couchbase

Since templates can be imported using the standard `oc` cli tool, we've created a bootstrap script to assist with importing Couchbase templates into an OpenShift cluster.

```console
$ wget https://packages.couchbase.com/kubernetes/0.8.1-beta2/openshift-bootstrap.zip
$ unzip openshift-bootstrap.zip
$ cd openshift-bootstrap
$ chmod +x openshift-bootstrap.sh
$ ./openshift-bootstrap.sh
```

*Note*: The bootstrap will also create a utility project named `admin-ct-ctl`. This utility project consists of a controller that detects when a new Couchbase cluster is being created from your template, and ensures that there is an instance of the couchbase-operator within your clusters project.

Refreshing the application catalog page should reveal a new Couchbase template in the catalog. Click the Couchbase template to deploy a cluster into a new or existing project. Upon completion, you will see a link to the Couchbase Server Web Console of the cluster that is being created. This link is also available in the clusters project under **Resources > Routes** for future referencing.

## Troubleshooting/FAQ

In general, it's important to pay attention to the output from the bootstrap script, as any failures there indicate that something has gone wrong.

### The template doesn't appear in the catalog

Templates are stored in the OpenShift namespace. Try listing the templates to ensure that it exists:

```bash
oc get template -n openshift | grep couchbase
```

If the template is listed there, then you may need to wait for the catalog to sync. If not, then you may need to debug your [template service broker](https://docs.openshift.com/container-platform/3.6/architecture/service_catalog/template_service_broker.html) for any issues.

### The Couchbase cluster isn't starting

Make sure that the couchbase-operator was deployed in your project.  You should see it in the overview page of your project within the OpenShift UI. If the operator exists, refer to the documentation about [logs and troubleshooting](logsTroubleshooting.md) of the operator.  If the operator does not exist, then check the `admin-ct-ctl` project.

As previously mentioned, `admin-ct-ctl` is the project that is responsible for deploying the operator into your project. Opening this project should show a deployment named `couchbase-operator-admin-controller`. Check the logs of its pod for debugging any errors that may occur.  If you do not see the deployment there, then it's possible that you did not have permissions to create new projects. Double check the output of the bootstrap script to confirm.

### Routing to the Couchbase Server Web Console isn't working

In some cases, it can take a few minutes for the Couchbase pods to start, which will delay access to the Couchbase Server Web Console. Therefore, it's best to check that the cluster pods are indeed up and running before assuming its availability. If the pods are running, then it's possible that the IP being routed to is not actually publicly accessible, or that it is associated with the wrong interface.

Note that the IP address of the route is based on the response from `oc config`. If it has gathered the wrong IP address you will need to edit the route to your cluster via **Resources > Routes** in the OpenSHift UI.
