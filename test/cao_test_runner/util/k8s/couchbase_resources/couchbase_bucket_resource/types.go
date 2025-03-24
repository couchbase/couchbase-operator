package couchbasebucketresource

import "time"

type Metadata struct {
	Annotations       map[string]string   `json:"annotations"`
	CreationTimestamp time.Time           `json:"creationTimestamp"`
	Labels            map[string]string   `json:"labels"`
	Name              string              `json:"name"`
	Namespace         string              `json:"namespace"`
	OwnerReferences   []MetadataOwnerRefs `json:"ownerReferences"`
	ResourceVersion   string              `json:"resourceVersion"`
	UID               string              `json:"uid"`
}

type MetadataOwnerRefs struct {
	APIVersion         string `json:"apiVersion"`
	BlockOwnerDeletion bool   `json:"blockOwnerDeletion"`
	Controller         bool   `json:"controller"`
	Kind               string `json:"kind"`
	Name               string `json:"name"`
	UID                string `json:"uid"`
}

type Spec struct {
	AutoCompaction struct {
		DatabaseFragmentationThreshold struct {
			Percent int    `json:"percent"`
			Size    string `json:"size"`
		} `json:"databaseFragmentationThreshold"`
		TimeWindow struct {
			AbortCompactionOutsideWindow bool   `json:"abortCompactionOutsideWindow"`
			End                          string `json:"end"`
			Start                        string `json:"start"`
		} `json:"timeWindow"`
		TombstonePurgeInterval     string `json:"tombstonePurgeInterval"`
		ViewFragmentationThreshold struct {
			Percent int    `json:"percent"`
			Size    string `json:"size"`
		} `json:"viewFragmentationThreshold"`
	} `json:"autoCompaction"`
	CompressionMode    string `json:"compressionMode"`
	ConflictResolution string `json:"conflictResolution"`
	EnableFlush        bool   `json:"enableFlush"`
	EnableIndexReplica bool   `json:"enableIndexReplica"`
	EvictionPolicy     string `json:"evictionPolicy"`
	HistoryRetention   struct {
		Bytes                    int  `json:"bytes"`
		CollectionHistoryDefault bool `json:"collectionHistoryDefault"`
		Seconds                  int  `json:"seconds"`
	} `json:"historyRetention"`
	IoPriority        string `json:"ioPriority"`
	MaxTTL            string `json:"maxTTL"`
	MemoryQuota       string `json:"memoryQuota"`
	MinimumDurability string `json:"minimumDurability"`
	Name              string `json:"name"`
	Rank              int    `json:"rank"`
	Replicas          int    `json:"replicas"`
	Scopes            struct {
		Managed   bool `json:"managed"`
		Resources []struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"resources"`
		Selector struct {
		} `json:"selector"`
	} `json:"scopes"`
	StorageBackend string `json:"storageBackend"`
}

type Status struct {
}

type CouchbaseBucketResource struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
	Status     Status   `json:"status"`
}

type CouchbaseBucketResourceList struct {
	APIVersion               string                     `json:"apiVersion"`
	CouchbaseBucketResources []*CouchbaseBucketResource `json:"items"`
	Kind                     string                     `json:"kind"`
}
