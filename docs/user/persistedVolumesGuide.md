## Using Persistent Volumes

Pods can persist data to a storage system via persistent volumes.  To define a path within a Pod that should be persisted, volumeMounts need to be added to the `pod` within a group of `servers`.  These volumeMounts will only be used by the pods within its `servers` grouping.  See [CouchbaseCluster Configuration](couchbaseClusterConfig.md) Guide to understand how to define a cluster with persistent volumes.  It is also recommended to have an overall understanding of kubernetes [Persistent Volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/) prior to creating a cluster with persistent volumes.

### Benefits of using persistent volumes

* **Data Recoverability:** Persistent volumes allow the data associated within Pods to be recovered in the case that a Pod is terminated.  This helps to prevent data-loss and to avoid time-consuming index building when using the data or index services.

* **Pod Relocation:**  Kubernetes may decide to evict pods that reach resource thresholds such as Cpu and Memory Limits.  Pods that are backed with persistent volumes can be terminated and restarted on different nodes without incurring any downtime or data-loss.

### Drawbacks of using persistent volumes

* **Variable Performance:**  Persistent volumes support a wide variety of storage provisioners.  The majority of provisioners are network attached which may result in a reduction of overall i/o performance.  Refer to documentation about [Storage Provisioners] (https://kubernetes.io/docs/concepts/storage/storage-classes/#provisioner) for information about supported storage classes.

* **Additional point of failure:**  In the case that connectivity to your storage system is lost or becomes corrupt causing the persistent volumes to go offline, then the Pods will also be affected.  Data recovery will not be possible unless the persistent volumes become available again.
