# Operator Configuration

The operator configuration is defined below. When loaded into Kubernetes it will download the Couchbase Operator docker image, create the CouchbaseCluster Custom Resource Definition(CRD), and start listening for CouchbaseCluster events. The Operator is defined as a Deployment to allow it to be restarted in the event of a pod or node failure.

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
        image: couchbase/couchbase-operator:v1
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

```

Most fields in the operator configuration should never be changed and we recommend using the configuration as is. Some exceptions are below.

**Changing The Namespace**

The operator will manage clusters in the namespace that it is deployed in. If you want to deploy the operator in a namespace other than the default namespace then the metadata.namespace field can be changed.

**Changing The Operation Container Image**

Most users will not need to change the operator image unless they are pulling the image from somewhere other than the official couchbase docker repository. But if you are pulling the image from somewhere else the spec.spec.containers[0].image field can be changed.

**Changing the Name**

By default the name of the deployment created to maintain the Couchbase Operator is called couchbase-operator. We recommend keeping this name since it is used in all of our examples and tutorials. If you need to change it for some reason than you must make sure to change the metadata.name, spec.template.metadata.labels.name, and spec.spec.containers[0].name fields. These fields also must all have the same value.
