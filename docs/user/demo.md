# Cluster Deployment Tutorial

This tutorial walks you through the steps to deploy a 3-nodes cluster and load data into the cluster.  

**Prerequisites**
Before proceeding to perform the steps in this tutorial, ensure that you've prepared your Kubernetes cluster to run Couchbase pods. See [Prerequisites and Setup](prerequisiteAndSetup.md) for details.
The tutorial also requires the following artifacts that are bundled with the Couchbase Operator package:
- `create-user.yaml` spec to create secrets for the Couchbase RBAC user.
- `pillowfight-data-loader.yaml` job to load data into your cluster.

1. **Create a Couchbase cluster**
```console
$ kubectl apply -f  https://packages.couchbase.com/kubernetes/beta/couchbase-cluster.yaml
couchbasecluster "cb-example" created
```

2. **Create a Couchbase RBAC user**

This tutorial will be loading data in Couchbase and hence requires a Couchbase RBAC user to be created. RBAC users can be created by directly using the CLI, or as a job within the Kubernetes cluster. See [Accessing the Couchbase CLI](couchbaseCliGuide.md) for details.

 - **Option 1: Create a default user using the CLI**

Use the `describe` command to get the Web Console port:
```console
$ kubectl describe cbc cb-example |  grep "Admin Console Port:"
   Admin Console Port:		32486
```

Create a Couchbase RBAC user by running the following command:
```console
$ ./couchbase-cli user-manage -c 192.168.99.100:32486 -u Administrator -p password --rbac-username default --rbac-password password --roles admin --auth-domain local --set
SUCCESS: RBAC user set
```
(You can skip to the next step: Loading data)

 - **Option 2: Create a default user with Kubernetes job**

Just as the admin user has a secret, the RBAC user also requires its own secret. Create a secret for the RBAC user by running the following commands:

```console
# secret for user named 'default'
$ echo -n "default" | base64
ZGVmYXVsdA==
$ echo -n "password" | base64
cGFzc3dvcmQ=

$ echo 'apiVersion: v1
kind: Secret
metadata:
  name: cb-user-auth
type: Opaque
data:
  default_user: ZGVmYXVsdA==
  default_password: cGFzc3dvcmQ=' >  cb-user-auth-secret.yaml

$ kubectl create -f  cb-user-auth-secret.yaml
secret "cb-user-auth" created
```

Now, use the RBAC user's secret to securely create a Couchbase RBAC user using a Kubernetes job:

```console
$ kubectl create -f https://packages.couchbase.com/kubernetes/beta/couchbase-cli-create-user.yaml
job "create-user" created

$ kubectl get job
NAME          DESIRED   SUCCESSFUL   AGE
create-user   1         1            4s
```

The name of the secret and its keys are very important as the sample `create-user` spec mounts the secrets into a volume.

```yaml
# https://packages.couchbase.com/kubernetes/beta/couchbase-cli-create-user.yaml
---
apiVersion: batch/v1
kind: Job
metadata:
  name: create-user
spec:
...
      volumes:
        - name: cb-admin
          secret:
            secretName: cb-example-auth
        - name: cb-users
          secret:
            secretName: cb-user-auth
```

3. **Loading data**

Use the sample `pillowfight` job to load items into your cluster. The following spec loads 10k items into the Couchbase cluster:

```yaml
---
apiVersion: batch/v1
kind: Job
metadata:
  name: pillowfight
spec:
  template:
    metadata:
      name: pillowfight
    spec:
      containers:
      - name: pillowfight
        image: sequoiatools/pillowfight
        command: ["cbc-pillowfight",
                  "-U", "couchbase://cb-example-0000.cb-example.default.svc/default?select_bucket=true",
                  "-I", "10000", "-B", "1000", "-c", "10", "-t", "1", "-P", "password"]
      restartPolicy: Never
```

To deploy the `pillowfight` data loader, run the following command:
```console
$ kubectl create -f https://packages.couchbase.com/kubernetes/beta/pillowfight-data-loader.yaml
job "pillowfight" created
```

Once it completes, you should have 10k items loaded in your cluster.

See [Accessing the Couchbase Web Console](adminConsoleAccess.md) for information about how to access the Web Console.
