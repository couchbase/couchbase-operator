# Deploying with AKS

These instructions detail how to deploy an [AKS](https://azure.microsoft.com/en-us/blog/introducing-azure-container-service-aks-managed-kubernetes-and-azure-container-registry-geo-replication/) cluster and then stand Couchbase up on that cluster with the Kubernetes operator.

## Deploying an AKS Cluster

AKS is currently in preview.  Because of that, AKS must be explicitly enabled for the subscription you are working on.  Detailed instructions on how to do that are [here](https://blogs.msdn.microsoft.com/alimaz/2017/10/24/enabling-aks-in-your-azure-subscription/).

In short, you need to run the Azure Power Shell command:

    Register-AzurermresourceProvider --ProviderNamespace Microsoft.ContainerService

With that complete you can run an AKS command with the Azure CLI 2.0 to create a cluster:

    az aks create -n MyCluster -g MyResourceGroup

## Deploying the Operator

With this done, you can follow the instructions in [operationGuide.md](operationGuide.md).

## ACI

[Azure Container Instances (ACI)](https://azure.microsoft.com/en-us/blog/announcing-azure-container-instances/) are serverless Docker containers on Azure.  The [ACI connector](https://github.com/Azure/aci-connector-k8s) enables Kubernetes clusters to make use of ACI.

The connector doesn't currently support secrets, so using ACI is not possible.  We're going to have discussions to explore the roadmap here.
