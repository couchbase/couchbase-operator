# Operator Configuration

The operator configuration is defined below. When loaded into Kubernetes it will download the Couchbase Operator docker image, create the CouchbaseCluster CRD, and start listening for CouchbaseCluster events. The Operator is defined as a Deployment to allow it to be restarted in the event of a pod or node failure.

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

The only fields that users may need to consider changing are below.

* spec.spec.containers[0].image - To change the operator docker image.
* metadata.namespace - To change the namespace that the operator is deployed in.
