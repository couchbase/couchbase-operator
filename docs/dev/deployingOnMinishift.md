# Deploying on MiniShift

These instruction detail how to install the Couchbase operator and create a Couchbase cluster using MiniShift which is the single note version of Red Hat OpenShift. We intend the steps in this document to be used for the purposes of developing the Couchbase Operator and testing it on OpenShift.

The first thing that we need to do is install MiniShift. If you're running on OS X the easiest way to do this is to install through Homebrew. This can be done by running the command below:

    $ brew cask install minishift

This will get you the latest version of MiniShift. You will also need Virtual Machine software and we recommend VirtualBox. If you don't have this then you can install with Homebrew.

    $ brew cask install virtualbox

Now that you hovae the software installed you can bring up Openshift by running the following command:

    $ minishift start --vm-driver=virtualbox --openshift-version=v3.7.0

Note that VirtualBox and OpenShift 3.7 are not the defaults (at least at the time of this writing) so you need to modify the default slightly. If there is a released version of Openshift after 3.7 the we recommend using the newer version.

Next we need to configure our machine so that we can use the 'oc' command and we also need to make sure we set our docker environment to the one that's running inside of MiniShift. Setting the docker environment is important for development because it allows us to push our development containers directly into MiniShift so that we can test them.

    $ eval $(minishift oc-env)
    $ eval $(minishift docker-env)

After this is completed we need to make sure that we setup the ServiceAccounts inside of OpenShift correctly. To do this we need to use the oc command to logout our current user (When MiniShift starts it logs you in as the developer user) and then log in as the administrator. Once we're logged in we can update the security policies.

    $ oc logout
    $ oc login -u system:admin
    $ oc new-project myproject
    $ oc adm policy add-cluster-role-to-user cluster-admin -z default -n myproject
    $ oc adm policy add-scc-to-user anyuid system:serviceaccount:myproject:default

The first policy update (add-cluster-role-to-user) allows us to be able to deploy the Operator. It turns out that the Operator needs some non-standard permissions to create CRD's and watch for events on CouchbaseCluster objects and this allows us to do that. The second policy update (add-scc-to-user) allows our Couchbase containers to start Couchbase as root.

The last thing that we need to do is build our development container. Since we set our local docker environment to the one in MiniShift our container will automatically be pushed there.

    $ make container

Now we're all setup and ready to go. We can install our the Operator, create a Secret, and then deploy our first Couchbase Cluster.

    $ kubectl create -f example/deployment.yaml
    $ kubectl create -f example/secret.yaml
    $ kubectl create -f example/couchbase-cluster.yaml
