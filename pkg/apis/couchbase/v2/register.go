/*
Copyright 2019-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package v2

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

const (
	ClusterCRDResourceKind                = "CouchbaseCluster"
	ClusterCRDResourcePlural              = "couchbaseclusters"
	BackupCRDResourceKind                 = "CouchbaseBackup"
	BackupCRDResourcePlural               = "couchbasebackups"
	BackupRestoreCRDResourceKind          = "CouchbaseBackupRestore"
	BackupRestoreCRDResourcePlural        = "couchbasebackuprestores"
	BucketCRDResourceKind                 = "CouchbaseBucket"
	BucketCRDResourcePlural               = "couchbasebuckets"
	EphemeralBucketCRDResourceKind        = "CouchbaseEphemeralBucket"
	EphemeralBucketCRDResourcePlural      = "couchbaseephemeralbuckets"
	MemcachedBucketCRDResourceKind        = "CouchbaseMemcachedBucket"
	MemcachedBucketCRDResourcePlural      = "couchbasememcachedbuckets"
	ReplicationCRDResourceKind            = "CouchbaseReplication"
	ReplicationCRDResourcePlural          = "couchbasereplications"
	UserCRDResourceKind                   = "CouchbaseUser"
	UserCRDResourcePlural                 = "couchbaseusers"
	GroupCRDResourceKind                  = "CouchbaseGroup"
	GroupCRDResourcePlural                = "couchbasegroups"
	RoleBindingCRDResourceKind            = "CouchbaseRoleBinding"
	RoleBindingCRDResourcePlural          = "couchbaserolebindings"
	AutoscalerCRDResourceKind             = "CouchbaseAutoscaler"
	AutoscalerCRDResourcePlural           = "couchbaseautoscalers"
	CollectionCRDResourceKind             = "CouchbaseCollection"
	CollectionCRDResourcePlural           = "couchbasecollections"
	CollectionGroupCRDResourceKind        = "CouchbaseCollectionGroup"
	CollectionGroupCRDResourcePlural      = "couchbasecollectiongroups"
	ScopeCRDResourceKind                  = "CouchbaseScope"
	ScopeCRDResourcePlural                = "couchbasescopes"
	ScopeGroupCRDResourceKind             = "CouchbaseScopeGroup"
	ScopeGroupCRDResourcePlural           = "couchbasescopegroups"
	MigrationReplicationCRDResourceKind   = "CouchbaseMigrationReplication"
	MigrationReplicationCRDResourcePlural = "couchbasemigrationreplications"

	GroupVersion = "v2"
	GroupName    = "couchbase.com"
	Group        = GroupName + "/" + GroupVersion

	ClusterCRDName              = ClusterCRDResourcePlural + "." + GroupName
	BackupCRDName               = BackupCRDResourcePlural + "." + GroupName
	BackupRestoreCRDName        = BackupRestoreCRDResourcePlural + "." + GroupName
	BucketCRDName               = BucketCRDResourcePlural + "." + GroupName
	EphemeralBucketCRDName      = EphemeralBucketCRDResourcePlural + "." + GroupName
	MemcachedBucketCRDName      = MemcachedBucketCRDResourcePlural + "." + GroupName
	ReplicationCRDName          = ReplicationCRDResourcePlural + "." + GroupName
	UserCRDName                 = UserCRDResourcePlural + "." + GroupName
	GroupCRDName                = GroupCRDResourcePlural + "." + GroupName
	RoleBindingCRDName          = RoleBindingCRDResourcePlural + "." + GroupName
	AutoscalerCRDName           = AutoscalerCRDResourcePlural + "." + GroupName
	CollectionCRDName           = CollectionCRDResourcePlural + "." + GroupName
	CollectionGroupCRDName      = CollectionGroupCRDResourcePlural + "." + GroupName
	ScopeCRDName                = ScopeCRDResourcePlural + "." + GroupName
	ScopeGroupCRDName           = ScopeGroupCRDResourcePlural + "." + GroupName
	MigrationReplicationCRDName = MigrationReplicationCRDResourcePlural + "." + GroupName
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
	SchemeBuilder.Register(&CouchbaseAutoscaler{}, &CouchbaseAutoscalerList{})
	SchemeBuilder.Register(&CouchbaseCollection{}, &CouchbaseCollectionList{})
	SchemeBuilder.Register(&CouchbaseCollectionGroup{}, &CouchbaseCollectionGroupList{})
	SchemeBuilder.Register(&CouchbaseScope{}, &CouchbaseScopeList{})
	SchemeBuilder.Register(&CouchbaseScopeGroup{}, &CouchbaseScopeGroupList{})
	SchemeBuilder.Register(&CouchbaseMigrationReplication{}, &CouchbaseMigrationReplicationList{})
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
	case "couchbaseautoscaler":
		return schema.GroupResource{Group: GroupName, Resource: AutoscalerCRDResourceKind}
	case "couchbasecollection":
		return schema.GroupResource{Group: GroupName, Resource: CollectionCRDResourceKind}
	case "couchbasecollectiongroup":
		return schema.GroupResource{Group: GroupName, Resource: CollectionGroupCRDResourceKind}
	case "couchbasescope":
		return schema.GroupResource{Group: GroupName, Resource: ScopeCRDResourceKind}
	case "couchbasescopegroup":
		return schema.GroupResource{Group: GroupName, Resource: ScopeGroupCRDResourceKind}
	case "couchbasemigrationreplication":
		return schema.GroupResource{Group: GroupName, Resource: MigrationReplicationCRDResourceKind}
	default:
		return schema.GroupResource{}
	}
}
