package v2

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/runtime/scheme"
)

const (
	ClusterCRDResourceKind           = "CouchbaseCluster"
	ClusterCRDResourcePlural         = "couchbaseclusters"
	BucketCRDResourceKind            = "CouchbaseBucket"
	BucketCRDResourcePlural          = "couchbasebuckets"
	EphemeralBucketCRDResourceKind   = "CouchbaseEphemeralBucket"
	EphemeralBucketCRDResourcePlural = "couchbaseephemeralbuckets"
	MemcachedBucketCRDResourceKind   = "CouchbaseMemcachedBucket"
	MemcachedBucketCRDResourcePlural = "couchbasememcachedbuckets"
	ReplicationCRDResourceKind       = "CouchbaseReplication"
	ReplicationCRDResourcePlural     = "couchbasereplications"
	GroupName                        = "couchbase.com"
	ClusterCRDName                   = ClusterCRDResourcePlural + "." + GroupName
	BucketCRDName                    = BucketCRDResourcePlural + "." + GroupName
	EphemeralBucketCRDName           = EphemeralBucketCRDResourcePlural + "." + GroupName
	MemcachedBucketCRDName           = MemcachedBucketCRDResourcePlural + "." + GroupName
	ReplicationCRDName               = ReplicationCRDResourcePlural + "." + GroupName
)

var (
	SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: "v2"}

	SchemeBuilder = &scheme.Builder{GroupVersion: SchemeGroupVersion}

	AddToScheme = SchemeBuilder.AddToScheme
)

func init() {
	SchemeBuilder.Register(&CouchbaseCluster{}, &CouchbaseClusterList{})
	SchemeBuilder.Register(&CouchbaseBucket{}, &CouchbaseBucketList{})
	SchemeBuilder.Register(&CouchbaseEphemeralBucket{}, &CouchbaseEphemeralBucketList{})
	SchemeBuilder.Register(&CouchbaseMemcachedBucket{}, &CouchbaseMemcachedBucketList{})
	SchemeBuilder.Register(&CouchbaseReplication{}, &CouchbaseReplicationList{})
}

func Resource(resource string) schema.GroupResource {
	switch resource {
	case "couchbasecluster":
		return schema.GroupResource{Group: GroupName, Resource: ClusterCRDResourceKind}
	case "couchbasebucket":
		return schema.GroupResource{Group: GroupName, Resource: BucketCRDResourceKind}
	case "couchbaseephemeralbucket":
		return schema.GroupResource{Group: GroupName, Resource: EphemeralBucketCRDResourceKind}
	case "couchbasememcachedbucket":
		return schema.GroupResource{Group: GroupName, Resource: MemcachedBucketCRDResourceKind}
	case "couchbasereplication":
		return schema.GroupResource{Group: GroupName, Resource: ReplicationCRDResourceKind}
	default:
		return schema.GroupResource{}
	}
}
