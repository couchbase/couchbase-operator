# Couchbase Operator

This is a Kubernetes [operator](https://coreos.com/operators) for Couchbase.  The operator is a [custom resource definition](https://kubernetes.io/docs/concepts/api-extension/custom-resources/).  A description of building the Docker image and the Operator are available [here](docs/dev/development.md).  Typical users will not need to follow these steps.

As a user of the operator, the first step is to create a cluster.  Instructions for doing that are available on:
* [CentOS](docs/user/deployingOnCentOS.md)
* [Azure Kubernetes Service (AKS)](docs/user/deployingWithAKS.md)  

Once you have a Kuberentes cluster, you can follow the instructions in the:
* [Operations Guide](docs/user/operationsGuide.md) to create a Couchbase cluster.  
* With that complete, the [Administration Guide](docs/user/administrationGuide.md) provides detail on connecting to the cluster.
