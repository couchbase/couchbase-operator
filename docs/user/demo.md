# Cluster Deployment Tutorial

The following steps demonstrate the deployment of a 3 node cluster with data loading.  See the [prerequisite and setup guide](prerequisiteAndSetup.md) for instructions to prepare your Kubernetes cluster to run Couchbase pods.

**Create Couchbase cluster**
```console
$ kubectl apply -f  https://packages.couchbase.com/kubernetes/beta/couchbase-cluster.yaml
couchbasecluster "cb-example" created
```

**Create a Couchbase RBAC user**

Since we're going to be loading data, a Couchbase RBAC user is required. As mentioned in the [couchbase-cli guide]( couchbaseCliGuide.md),  users can be created by directly using the cli tool or as a job within the Kubernetes cluster.

**Option 1: Create deafult user with cli tool**

Use describe command to get the administration console port:
```console
$ kubectl describe cbc cb-example |  grep "Admin Console Port:"
   Admin Console Port:		32486
```

Create a Couchbase RBAC user:
```console
$ ./couchbase-cli user-manage -c 192.168.99.100:32486 -u Administrator -p password --rbac-username default --rbac-password password --roles admin --auth-domain local --set
SUCCESS: RBAC user set
```
(skip to loading data section)

**Option 2: Create a default user with Kubernetes job**

Just as the admin user has a secret, the RBAC user will also need it's own secret, so let's create one:

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

Now we can use the user secret to securely create a Couchbase rbac user using a Kubernetes job:

```console
$ kubectl create -f https://packages.couchbase.com/kubernetes/beta/couchbase-cli-create-user.yaml
job "create-user" created

$ kubectl get job
NAME          DESIRED   SUCCESSFUL   AGE
create-user   1         1            4s
```

Note that the name of the secret and it's keys are very important here since the example create-user spec volume mounts the secrets... ie:

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

**Loading data**

Now you can use the example pillowfight job to load items into your cluster.  The following spec will load 10k items into the cluster:

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

Deploy the pillowfight data loader:
```console
$ kubectl create -f https://packages.couchbase.com/kubernetes/beta/pillowfight-data-loader.yaml
job "pillowfight" created
```

Done!  You should have 10k items loaded in your cluster.  See the [administration guide](administrationGuide.md) for information about how to access the administration console.
