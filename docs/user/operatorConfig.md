# Couchbase Operator Configuration

The Couchbase Operator configuration is defined below. When loaded into Kubernetes, it downloads the Couchbase Operator Docker image, creates the CouchbaseCluster Custom Resource Definition(CRD), and starts listening for CouchbaseCluster events. The Operator is defined as a deployment to allow it to be restarted in the event of a pod or node failure.

```yaml
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: couchbase-operator
  namespace: default
spec:
  replicas: 1
  template:
    metadata:
      labels:
        name: couchbase-operator
    spec:
      containers:
      - name: couchbase-operator
        image: couchbase/k8s-operator:0.8.0
        command:
        - couchbase-operator
        args:
        - -create-crd
        env:
        - name: MY_POD_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        - name: MY_POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        ports:
          - name: readiness-port
            containerPort: 8080
        readinessProbe:
          httpGet:
            path: /readyz
            port: readiness-port
          initialDelaySeconds: 3
          periodSeconds: 3
          failureThreshold: 19
        serviceAccountName: couchbase-operator
```

Most fields in the Operator configuration should never be changed and we recommend using the configuration as is. Some exceptions are below.

**Changing the Namespace**

The Operator manages clusters in the namespace that it is deployed in. If you want to deploy the Operator in a namespace other than the default namespace, then change the `metadata.namespace` field.

**Changing the Operator Container Image**

You should not need to change the Operator image unless you are pulling the image from somewhere other than the official Couchbase Docker repository. If you are pulling the image from somewhere else, change the `spec.spec.containers[0].image` field.

**Changing the Name**

By default the name of the deployment created to maintain the Couchbase Operator is called couchbase-operator. We recommend keeping this name since it is used in all of our examples and tutorials. If you need to change it for some reason, ensure that you change the `metadata.name`, `spec.template.metadata.labels.name`, and `spec.spec.containers[0].name` fields. These fields also must all have the same value.

**Changing the Service Account**

We recommend using a ServiceAccount named couchbase-operator, but depending on your environment you may want to use a service account with a different name. Note that this field only takes effect if your Kubernetes environment has RBAC enabled.

**Changing the Replica Count**

Normally Deployments are used to create multiple instances of a pod in order to provide redundancy. However, when deploying the Couchbase Operator, you should always set replicas to 1. This is because the Operator pod uses leader election to ensure that only one Operator is running in a specific namespace. If you start more than one Operator pod in the same namespace, only the first one will start successfully. We use Deployments so that if the Operator dies, a new operator pod gets created and picks up from where the old one left off.