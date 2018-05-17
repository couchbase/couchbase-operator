# Ensuring Valid CouchbaseCluster Configurations with cbopctl

`cbopctl` is a command line tool that is provided with the Couchbase Operator that implements a custom subset of the `kubectl` and `oc` commands to ensure that CouchbaseCluster objects are valid configurations before they are uploaded to Kubernetes. It is important to check that a configuration is valid before uploading it, because once a configuration is uploaded to Kubernetes, the Couchbase Operator is unable to handle configuration errors.

The following diagram shows the process of uploading a CouchbaseCluster configuration to Kubernetes using just `kubectl`:


![Figure 1](images/cbopctl_figure_1.jpg)


1. The `kubectl` command is run and the CouchbaseCluster configuration is sent to the Kubernetes API Server.
2. The CouchbaseCluster configuration undergoes partial validation by Kubernetes.
3. If the CouchbaseCluster configuration is found to be valid by Kubernetes, it is inserted into `etcd`. If it is not valid, an error is returned to `kubectl`.
4. `etcd` acknowledges to the API Server that the CouchbaseCluster object was persisted.
5. A success message is returned to `kubectl`.
6. The CouchbaseCluster object is sent to the Couchbase Operator.

Since Kubernetes can only do a partial validation, the only place to do full validation is in the API Server, because errors in the configuration must be caught before the CouchbaseCluster configuration makes it to `etcd`.

Kubernetes is a constantly evolving project and the ability to validate Custom Resource Definitions is still being improved. In Kubernetes 1.8, for example, there is no ability to do validation. In Kubernetes 1.9, there is some CRD validation, but it is incomplete and doesn't allow for the defaulting of configuration parameters. In Kubernetes 1.11, defaulting will be added, but there still won't be support for more complex types of validation.

Until Kubernetes provides full native validation, the Couchbase Operator needs to provide a way to validate that configurations are correct before submitting them to Kubernetes. The `cbopctl` command contains all of Couchbase's validation code in one command line tool to ensure that invalid configurations can't make it into Kubernetes.

### Downloading

OS X:

```bash
$ curl -O https://packages.couchbase.com/kubernetes/0.8.1-beta2/darwin/cbopctl
$ chmod +x cbopctl
```

Linux:

```bash
$ curl -O https://packages.couchbase.com/kubernetes/0.8.1-beta2/linux/cbopctl
$ chmod +x cbopctl
```

Windows:

```bash
$ curl -O https://packages.couchbase.com/kubernetes/0.8.1-beta2/windows/cbopctl
$ chmod +x cbopctl
```

### cbopctl commands

`cbopctl` implements the create, delete, and apply commands in a similar way to `kubectl` and `oc`.

```bash
$ cbopctl
cbopctl [<command>] [<args>]

  apply    Update a Couchbase Cluster
  create   Create a new Couchbase Cluster
  delete   Delete a new Couchbase Cluster

Optional Flags:

     --version                Prints version information
  -h,--help                   Prints the help message
```

The `create` command should be used to create new CouchbaseClusters, `delete` can be used to delete CouchbaseClusters (although no validation takes place during a delete so this command is here for completeness), and `apply` should be used when updating a cluster.

Each subcommand has three flags - one that is required and two that are optional.

```bash
Required Flags:

  -f,--filename               Filename or the resource to create

Optional Flags:

     --dry-run                If true, only print the object that would be sent,
                              without sending it
     --kubeconfig             The path to your kubernetes configuration
  -h,--help                   Prints the help message
```

Specifying the filename of a CouchbaseCluster configuration is required for all commands. The `dry-run` flag can be used to run validation on the specified configuration file without uploading it to Kubernetes. This is particularly useful if you want to use features in `kubectl` that are not in `cbopctl`, but you still want to validate the configuration before pushing it to Kubernetes. The `kubeconfig` flag is also available in case the configuration to your Kubernetes cluster is located on a non-default path.
