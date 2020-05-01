package v2

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

const (
	ClusterCRDResourceKind            = "CouchbaseCluster"
	ClusterCRDResourcePlural          = "couchbaseclusters"
	BackupCRDResourceKind             = "CouchbaseBackup"
	BackupCRDResourcePlural           = "couchbasebackups"
	BackupRestoreCRDResourceKind      = "CouchbaseBackupRestore"
	BackupRestoreCRDResourcePlural    = "couchbasebackuprestores"
	BackupCRDResourceShortName        = "cbbackup"
	BackupRestoreCRDResourceShortName = "cbrestore"
	BucketCRDResourceKind             = "CouchbaseBucket"
	BucketCRDResourcePlural           = "couchbasebuckets"
	EphemeralBucketCRDResourceKind    = "CouchbaseEphemeralBucket"
	EphemeralBucketCRDResourcePlural  = "couchbaseephemeralbuckets"
	MemcachedBucketCRDResourceKind    = "CouchbaseMemcachedBucket"
	MemcachedBucketCRDResourcePlural  = "couchbasememcachedbuckets"
	ReplicationCRDResourceKind        = "CouchbaseReplication"
	ReplicationCRDResourcePlural      = "couchbasereplications"
	UserCRDResourceKind               = "CouchbaseUser"
	UserCRDResourcePlural             = "couchbaseusers"
	GroupCRDResourceKind              = "CouchbaseGroup"
	GroupCRDResourcePlural            = "couchbasegroups"
	RoleBindingCRDResourceKind        = "CouchbaseRoleBinding"
	RoleBindingCRDResourcePlural      = "couchbaserolebindings"
	GroupVersion                      = "v2"
	GroupName                         = "couchbase.com"
	Group                             = GroupName + "/" + GroupVersion
	ClusterCRDName                    = ClusterCRDResourcePlural + "." + GroupName
	BackupCRDName                     = BackupCRDResourcePlural + "." + GroupName
	BackupRestoreCRDName              = BackupRestoreCRDResourcePlural + "." + GroupName
	BucketCRDName                     = BucketCRDResourcePlural + "." + GroupName
	EphemeralBucketCRDName            = EphemeralBucketCRDResourcePlural + "." + GroupName
	MemcachedBucketCRDName            = MemcachedBucketCRDResourcePlural + "." + GroupName
	ReplicationCRDName                = ReplicationCRDResourcePlural + "." + GroupName
	UserCRDName                       = UserCRDResourcePlural + "." + GroupName
	GroupCRDName                      = GroupCRDResourcePlural + "." + GroupName
	RoleBindingCRDName                = RoleBindingCRDResourcePlural + "." + GroupName
)

var (
	SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: GroupVersion}

	SchemeBuilder = &scheme.Builder{GroupVersion: SchemeGroupVersion}

	AddToScheme = SchemeBuilder.AddToScheme
)

func init() {
	SchemeBuilder.Register(&CouchbaseCluster{}, &CouchbaseClusterList{})
	SchemeBuilder.Register(&CouchbaseBucket{}, &CouchbaseBucketList{})
	SchemeBuilder.Register(&CouchbaseEphemeralBucket{}, &CouchbaseEphemeralBucketList{})
	SchemeBuilder.Register(&CouchbaseMemcachedBucket{}, &CouchbaseMemcachedBucketList{})
	SchemeBuilder.Register(&CouchbaseReplication{}, &CouchbaseReplicationList{})
	SchemeBuilder.Register(&CouchbaseUser{}, &CouchbaseUserList{})
	SchemeBuilder.Register(&CouchbaseGroup{}, &CouchbaseGroupList{})
	SchemeBuilder.Register(&CouchbaseRoleBinding{}, &CouchbaseRoleBindingList{})
	SchemeBuilder.Register(&CouchbaseBackup{}, &CouchbaseBackupList{})
	SchemeBuilder.Register(&CouchbaseBackupRestore{}, &CouchbaseBackupRestoreList{})
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
	case "couchbaseuser":
		return schema.GroupResource{Group: GroupName, Resource: UserCRDResourceKind}
	case "couchbasegroup":
		return schema.GroupResource{Group: GroupName, Resource: GroupCRDResourceKind}
	case "couchbaserolebinding":
		return schema.GroupResource{Group: GroupName, Resource: RoleBindingCRDResourceKind}
	case "couchbasebackup":
		return schema.GroupResource{Group: GroupName, Resource: BackupCRDResourceKind}
	case "couchbasebackuprestore":
		return schema.GroupResource{Group: GroupName, Resource: BackupRestoreCRDResourceKind}
	default:
		return schema.GroupResource{}
	}
}
