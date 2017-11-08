# Deploying on Azure

These instructions detail how to deploy a on Azure with Kubernetes.  With that complete, the next step is to setup the operator and then create a Couchbase cluster using the operator.

## Azure Kubernetes Service (AKS)

We're making use of [Azure Kubernetes Service (AKS)](https://azure.microsoft.com/en-us/blog/introducing-azure-container-service-aks-managed-kubernetes-and-azure-container-registry-geo-replication/).  AKS is currently in preview.  Because of that, AKS must be explicitly enabled for the subscription you are working on.  Detailed instructions on how to do that are [here](https://blogs.msdn.microsoft.com/alimaz/2017/10/24/enabling-aks-in-your-azure-subscription/).

In short, you need to run the Azure Power Shell command:

    Register-AzurermresourceProvider --ProviderNamespace Microsoft.ContainerService

With that complete you can run an AKS command with the Azure CLI 2.0 to create a cluster:

    az aks create -n MyCluster -g MyResourceGroup

## Deploying the Operator

With this done, you can follow the instructions in [operationsGuide.md](operationsGuide.md).

## Azure Container Instance (ACI)

[Azure Container Instances (ACI)](https://azure.microsoft.com/en-us/blog/announcing-azure-container-instances/) are serverless Docker containers on Azure.  The [ACI connector](https://github.com/Azure/aci-connector-k8s) enables Kubernetes clusters to make use of ACI.

To setup ACI run:
    git clone git@github.com:Azure/aci-connector-k8s.git
    cd aci-connector-k8s
    kubectl create -f examples/example-aci-connector.yaml

It's unclear to me how to point the operator at ACI.

The connector doesn't currently support secrets, so using ACI is not possible at this time.  We're going to have discussions to explore the roadmap here.
