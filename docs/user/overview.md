# Overview

The Couchbase Operator is a Kubernetes native integration that enables you to automate the management of common Couchbase tasks such as the configuration, creation, and scaling of Couchbase clusters. By reducing the complexity of running a Couchbase cluster, it lets you focus on the desired configuration and not worry about the details of manual deployment and lifecycle management.

## How It Works

The Couchbase Operator extends the Kubernetes API by creating a Custom Resource Definition(CRD) and registering a Couchbase specific controller(the Operator) to manage Couchbase clusters. The CRD allows you to define a configuration describing what a Couchbase cluster should look like. For example, a configuration might define a cluster with three nodes, one bucket, and 8GB of memory for the data service. Once the configuration is loaded into Kubernetes, the configuration is passed to the custom Couchbase controller which takes actions to ensure a Couchbase cluster with the specified configuration is provisioned. The controller can also detect updates to the configuration and reacts to changes that occur in the cluster itself. Like all Kubernetes standard built-in resources, the Couchbase Operator doesn’t just manage a single Couchbase cluster but multiple Couchbase clusters across an entire Kubernetes deployment.

## Why Not Just Use StatefulSets

StatefulSets are great for certain use cases, but they don't work that well when running complex software like databases. This is because StatefulSets focus on creating and managing pods, not on managing the software running on them. For example, if you wanted a 4-nodes cluster and deployed Couchbase using StatefulSets, you would get 4 uninitialized Couchbase pods that don't know about each other. It would then be up to you to cluster the nodes together, and this means extra operational tasks.

By creating our own custom Couchbase controller, we can add Couchbase specific knowledge so that as each Couchbase pod is deployed we can properly configure it and join it with the other Couchbase pods in the cluster. It's also important to keep in mind that provisioning a cluster is just one place where having a custom controller helps us to automate tasks. Node failure, ad-hoc scaling, and many other management tasks require Couchbase specific knowledge to manage Couchbase clusters.

## What It Supports

The goal of the Couchbase Operator is to fully manage one or more Couchbase deployments so that you as a developer/SRE don't need to worry about the operational complexities of running Couchbase. This is, however, our 0.7 Beta release, so not all features have been added yet. Below is a list of the management tasks we currently support:

* Cluster provisioning
* Elastic Scalability
* Auto Recovery
