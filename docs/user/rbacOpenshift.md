## OpenShift RBAC

The Couchbase Operator needs some special permissions in order to interact with the Kubernetes master. These permissions need to be set for each project that the Couchbase Operator is used in. Note in the example below the project name is "myproject". If you project name is different then replace "myproject" with your project name.

    $ oc login -u system:admin
    $ oc new-project myproject
    $ oc adm policy add-cluster-role-to-user cluster-admin -z default -n myproject
    $ oc adm policy add-scc-to-user anyuid system:serviceaccount:myproject:default

The first policy update (add-cluster-role-to-user) allows us to be able to deploy the Operator. It turns out that the Operator needs some non-standard permissions to create CRD's and watch for events on CouchbaseCluster objects and this allows us to do that. The second policy update (add-scc-to-user) allows our Couchbase containers to start Couchbase as root.