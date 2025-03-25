package couchbaseephemeralbucketresource

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
	CompressionMode    string `json:"compressionMode"`
	ConflictResolution string `json:"conflictResolution"`
	EnableFlush        bool   `json:"enableFlush"`
	EvictionPolicy     string `json:"evictionPolicy"`
	IoPriority         string `json:"ioPriority"`
	MaxTTL             string `json:"maxTTL"`
	MemoryQuota        string `json:"memoryQuota"`
	MinimumDurability  string `json:"minimumDurability"`
	Name               string `json:"name"`
	Rank               int    `json:"rank"`
	Replicas           int    `json:"replicas"`
	Scopes             struct {
		Managed   bool `json:"managed"`
		Resources []struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"resources"`
		Selector map[string]string `json:"selector"`
	} `json:"scopes"`
}

type Status struct {
}

type CouchbaseEphemeralBucketResource struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
	Status     Status   `json:"status"`
}

type CouchbaseEphemeralBucketResourceList struct {
	APIVersion                        string                              `json:"apiVersion"`
	CouchbaseEphemeralBucketResources []*CouchbaseEphemeralBucketResource `json:"items"`
	Kind                              string                              `json:"kind"`
}
