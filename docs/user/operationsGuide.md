# Operations Guide

## Install the Couchbase Operator

OPTIONAL: If you are using a private registry (as is likely the case for the current pre release state of this), make sure to deploy the registry key secret first.  Create the 'registrykey' secret:

    kubectl apply -f example/registrykeysecret.yaml

Create deployment: In order to run a couchbase cluster you'll first need to deploy a custom resource definition
which will allow for the creation of a 'couchbasecluster' type within the kubernetes cluster.

    kubectl create -f example/deployment.yaml

Create the 'cb-example-auth' secret

    kubectl apply -f example/secret.yaml

Now objects with "kind: CouchbaseCluster" can be created.  Create a cluster:

    kubectl apply -f example/couchbase-cluster.yaml

Couchbase operator will automatically create a Kubernetes Custom Resource Definition (CRD).  You can check that with the command:

    kubectl get couchbasecluster

With these steps complete, you can follow the steps in the [Administration Guide](administrationGuide.md) to connect to the cluster.
