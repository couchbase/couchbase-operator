// nolint:godot
package v2

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CouchbaseBucketCompressionMode defines the available compression modes for Couchbase
// and Ephemeral bucket types.
// +kubebuilder:validation:Enum=off;passive;active
type CouchbaseBucketCompressionMode string

const (
	CouchbaseBucketCompressionModeOff     CouchbaseBucketCompressionMode = "off"
	CouchbaseBucketCompressionModePassive CouchbaseBucketCompressionMode = "passive"
	CouchbaseBucketCompressionModeActive  CouchbaseBucketCompressionMode = "active"
)

// CouchbaseBucketEvictionPolicy defines the available eviction policies for
// Couchbase bucket types.
// +kubebuilder:validation:Enum=valueOnly;fullEviction
type CouchbaseBucketEvictionPolicy string

const (
	// CouchbaseBucketEvictionPolicyValueOnly evicts the document body only from
	// memory and retains the metadata.
	CouchbaseBucketEvictionPolicyValueOnly CouchbaseBucketEvictionPolicy = "valueOnly"

	// CouchbaseBucketEvictionPolicyFullEviction evicts both the document and
	// metadata from memory.
	CouchbaseBucketEvictionPolicyFullEviction CouchbaseBucketEvictionPolicy = "fullEviction"
)

// CouchbaseEphemeralBucketEvictionPolicy defines the available eviction
// policies for ephemeral bucket types.
// +kubebuilder:validation:Enum=noEviction;nruEviction
type CouchbaseEphemeralBucketEvictionPolicy string

const (
	// CouchbaseEphemeralBucketEvictionPolicyNoEviction never evicts a docuement from
	// memory.
	CouchbaseEphemeralBucketEvictionPolicyNoEviction CouchbaseEphemeralBucketEvictionPolicy = "noEviction"

	// CouchbaseEphemeralBucketEvictionPolicyNRUEviction evict not recently used
	// documents from memory.
	CouchbaseEphemeralBucketEvictionPolicyNRUEviction CouchbaseEphemeralBucketEvictionPolicy = "nruEviction"
)

// CouchbaseBucketIOPriority defines the priority of a bucket.
// +kubebuilder:validation:Enum=low;high
type CouchbaseBucketIOPriority string

const (
	// CouchbaseBucketIOPriorityHigh runs the bucket with a high number of threads.
	CouchbaseBucketIOPriorityHigh CouchbaseBucketIOPriority = "high"

	// CouchbaseBucketIOPriorityLow runs the bucket with a low number of threads.
	CouchbaseBucketIOPriorityLow CouchbaseBucketIOPriority = "low"
)

// CouchbsaeBucketConflictResolution defines the XDCR conflict resolution for a bucket.
// +kubebuilder:validation:Enum=seqno;lww
type CouchbaseBucketConflictResolution string

const (
	// CouchbaseBucketConflictResolutionSequenceNumber chooses the most recent document based
	// on sequence number.
	CouchbaseBucketConflictResolutionSequenceNumber CouchbaseBucketConflictResolution = "seqno"

	// CouchbaseBucketConflictResolutionTimestamp chooses the most recent document based
	// on timestamps.
	CouchbaseBucketConflictResolutionTimestamp CouchbaseBucketConflictResolution = "lww"
)

// CouchbaseBucketMinimumDurability is the miniumum durability that writes to a bucket
// should conform to.  Clients can explicitly write with a stricter policy.
// +kubebuilder:validation:Enum=none;majority;majorityAndPersistActive;persistToMajority
type CouchbaseBucketMinimumDurability string

const (
	// CouchbaseBucketMinimumDurabilityNone applies no durability constraints.
	CouchbaseBucketMinimumDurabilityNone CouchbaseBucketMinimumDurability = "none"

	// CouchbaseBucketMinimumDurabilityMajority ensures all writes are duplicated to
	// the majority of nodes, in-memory.
	CouchbaseBucketMinimumDurabilityMajority CouchbaseBucketMinimumDurability = "majority"

	// CouchbaseBucketMinimumDurabilityMajorityAndPersistActive ensures all writes are
	// duplicated to the majority of nodes, in-memory, and persisted to disk on the
	// vbucket master copy.
	CouchbaseBucketMinimumDurabilityMajorityAndPersistActive CouchbaseBucketMinimumDurability = "majorityAndPersistActive"

	// CouchbaseBucketMinimumDurabilityPersistToMajority ensures all writes are persisted
	// to the majority of disks on all nodes.
	CouchbaseBucketMinimumDurabilityPersistToMajority CouchbaseBucketMinimumDurability = "persistToMajority"
)

// CouchbaseEphemeralBucketMinimumDurability is the miniumum durability that writes to a bucket
// should conform to.  Clients can explicitly write with a stricter policy.
// +kubebuilder:validation:Enum=none;majority
type CouchbaseEphemeralBucketMinimumDurability string

const (
	// CouchbaseEphemeralBucketMinimumDurabilityNone applies no durability constraints.
	CouchbaseEphemeralBucketMinimumDurabilityNone CouchbaseEphemeralBucketMinimumDurability = "none"

	// CouchbaseEphemeralBucketMinimumDurabilityMajority ensures all writes are duplicated to
	// the majority of nodes, in-memory.
	CouchbaseEphemeralBucketMinimumDurabilityMajority CouchbaseEphemeralBucketMinimumDurability = "majority"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:categories=all;couchbase
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:resource:shortName=cbbackup
// +kubebuilder:printcolumn:name="strategy",type="string",JSONPath=".spec.strategy"
// +kubebuilder:printcolumn:name="volume size",type="string",JSONPath=".spec.size"
// +kubebuilder:printcolumn:name="capacity used",type="string",JSONPath=".status.capacityUsed"
// +kubebuilder:printcolumn:name="last run",type="string",JSONPath=".status.lastRun"
// +kubebuilder:printcolumn:name="last success",type="string",JSONPath=".status.lastSuccess"
// +kubebuilder:printcolumn:name="running",type="boolean",JSONPath=".status.running"
// +kubebuilder:printcolumn:name="age",type="date",JSONPath=".metadata.creationTimestamp"
type CouchbaseBackup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CouchbaseBackupSpec   `json:"spec"`
	Status            CouchbaseBackupStatus `json:"status,omitempty"`
}

type CouchbaseBackupSpec struct {
	// CB backup strategy - Full/Incremental, Full only. Default: Full/Incremental
	Strategy Strategy `json:"strategy"`

	// Incremental is the schedule on when to take incremental backups.
	// Used in Full/Incremental backup strategies
	Incremental *CouchbaseBackupSchedule `json:"incremental,omitempty"`

	// Full is the schedule on when to take full backups.
	// Used in Full/Incremental and FullOnly backup strategies
	Full *CouchbaseBackupSchedule `json:"full,omitempty"`

	// Amount of successful jobs to keep
	// +kubebuilder:validation:Minimum=0
	SuccessfulJobsHistoryLimit int32 `json:"successfulJobsHistoryLimit,omitempty"`

	// Amount of failed jobs to keep
	// +kubebuilder:validation:Minimum=0
	FailedJobsHistoryLimit int32 `json:"failedJobsHistoryLimit,omitempty"`

	// Number of times a backup job should try to execute.
	// Once it hits the BackoffLimit it will not run until the next scheduled job
	BackoffLimit int32 `json:"backoffLimit,omitempty"`

	// Number of hours to hold backups for, everything older will be deleted
	BackupRetention *metav1.Duration `json:"backupRetention,omitempty"`

	// Number of hours to hold script logs for, everything older will be deleted
	LogRetention *metav1.Duration `json:"logRetention,omitempty"`

	// Size in GB of the associated PVC
	Size *resource.Quantity `json:"size,omitempty"`

	// Name of StorageClass to use
	StorageClassName *string `json:"storageClassName,omitempty"`
}

type CouchbaseBackupStatus struct {
	// CapacityUsed tells us how much of the PVC we are using
	CapacityUsed *resource.Quantity `json:"capacityUsed,omitempty"`
	// Location of Backup Archive
	Archive string `json:"archive,omitempty"`
	// Repo is where we are currently performing operations
	Repo string `json:"repo,omitempty"`
	// Backups gives us a full list of all backups
	// and their respective repo locations
	Backups []BackupStatus `json:"backups,omitempty"`
	// Running indicates whether a backup is currently being performed
	Running bool `json:"running"`
	// Failed indicates whether the most recent backup has failed
	Failed bool `json:"failed"`
	// Output reports useful information from the backup_script
	Output string `json:"output,omitempty"`
	// Pod tells us which pod is running/ran last
	Pod string `json:"pod"`
	// Job tells us which job is running/ran last
	Job string `json:"job"`
	// Cronjob tells us which Cronjob the job belongs to
	CronJob string `json:"cronjob"`
	// Duration tells us how long the last backup took
	Duration *metav1.Duration `json:"duration,omitempty"`
	// LastFailure tells us the time the last failed backup failed
	LastFailure *metav1.Time `json:"lastFailure,omitempty"`
	// LastSuccess gives us the time the last successful backup finished
	LastSuccess *metav1.Time `json:"lastSuccess,omitempty"`
	// LastRun tells us the time the last backup job started
	LastRun *metav1.Time `json:"lastRun,omitempty"`
}

// +kubebuilder:validation:Enum=full_incremental;full_only
type Strategy string

const (
	// Similar to Periodic but we create a new backup repo and take a full backup instead of merging.
	FullIncremental Strategy = "full_incremental"

	// Expensive Full Backup only recommended for small clusters.
	FullOnly Strategy = "full_only"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseBackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseBackup `json:"items"`
}

type CouchbaseBackupSchedule struct {
	// Schedule takes a cron schedule in string format
	Schedule string `json:"schedule"`
}

type BackupStatus struct {
	// name of the repo
	Name string `json:"name"`
	// the full backup inside the repo
	Full string `json:"full,omitempty"`
	// incremental backups inside the repo
	Incrementals []string `json:"incrementals,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:categories=all;couchbase
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:resource:shortName=cbrestore
// +kubebuilder:printcolumn:name="capacity used",type="string",JSONPath=".status.capacityUsed"
// +kubebuilder:printcolumn:name="last run",type="string",JSONPath=".status.lastRun"
// +kubebuilder:printcolumn:name="last success",type="string",JSONPath=".status.lastSuccess"
// +kubebuilder:printcolumn:name="duration",type="string",JSONPath=".status.duration"
// +kubebuilder:printcolumn:name="running",type="boolean",JSONPath=".status.running"
// +kubebuilder:printcolumn:name="age",type="date",JSONPath=".metadata.creationTimestamp"
type CouchbaseBackupRestore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CouchbaseBackupRestoreSpec   `json:"spec"`
	Status            CouchbaseBackupRestoreStatus `json:"status,omitempty"`
}

type CouchbaseBackupRestoreSpec struct {
	// the cbbackup we associate this restore with or the cbbackup pvc we want to restore from
	Backup string `json:"backup"`
	// Repo is the backup folder to restore from
	Repo string `json:"repo,omitempty"`
	// Start and End denote the names of the start and end backups of a time range to restore backups from
	// this will restore these two backups and any backups in between.
	// If we only wish to restore one backup leave end blank or use the same backup for End
	Start *StrOrInt `json:"start"`
	End   *StrOrInt `json:"end,omitempty"`
	// Number of hours to hold restore script logs for, everything older will be deleted
	LogRetention *metav1.Duration `json:"logRetention,omitempty"`
	// Number of times the restore job should try to execute.
	BackoffLimit int32 `json:"backoffLimit,omitempty"`
}

// struct we use in CouchbaseBackupRestoreSpec to enforce type-safeness
type StrOrInt struct {
	Str *string `json:"str,omitempty"`
	// +kubebuilder:validation:Minimum=1
	Int *int `json:"int,omitempty"`
}

type CouchbaseBackupRestoreStatus struct {
	// Location of Backup Archive
	Archive string `json:"archive,omitempty"`
	// Repo is where we are currently performing operations
	Repo string `json:"repo,omitempty"`
	// Backups gives us a full list of all backups
	// and their respective repo locations
	Backups []BackupStatus `json:"backups,omitempty"`
	// Running indicates whether a restore is currently being performed
	Running bool `json:"running"`
	// Failed indicates whether the most recent restore has failed
	Failed bool `json:"failed"`
	// Output reports useful information from the backup_script
	Output string `json:"output,omitempty"`
	// Pod tells us which pod is running/ran last
	Pod string `json:"pod"`
	// Job tells us which job is running/ran last
	Job string `json:"job"`
	// Duration tells us how long the last restore took
	Duration *metav1.Duration `json:"duration,omitempty"`
	// LastFailure tells us the time the last failed restore failed
	LastFailure *metav1.Time `json:"lastFailure,omitempty"`
	// LastSuccess gives us the time the last successful restore finished
	LastSuccess *metav1.Time `json:"lastSuccess,omitempty"`
	// LastRun tells us the time the last restore job started
	LastRun *metav1.Time `json:"lastRun,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseBackupRestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseBackupRestore `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:categories=all;couchbase
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="memory quota",type="string",JSONPath=".spec.memoryQuota"
// +kubebuilder:printcolumn:name="replicas",type="integer",JSONPath=".spec.replicas"
// +kubebuilder:printcolumn:name="io priority",type="string",JSONPath=".spec.ioPriority"
// +kubebuilder:printcolumn:name="eviction policy",type="string",JSONPath=".spec.evictionPolicy"
// +kubebuilder:printcolumn:name="conflict resolution",type="string",JSONPath=".spec.conflictResolution"
// +kubebuilder:printcolumn:name="age",type="date",JSONPath=".metadata.creationTimestamp"
type CouchbaseBucket struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CouchbaseBucketSpec `json:"spec"`
}

type CouchbaseBucketSpec struct {
	// +kubebuilder:validation:Pattern="^[a-zA-Z0-9-_%\\.]+$"
	// +kubebuilder:validation:MaxLength=100
	Name        string             `json:"name,omitempty"`
	MemoryQuota *resource.Quantity `json:"memoryQuota,omitempty"`
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=3
	Replicas           int                               `json:"replicas,omitempty"`
	IoPriority         CouchbaseBucketIOPriority         `json:"ioPriority,omitempty"`
	EvictionPolicy     CouchbaseBucketEvictionPolicy     `json:"evictionPolicy,omitempty"`
	ConflictResolution CouchbaseBucketConflictResolution `json:"conflictResolution,omitempty"`
	EnableFlush        bool                              `json:"enableFlush,omitempty"`
	EnableIndexReplica bool                              `json:"enableIndexReplica,omitempty"`
	CompressionMode    CouchbaseBucketCompressionMode    `json:"compressionMode,omitempty"`
	MinimumDurability  CouchbaseBucketMinimumDurability  `json:"minimumDurability,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseBucketList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseBucket `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:categories=all;couchbase
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="memory quota",type="string",JSONPath=".spec.memoryQuota"
// +kubebuilder:printcolumn:name="replicas",type="integer",JSONPath=".spec.replicas"
// +kubebuilder:printcolumn:name="io priority",type="string",JSONPath=".spec.ioPriority"
// +kubebuilder:printcolumn:name="eviction policy",type="string",JSONPath=".spec.evictionPolicy"
// +kubebuilder:printcolumn:name="conflict resolution",type="string",JSONPath=".spec.conflictResolution"
// +kubebuilder:printcolumn:name="age",type="date",JSONPath=".metadata.creationTimestamp"
type CouchbaseEphemeralBucket struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CouchbaseEphemeralBucketSpec `json:"spec"`
}

type CouchbaseEphemeralBucketSpec struct {
	// +kubebuilder:validation:Pattern="^[a-zA-Z0-9-_%\\.]+$"
	// +kubebuilder:validation:MaxLength=100
	Name        string             `json:"name,omitempty"`
	MemoryQuota *resource.Quantity `json:"memoryQuota,omitempty"`
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=3
	Replicas           int                                       `json:"replicas,omitempty"`
	IoPriority         CouchbaseBucketIOPriority                 `json:"ioPriority,omitempty"`
	EvictionPolicy     CouchbaseEphemeralBucketEvictionPolicy    `json:"evictionPolicy,omitempty"`
	ConflictResolution CouchbaseBucketConflictResolution         `json:"conflictResolution,omitempty"`
	EnableFlush        bool                                      `json:"enableFlush,omitempty"`
	CompressionMode    CouchbaseBucketCompressionMode            `json:"compressionMode,omitempty"`
	MinimumDurability  CouchbaseEphemeralBucketMinimumDurability `json:"minimumDurability,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseEphemeralBucketList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseEphemeralBucket `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:categories=all;couchbase
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="memory quota",type="string",JSONPath=".spec.memoryQuota"
// +kubebuilder:printcolumn:name="age",type="date",JSONPath=".metadata.creationTimestamp"
type CouchbaseMemcachedBucket struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CouchbaseMemcachedBucketSpec `json:"spec"`
}

type CouchbaseMemcachedBucketSpec struct {
	// +kubebuilder:validation:Pattern="^[a-zA-Z0-9-_%\\.]+$"
	// +kubebuilder:validation:MaxLength=100
	Name        string             `json:"name,omitempty"`
	MemoryQuota *resource.Quantity `json:"memoryQuota,omitempty"`
	EnableFlush bool               `json:"enableFlush,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseMemcachedBucketList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseMemcachedBucket `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:categories=all;couchbase
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="bucket",type="string",JSONPath=".spec.bucket"
// +kubebuilder:printcolumn:name="remote bucket",type="string",JSONPath=".spec.remoteBucket"
// +kubebuilder:printcolumn:name="paused",type="boolean",JSONPath=".spec.paused"
// +kubebuilder:printcolumn:name="age",type="date",JSONPath=".metadata.creationTimestamp"
type CouchbaseReplication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CouchbaseReplicationSpec `json:"spec"`
}

// CompressionType represents all allowable XDCR compression modes.
type CompressionType string

const (
	// CompressionTypeNone applies no compression
	CompressionTypeNone CompressionType = "None"

	// CompressionTypeAuto automatically applies compression based on internal heuristics.
	CompressionTypeAuto CompressionType = "Auto"

	// CompressionTypeSnappy applies snappy compression to all documents.
	CompressionTypeSnappy CompressionType = "Snappy"
)

type CouchbaseReplicationSpec struct {
	// Bucket is the source bucket to replicate from.  This must be defined and
	// selected by the cluster.
	Bucket string `json:"bucket,omitempty"`

	// RemoteBucket is the remote bucket name to synchronize to.
	RemoteBucket string `json:"remoteBucket,omitempty"`

	// CompressionType is the type of compression to apply to the replication.
	// +kubebuilder:validation:Enum=None;Auto;Snappy
	CompressionType CompressionType `json:"compressionType,omitempty"`

	// FilterExpression allows certain documents to be filtered out of the replication.
	FilterExpression string `json:"filterExpression,omitempty"`

	// Paused allows a replication to be stopped and restarted.
	Paused bool `json:"paused,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseReplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseReplication `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:categories=all;couchbase
// +kubebuilder:resource:scope=Namespaced
type CouchbaseUser struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CouchbaseUserSpec `json:"spec"`
}

type AuthDomain string

const (
	InternalAuthDomain AuthDomain = "local"
	LDAPAuthDomain     AuthDomain = "external"
)

type CouchbaseUserSpec struct {
	// (Optional) Full Name of Couchbase user
	FullName string `json:"fullName,omitempty"`
	// The domain which provides user auth
	// +kubebuilder:validation:Enum=local;external
	AuthDomain AuthDomain `json:"authDomain"`
	// Name of kubernetes secret with password for couchbase domain
	AuthSecret string `json:"authSecret,omitempty"`
}

// CouchbaseUserList is a list of Couchbase users.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseUserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseUser `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:categories=all;couchbase
// +kubebuilder:resource:scope=Namespaced
type CouchbaseGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CouchbaseGroupSpec `json:"spec"`
}

type CouchbaseGroupSpec struct {
	// role identifier
	Roles []Role `json:"roles"`
	// optional reference to LDAP group
	LDAPGroupRef string `json:"ldapGroupRef,omitempty"`
}

// RoleName is a type-safe enumeration of all supported role names.
type RoleName string

const (
	RoleFullAdmin          RoleName = "admin"
	RoleClusterAdmin       RoleName = "cluster_admin"
	RoleSecurityAdmin      RoleName = "security_admin"
	RoleReadOnlyAdmin      RoleName = "ro_admin"
	RoleXDCRAdmin          RoleName = "replication_admin"
	RoleQueryCurlAccess    RoleName = "query_external_access"
	RoleQuestySystemAccess RoleName = "query_system_catalog"
	RoleAnalyticsReader    RoleName = "analytics_reader"
	RoleBucketAdmin        RoleName = "bucket_admin"
	RoleViewsAdmin         RoleName = "views_admin"
	RoleSearchAdmin        RoleName = "fts_admin"
	RoleApplicationAccess  RoleName = "bucket_full_access"
	RoleDataReader         RoleName = "data_reader"
	RoleDataWriter         RoleName = "data_writer"
	RoleDCPReader          RoleName = "data_dcp_reader"
	RoleBackup             RoleName = "data_backup"
	RoleMonitor            RoleName = "data_monitoring"
	RoleXDCRInbound        RoleName = "replication_target"
	RoleAnalyticsManager   RoleName = "analytics_manager"
	RoleViewsReader        RoleName = "views_reader"
	RoleSearchReader       RoleName = "fts_searcher"
	RoleQuerySelect        RoleName = "query_select"
	RoleQueryUpdate        RoleName = "query_update"
	RoleQueryInsert        RoleName = "query_insert"
	RoleQueryDelete        RoleName = "query_delete"
	RoleQueryManageIndex   RoleName = "query_manage_index"
)

type Role struct {
	// name of role
	// +kubebuilder:validation:Enum=admin;cluster_admin;security_admin;ro_admin;replication_admin;query_external_access;query_system_catalog;analytics_reader;bucket_admin;views_admin;fts_admin;bucket_full_access;data_reader;data_writer;data_dcp_reader;data_backup;data_monitoring;replication_target;analytics_manager;views_reader;fts_searcher;query_select;query_update;query_insert;query_delete;query_manage_index
	Name RoleName `json:"name"`
	// optional bucket name for bucket admin roles
	// +kubebuilder:validation:Pattern="^\\*$|^[a-zA-Z0-9-_%\\.]+$"
	Bucket string `json:"bucket,omitempty"`
}

// CouchbaseGroupList is a list of Couchbase users.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseGroup `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:categories=all;couchbase
// +kubebuilder:resource:scope=Namespaced
type CouchbaseRoleBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CouchbaseRoleBindingSpec `json:"spec"`
}

type CouchbaseRoleBindingSpec struct {
	//  List of users to bind a role to
	Subjects []CouchbaseRoleBindingSubject `json:"subjects"`
	// CouchbaseRole being bound to subjects
	RoleRef CouchbaseRoleBindingRef `json:"roleRef"`
}

type RoleBindingSubjectType string

const (
	// RoleBindingSubjectTypeUser applies role to Couchbase User
	RoleBindingSubjectTypeUser RoleBindingSubjectType = "CouchbaseUser"
)

type RoleBindingReferenceType string

const (
	// RoleBindingReferenceTypeGroup applies role to Couchbase Group
	RoleBindingReferenceTypeGroup RoleBindingReferenceType = "CouchbaseGroup"
)

type CouchbaseRoleBindingSubject struct {
	// Couchbase user/group kind
	Kind RoleBindingSubjectType `json:"kind"`
	// Name of Couchbase user resource
	Name string `json:"name"`
}

type CouchbaseRoleBindingRef struct {
	// Kind of role to use for binding
	Kind RoleBindingReferenceType `json:"kind"`
	// Name of role resource to use for binding
	Name string `json:"name"`
}

// CouchbaseRoleBindingList is a list of Couchbase users.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseRoleBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseRoleBinding `json:"items"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// CouchbaseClusterList is a list of Couchbase clusters.
type CouchbaseClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseCluster `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:categories=all;couchbase
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:resource:shortName=cbc
// +kubebuilder:printcolumn:name="version",type="string",JSONPath=".status.currentVersion"
// +kubebuilder:printcolumn:name="size",type="string",JSONPath=".status.size"
// +kubebuilder:printcolumn:name="status",type="string",JSONPath=".status.conditions[?(@.type==\"Available\")].reason"
// +kubebuilder:printcolumn:name="uuid",type="string",JSONPath=".status.clusterId"
// +kubebuilder:printcolumn:name="age",type="date",JSONPath=".metadata.creationTimestamp"
type CouchbaseCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ClusterSpec   `json:"spec"`
	Status            ClusterStatus `json:"status,omitempty"`
}

// Supported services
// +kubebuilder:validation:Enum=admin;data;index;query;search;eventing;analytics
type Service string

const (
	AdminService     Service = "admin"
	DataService      Service = "data"
	IndexService     Service = "index"
	QueryService     Service = "query"
	SearchService    Service = "search"
	EventingService  Service = "eventing"
	AnalyticsService Service = "analytics"
)

type ServiceList []Service

// +kubebuilder:validation:Enum=admin;xdcr;client
type ExposedFeature string

// Supported features
const (
	// Exposes the admin port/UI
	FeatureAdmin ExposedFeature = "admin"
	// Exposes ports necessary for XDCR
	FeatureXDCR ExposedFeature = "xdcr"
	// Exposes all client ports for services
	FeatureClient ExposedFeature = "client"
)

// A list of exposed features e.g. admin,xdcr
type ExposedFeatureList []ExposedFeature

// PlatformType defines the platform you are running on which provides explicit
// control over how resources are configured.
// +kubebuilder:validation:Enum=aws;gce;azure
type PlatformType string

const (
	PlatformTypeAWS   PlatformType = "aws"
	PlatformTypeGCE   PlatformType = "gce"
	PlatformTypeAzure PlatformType = "azure"
)

// This controls how aggressive we are when recovering cluster topology.
// +kubebuilder:valudation:Enum=PrioritzeDataIntegrity;PrioritzeUptime
type RecoveryPolicy string

const (
	// PrioritzeDataIntegrity listens to Couchbase Server, assuming it knows
	// best what it is safe to do.  This is the default value.
	PrioritzeDataIntegrity RecoveryPolicy = "PrioritzeDataIntegrity"

	// PrioritzeUptime ignores Couchbase server after the auto-failover time
	// period and may result in data loss.  This is perfect for in-memory caches
	// as they can tolerate such behaviour.  It also handles things like ephemeral
	// pods in a supported cluster e.g. losing all of your query nodes it not a
	// big problem.
	PrioritzeUptime RecoveryPolicy = "PrioritzeUptime"
)

// This controls how aggressive to be with upgrades.
// +kubebuilder:validation:Enum=RollingUpgrade;ImmediateUpgrade
type UpgradeStrategy string

const (
	// RollingUpgrade is a one-at-a-time upgrade strategy.
	// It's safer in that it can be rolled-back and only affects a single node
	// at a time, therefore giving potentially higher front-end performance.
	// Upgrades will take longer.
	RollingUpgrade UpgradeStrategy = "RollingUpgrade"

	// ImmediateUpgrade is an all-at-once upgrade strategy.
	// It's less safe, cannot be reverted halfway through, and affects all nodes
	// at once so performance may suffer.  Upgrades however will take a fraction
	// of the time.
	ImmediateUpgrade UpgradeStrategy = "ImmediateUpgrade"
)

// HibernationStrategy defines how aggressive to be when putting a cluster to sleep.
// +kubebuilder:validation:Enum=Immediate;DrainWriteQueues
type HibernationStrategy string

const (
	// ImmediateHibernation is a forced hibernation and immediate terminates all
	// pods.  This can be used in cases where clients are keeping the cluster
	// awake by committing write traffic.
	ImmediateHibernation HibernationStrategy = "Immediate"

	// DrainWriteQueueHibernation is the safe method of hibernating as cluster.
	// This assumes you have full control of clients and can stop them.  The
	// cluster will eventually persist writes to disk and all transactions can
	// be persisted across the hibernation.
	DrainWriteQueueHibernation HibernationStrategy = "DrainWriteQueues"
)

type ClusterSpec struct {
	// BaseImage is the base couchbase image name that will be used to launch
	// couchbase clusters. This is useful for private registries, etc.
	// +kubebuilder:validation:Pattern="^(.*?(:\\d+)?/)?.*?/.*?(:.*?\\d+\\.\\d+\\.\\d+.*|@sha256:[0-9a-f]{64})$"
	Image string `json:"image"`

	// Paused is to pause the control of the operator for the couchbase cluster.
	Paused bool `json:"paused,omitempty"`

	// Hibernate is whether to hibernate the cluster.
	Hibernate bool `json:"hibernate,omitempty"`

	// HibernationStrategy is how aggressive the are when hibernating.
	HibernationStrategy *HibernationStrategy `json:"hibernationStrategy,omitempty"`

	// This controls how aggressive we are when recovering cluster topology.
	RecoveryPolicy *RecoveryPolicy `json:"recoveryPolicy,omitempty"`

	// This controls how aggressive we are when performing a cluster upgrade.
	UpgradeStrategy *UpgradeStrategy `json:"upgradeStrategy,omitempty"`

	// AntiAffinity determines if the couchbase-operator tries to avoid putting
	// the couchbase members in the same cluster onto the same node.
	AntiAffinity bool `json:"antiAffinity,omitempty"`

	// Cluster specific settings
	ClusterSettings ClusterConfig `json:"cluster,omitempty"`

	// Enables software update notifications in the UI
	SoftwareUpdateNotifications bool `json:"softwareUpdateNotifications,omitempty"`

	// VolumeClaimTemplates define the desired characteristics of a volume
	// that can be requested/claimed by a pod.
	// When specified, each claim should map to the name of a volumeMount
	// defined in a PodPolicy
	VolumeClaimTemplates []v1.PersistentVolumeClaim `json:"volumeClaimTemplates,omitempty"`

	// ServerGroups define the set of availability zones we want to distribute
	// pods over.  This allows the Kubernetes cluster administrator to label all
	// nodes, but use a specific subset for a particular Couchbase cluster.
	ServerGroups []string `json:"serverGroups,omitempty"`

	// Security Context for all pods
	SecurityContext *v1.PodSecurityContext `json:"securityContext,omitempty"`

	// Platform gives a hint as to what platform we are running on and how
	// to configure services etc.
	Platform PlatformType `json:"platform,omitempty"`

	// Security groups together related security options.
	Security CouchbaseClusterSecuritySpec `json:"security"`

	// Networking groups together related networking options.
	Networking CouchbaseClusterNetworkingSpec `json:"networking,omitempty"`

	// Logging groups together logging related options.
	Logging CouchbaseClusterLoggingSpec `json:"logging,omitempty"`

	// Servers specifies
	// +kubebuilder:validation:MinItems=1
	Servers []ServerConfig `json:"servers"`

	// Bucket specific settings
	Buckets Buckets `json:"buckets,omitempty"`

	// XDCR specific settings.
	XDCR XDCR `json:"xdcr,omitempty"`

	// Prometheus Monitoring settings.
	Monitoring *CouchbaseClusterMonitoringSpec `json:"monitoring,omitempty"`

	// Backup specific settings
	Backup Backup `json:"backup,omitempty"`
}

type Backup struct {
	// Managed defines whether backups are managed by us or the clients.
	Managed bool `json:"managed,omitempty"`
	// The Backup Image to run on backup pods
	Image string `json:"image,omitempty"`
	// The Service Account to run backup (and restore) pods under.
	// Without this backup pods will not be able to update status
	ServiceAccount string `json:"serviceAccountName,omitempty"`
	// NodeSelector defines which nodes to constrain the pods that
	// run any backup operations to
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// Resources is the resource requirements for the backup container.
	// This field cannot be updated once the cluster is created.
	// Will be populated by defaults if not specified.
	Resources *v1.ResourceRequirements `json:"resources,omitempty"`
	// Selector allows CouchbaseBackup and CouchbaseBackupRestore
	// resources to be filtered based on labels.
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
	// Tolerations specifies all backup pod tolerations.
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`
	// ImagePullSecrets allow you to use an image from private
	// repos and non-dockerhub ones.
	ImagePullSecrets []v1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
}

// +kubebuilder:validation:Enum=None;StartTLSExtension;TLS
type LDAPEncryption string

// LDAP Encryption types
const (
	LDAPEncryptionNone     LDAPEncryption = "None"
	LDAPEncryptionStartTLS LDAPEncryption = "StartTLSExtension"
	LDAPEncryptionTLS      LDAPEncryption = "TLS"
)

// LDAP Spec
type CouchbaseClusterLDAPSpec struct {
	// BindSecret is the name of a kubernetes secret to use containing password for LDAP user binding
	BindSecret string `json:"bindSecret"`

	// TLSSecret is the name of a kubernetes secret to use for LDAP ca cert.
	TLSSecret string `json:"tlsSecret,omitempty"`

	// Enables using LDAP to authenticate users.
	AuthenticationEnabled bool `json:"authenticationEnabled,omitempty"`

	// Enables use of LDAP groups for authorization.
	AuthorizationEnabled bool `json:"authorizationEnabled,omitempty"`

	// List of LDAP hosts.
	Hosts []string `json:"hosts"`

	// LDAP port
	Port int `json:"port"`

	// Encryption method to communicate with LDAP servers.
	// Can be StartTLSExtension, TLS, or false.
	Encryption LDAPEncryption `json:"encryption,omitempty"`

	// Whether server certificate validation be enabled
	EnableCertValidation bool `json:"serverCertValidation,omitempty"`

	// Certificate in PEM format to be used in LDAP server certificate validation
	CACert string `json:"cacert,omitempty"`

	// LDAP query, to get the users' groups by username in RFC4516 format.
	GroupsQuery string `json:"groupsQuery,omitempty"`

	// DN to use for searching users and groups synchronization.
	BindDN string `json:"bindDN,omitempty"`

	// User to distinguished name (DN) mapping. If none is specified,
	// the username is used as the user’s distinguished name.
	UserDNMapping LDAPUserDNMapping `json:"userDNMapping,omitempty"`

	// If enabled Couchbase server will try to recursively search for groups
	// for every discovered ldap group. groups_query will be user for the search.
	NestedGroupsEnabled bool `json:"nestedGroupsEnabled,omitempty"`

	// Maximum number of recursive groups requests the server is allowed to perform.
	// Requires NestedGroupsEnabled.  Values between 1 and 100: the default is 10.
	NestedGroupsMaxDepth uint64 `json:"nestedGroupsMaxDepth,omitempty"`

	// Lifetime of values in cache in milliseconds. Default 300000 ms.
	CacheValueLifetime uint64 `json:"cacheValueLifetime,omitempty"`
}

type LDAPUserDNMapping struct {
	Template string `json:"template"`
}

type CouchbaseClusterSecuritySpec struct {
	// AdminSecret is the name of a kubernetes secret to use for administrator authentication.
	AdminSecret string `json:"adminSecret"`
	// Couchbase RBAC Users
	RBAC RBAC `json:"rbac,omitempty"`
	// LDAP Settings
	LDAP *CouchbaseClusterLDAPSpec `json:"ldap,omitempty"`
}

// +kubebuilder:validation:Pattern="^\\d{1,3}\\.\\d{1,3}\\.\\d{1,3}\\.\\d{1,3}/\\d{1,2}$"
type IPv4Prefix string

type IPV4PrefixList []IPv4Prefix

// ObjectMeta is a sanitized object metadata object that exposes
// what we allow a user to modify.
type ObjectMeta struct {
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ServiceTemplateSpec is a sanitized version of a service that exposes
// what we allow a user to modify.
type ServiceTemplateSpec struct {
	ObjectMeta `json:"metadata,inline,omitempty"`
	Spec       *v1.ServiceSpec `json:"spec,omitempty"`
}

// NetworkPlatform defines any hacks we have to do to work on specific
// netwoork meshes.
// +kubebuilder:validation:Enum=Istio
type NetworkPlatform string

const (
	// NetworkPlatformIstio is for Istio service mesh.
	NetworkPlatformIstio NetworkPlatform = "Istio"
)

type CouchbaseClusterNetworkingSpec struct {
	// ExposeAdminConsole creates a service referencing the admin console.
	ExposeAdminConsole bool `json:"exposeAdminConsole,omitempty"`

	// DEPRECATED from Couchbase Server 6.5.0 onwards.
	// AdminConsoleServices is a selector to choose specific services to expose via the admin console.
	AdminConsoleServices ServiceList `json:"adminConsoleServices,omitempty"`

	// AdminConsoleServiceTemplate provides user access to every possible
	// field so they can control anything they want.
	AdminConsoleServiceTemplate *ServiceTemplateSpec `json:"adminConsoleServiceTemplate,omitempty"`

	// DEPRECATED this will be removed in a later release.
	// AdminConsoleServiceType defines whether to create a NodePort or LoadBalancer service.
	// +kubebuilder:validation:Enum=NodePort;LoadBalancer
	AdminConsoleServiceType v1.ServiceType `json:"adminConsoleServiceType,omitempty"`

	// ExposedFeatures is a list of features to expose on the K8S node
	// network.  They represent a subset of ports e.g. admin=8091,
	// xdcr=8091,8092,11210, and thus may overlap.
	ExposedFeatures ExposedFeatureList `json:"exposedFeatures,omitempty"`

	// ExposedFeatureServiceTemplate provides user access to every possible
	// field so they can control anything they want.
	ExposedFeatureServiceTemplate *ServiceTemplateSpec `json:"exposedFeatureServiceTemplate,omitempty"`

	// DEPRECATED this will be removed in a later release.
	// ExposedFeatureServiceType defines whether to create a NodePort or LoadBalancer service.
	// +kubebuilder:validation:Enum=NodePort;LoadBalancer
	ExposedFeatureServiceType v1.ServiceType `json:"exposedFeatureServiceType,omitempty"`

	// DEPRECATED this will be removed in a later release.
	// ExposedFeatureTrafficPolicy defines how packets should be routed.
	// +kubebuilder:validation:Enum=Cluster;Local
	ExposedFeatureTrafficPolicy *v1.ServiceExternalTrafficPolicyType `json:"exposedFeatureTrafficPolicy,omitempty"`

	// TLS contains the TLS configuration for the cluster.
	TLS *TLSPolicy `json:"tls,omitempty"`

	// DNS points to information for Dynamic DNS support.
	DNS *DNS `json:"dns,omitempty"`

	// DEPRECATED this will be removed in a later release.
	// ServiceAnnotations allows services to be annotated with custom labels.
	// Operator annotations are merged on top of these so have precedence as
	// they are required for correct operation.
	ServiceAnnotations map[string]string `json:"serviceAnnotations,omitempty"`

	// DEPRECATED this will be removed in a later release.
	// LoadBalancerSourceRanges applies only when an exposed service is of type
	// LoadBalancer and limits the source IP ranges that are allowed to use the
	// service.
	LoadBalancerSourceRanges IPV4PrefixList `json:"loadBalancerSourceRanges,omitempty"`

	// NetworkPlatform is used to enable support for vairous netwokring
	// technologoes.
	NetworkPlatform *NetworkPlatform `json:"networkPlatform,omitempty"`
}

type CouchbaseClusterLoggingSpec struct {
	// LogRetentionTime gives the time to keep persistent log PVCs alive for.
	// +kubebuilder:validation:Pattern="^\\d+(ns|us|ms|s|m|h)$"
	LogRetentionTime string `json:"logRetentionTime,omitempty"`

	// LogRetentionCount gives the number of persistent log PVCs to keep.
	// +kubebuilder:validation:Minimum=0
	LogRetentionCount int `json:"logRetentionCount,omitempty"`
}

// DNS contains information for Dynamic DNS support.
type DNS struct {
	// Domain is the domain to create pods in.
	Domain string `json:"domain,omitempty"`
}

// CouchbaseClusterIndexStorageSetting describes the allowed storage engines for
// databsae indexes.
type CouchbaseClusterIndexStorageSetting string

const (
	// CouchbaseClusterIndexStorageSettingMemoryOptimized uses indexes in memory.
	CouchbaseClusterIndexStorageSettingMemoryOptimized CouchbaseClusterIndexStorageSetting = "memory_optimized"

	// CouchbaseClusterIndexStorageSettingStandard uses indexes both in memory and on disk.
	CouchbaseClusterIndexStorageSettingStandard CouchbaseClusterIndexStorageSetting = "plasma"
)

type ClusterConfig struct {
	// The name of the cluster
	ClusterName string `json:"clusterName,omitempty"`

	// The amount of memory that should be allocated to the data service
	DataServiceMemQuota *resource.Quantity `json:"dataServiceMemoryQuota,omitempty"`

	// The amount of memory that should be allocated to the index service
	IndexServiceMemQuota *resource.Quantity `json:"indexServiceMemoryQuota,omitempty"`

	// The amount of memory that should be allocated to the search service
	SearchServiceMemQuota *resource.Quantity `json:"searchServiceMemoryQuota,omitempty"`

	// The amount of memory that should be allocated to the eventing service
	EventingServiceMemQuota *resource.Quantity `json:"eventingServiceMemoryQuota,omitempty"`

	// The amount of memory that should be allocated to the analytics service
	AnalyticsServiceMemQuota *resource.Quantity `json:"analyticsServiceMemoryQuota,omitempty"`

	// The index storage mode to use for secondary indexing
	IndexStorageSetting CouchbaseClusterIndexStorageSetting `json:"indexStorageSetting,omitempty"`

	// Timeout that expires to trigger the auto failover.
	AutoFailoverTimeout *metav1.Duration `json:"autoFailoverTimeout,omitempty"`

	// The number of failover events we can tolerate
	AutoFailoverMaxCount uint64 `json:"autoFailoverMaxCount,omitempty"`

	// Whether to auto failover if disk issues are detected
	AutoFailoverOnDataDiskIssues bool `json:"autoFailoverOnDataDiskIssues,omitempty"`

	// How long to wait for transient errors before failing over a faulty disk
	AutoFailoverOnDataDiskIssuesTimePeriod *metav1.Duration `json:"autoFailoverOnDataDiskIssuesTimePeriod,omitempty"`

	// Whether to enable failing over a server group
	AutoFailoverServerGroup bool `json:"autoFailoverServerGroup,omitempty"`

	// Auto-compaction settings
	AutoCompaction *AutoCompaction `json:"autoCompaction,omitempty"`
}

// DatabaseFragmentationThreshold lists triggers for when database compaction should start.
type DatabaseFragmentationThreshold struct {
	// Percent is the percentage of disk fragmentation (2-100).
	// +kubebuilder:validation:Minimum=2
	// +kubebuilder:validation:Maximum=100
	Percent *int `json:"percent,omitempty"`

	// Size is the size of disk framentation.
	Size *resource.Quantity `json:"size,omitempty"`
}

// ViewFragmentationThreshold lists triggers for when view compaction should start.
type ViewFragmentationThreshold struct {
	// Percent is the percentage of disk fragmentation (2-100).
	// +kubebuilder:validation:Minimum=2
	// +kubebuilder:validation:Maximum=100
	Percent *int `json:"percent,omitempty"`

	// Size is the size of disk framentation.
	Size *resource.Quantity `json:"size,omitempty"`
}

// TimeWindow allows the user to restrict when compaction can occur.
type TimeWindow struct {
	// Start is a string in the form HH:MM.
	// +kubebuilder:validation:Pattern="^(2[0-3]|[01]?[0-9]):([0-5]?[0-9])$"
	Start string `json:"start,omitempty"`

	// End is a string in the form HH:MM.
	// +kubebuilder:validation:Pattern="^(2[0-3]|[01]?[0-9]):([0-5]?[0-9])$"
	End string `json:"end,omitempty"`

	// AbortCompactionOutsideWindow stops compaction processes when the
	// process moves outside the window.
	AbortCompactionOutsideWindow bool `json:"abortCompactionOutsideWindow,omitempty"`
}

// AutoCompaction define auto compaction settings.
type AutoCompaction struct {
	// DatabaseFragmentationThreshold lists triggers for when database compaction should start.
	DatabaseFragmentationThreshold DatabaseFragmentationThreshold `json:"databaseFragmentationThreshold,omitempty"`

	// ViewFragmentationThreshold lists triggers for when view compaction should start.
	ViewFragmentationThreshold ViewFragmentationThreshold `json:"viewFragmentationThreshold,omitempty"`

	// ParallelCompaction controls whether database and view compactions can happen
	// in parallel.
	ParallelCompaction bool `json:"parallelCompaction,omitempty"`

	// TimeWindow allows the user to restrict when compaction can occur.
	TimeWindow TimeWindow `json:"timeWindow,omitempty"`

	// TombstonePurgeInterval controls how long to wait before purging tombstones.
	TombstonePurgeInterval *metav1.Duration `json:"tombstonePurgeInterval,omitempty"`
}

// Replications defines XDCR replications.
type Replications struct {
	// Selector allows CouchbaseReplication resources to be filtered
	// based on labels.
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

const (
	// RemoteClusterTLSCA is the key in the secret mapping to the remote cluster's
	// expected trust anchor.
	RemoteClusterTLSCA = "ca"

	// RemoteClusterTLSCertificate is the key in the secret mapping to a client
	// certificate signed by the remote cluster.
	RemoteClusterTLSCertificate = "certificate"

	// RemoteClusterTLSKey is the key in the secret mapping to a client key to
	// digitally sign data for validation by remote cluster.
	RemoteClusterTLSKey = "key"
)

// RemoteClusterTLS is a structure that can source TLS certificates from
// different sources.
type RemoteClusterTLS struct {
	// Secret references a secret containing the CA certificate, and optionally a
	// client certificate and key.
	Secret *string `json:"secret"`
}

// RemoteCluster is a reference to a remote cluster for XDCR.
type RemoteCluster struct {
	// Name of the remote cluster.  Referenced by Replications.
	Name string `json:"name,omitempty"`

	// UUID of the remote cluster.
	// +kubebuilder:validation:Pattern="^[0-9a-f]{32}$"
	UUID string `json:"uuid,omitempty"`

	// Hostname is the connection string to use to connect the remote cluster.
	// +kubebuilder:validation:Pattern="^((couchbase|couchbases|http|https)://)?[0-9a-zA-Z\\-\\.]+(:\\d+)?(\\?network=[^&]+)?$"
	Hostname string `json:"hostname,omitempty"`

	// AuthenticationSecret is a secret used to authenticate when establishing a
	// remote connection.  It is only required when not using mTLS
	AuthenticationSecret *string `json:"authenticationSecret,omitempty"`

	// Replications are replication streams from this cluster to the remote one.
	Replications Replications `json:"replications,omitempty"`

	// TLS if specified references a resource containing the necessary certificate
	// data.
	TLS *RemoteClusterTLS `json:"tls,omitempty"`
}

// XDCR allows management of XDCR settings.
type XDCR struct {
	// Managed defines whether XDCR is managed by the operator or not.
	Managed bool `json:"managed,omitempty"`

	// RemoteClusters is a set of named remote clusters to establish replications to.
	RemoteClusters []RemoteCluster `json:"remoteClusters,omitempty"`
}

type Buckets struct {
	// Managed defines whether buckets are managed by us or the clients.
	Managed bool `json:"managed,omitempty"`
	// Selector is a label selector used to list buckets in the namespace
	// that are managed by the Operator.
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

type RBAC struct {
	// Managed defines whether RBAC is managed by us or the clients.
	Managed bool `json:"managed,omitempty"`
	// Selector is a label selector used to list RBAC resources in the namespace
	// that are managed by the Operator.
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

type ServerConfig struct {
	// Size is the expected size of the couchbase cluster. The
	// couchbase-operator will eventually make the size of the running
	// cluster equal to the expected size. The vaild range of the size is
	// from 1 to 50.
	// +kubebuilder:validation:Minimum=1
	Size int `json:"size"`

	// A name for the server configuration. It must be unique.
	Name string `json:"name"`

	// The services to run on nodes created with this spec
	Services ServiceList `json:"services"`

	// ServerGroups define the set of availability zones we want to distribute
	// pods over.  This allows the Kubernetes cluster administrator to label all
	// nodes, but use a specific subset for a particular Couchbase cluster.
	ServerGroups []string `json:"serverGroups,omitempty"`

	// Pod defines the policy to create pod for the couchbase pod.
	//
	// Updating Pod does not take effect on any existing couchbase pods.
	Pod *v1.PodTemplateSpec `json:"pod,omitempty"`

	// Volume mounts represent persistent volume claims to attach to pod.
	// If defined new pods will use persistent volumes.
	VolumeMounts *VolumeMounts `json:"volumeMounts,omitempty"`

	// Resources is the resource requirements for the couchbase container.
	// This field cannot be updated once the cluster is created.
	Resources v1.ResourceRequirements `json:"resources,omitempty"`

	// List of environment variables to set in the couchbase container.
	// This is used to configure couchbase process. couchbase cluster cannot be
	// created, when bad environement variables are provided. Do not overwrite
	// any flags used to bootstrap the cluster (for example `--initial-cluster`
	// flag). This field cannot be updated.
	Env []v1.EnvVar `json:"env,omitempty"`

	// EnvFrom allows the setting of environment variables from things like
	// Secrets and ConfigMaps.
	EnvFrom []v1.EnvFromSource `json:"envFrom,omitempty"`
}

type VolumeMountName string

const (
	DefaultVolumeMount   VolumeMountName = "default"
	DataVolumeMount      VolumeMountName = "data"
	IndexVolumeMount     VolumeMountName = "index"
	AnalyticsVolumeMount VolumeMountName = "analytics"
	LogsVolumeMount      VolumeMountName = "logs"
)

type VolumeMounts struct {
	// Name of claim to use for couchbases default install path
	DefaultClaim string `json:"default,omitempty"`
	// Name of claim to use for index path
	IndexClaim string `json:"index,omitempty"`
	// Name of claim to use for data path
	DataClaim string `json:"data,omitempty"`
	// Name of claims to use for analytics paths
	AnalyticsClaims []string `json:"analytics,omitempty"`
	// Name of claim to use for logs path
	LogsClaim string `json:"logs,omitempty"`
}

// NodeToNodeEncryptionType is used to define the level of node-to-node encryption.
// +kubebuilder:validation:Enum=ControlPlaneOnly;All
type NodeToNodeEncryptionType string

const (
	// NodeToNodeControlPlaneOnly is faster but at the expense of exposing your data.
	NodeToNodeControlPlaneOnly NodeToNodeEncryptionType = "ControlPlaneOnly"

	// NodeToNodeAll all traffic should be over TLS.
	NodeToNodeAll NodeToNodeEncryptionType = "All"
)

// TLSPolicy defines the TLS policy of an couchbase cluster
type TLSPolicy struct {
	// StaticTLS enables user to generate static x509 certificates and keys,
	// put them into Kubernetes secrets, and specify them into here.
	Static *StaticTLS `json:"static,omitempty"`

	// ClientCertificatePolicy optionally defines the policy to use.
	// If set then the OperatorSecret must contain a valid
	// certificate/key pair for the Administrator account.
	ClientCertificatePolicy *ClientCertificatePolicy `json:"clientCertificatePolicy,omitempty"`

	// ClientCertificatePaths optionally defines where to look in client
	// certificates to extract the user name.
	ClientCertificatePaths []ClientCertificatePath `json:"clientCertificatePaths,omitempty"`

	// NodeToNodeEncryption specifies whether to encrypt data between Couchbase nodes
	// within the same cluster.  This may come at the expense of performance.
	NodeToNodeEncryption *NodeToNodeEncryptionType `json:"nodeToNodeEncryption,omitempty"`
}

type StaticTLS struct {
	// ServerSecret is the secret containing TLS certs used by each couchbase member pod
	// for the communication between couchbase server and its clients.
	ServerSecret string `json:"serverSecret,omitempty"`

	// OperatorSecret is the secret containing TLS certs used by operator to
	// talk securely to this cluster.
	OperatorSecret string `json:"operatorSecret,omitempty"`
}

// ClientCertificatePolicy defines the type of TLS policy to apply.  The default
// "disable" is implicit when the policy is not set.
// +kubebuilder:validation:Enum=enable;mandatory
type ClientCertificatePolicy string

const (
	// ClientCertificatePolicyEnable enables optional TLS client ceritifcates
	// falling back to password based authentication on failure.  This mode
	// is required when using secure XDCR... aledgedly, even though that can
	// have a client cert specified.
	ClientCertificatePolicyEnable ClientCertificatePolicy = "enable"

	// ClientCertificatePolicyMandatory enables mandatory TLS client certification.
	ClientCertificatePolicyMandatory ClientCertificatePolicy = "mandatory"
)

// ClientCertificatePath defines how to extract a username from a client ceritficate.
type ClientCertificatePath struct {
	// Path defines where in the X.509 specification to extract the username from.
	// Valid values are subject.cn, san.uri, san.dnsname, san.email.
	// +kubebuilder:validation:Pattern="^subject\\.cn|san\\.uri|san\\.dnsname|san\\.email$"
	Path string `json:"path"`

	// Prefix if specified allows a prefix to be stripped from the username.
	Prefix string `json:"prefix,omitempty"`

	// Delimiter if specified allows a suffix to be stripped from the username.
	Delimiter string `json:"delimiter,omitempty"`
}

type ClusterCondition struct {
	// Type is the type of condition
	Type ClusterConditionType `json:"type"`
	// Status of the condition, one of True, False, Unknown.
	Status v1.ConditionStatus `json:"status"`
	// The last time this condition was updated.
	LastUpdateTime string `json:"lastUpdateTime,omitempty"`
	// Last time the condition transitioned from one status to another.
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`
	// The reason for the condition's last transition.
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition.
	Message string `json:"message,omitempty"`
}

// +kubebuilder:validation:Enum=Available;Balanced;ManageConfig;Scaling;Upgrading;Hibernating
type ClusterConditionType string

const (
	ClusterConditionAvailable    ClusterConditionType = "Available"
	ClusterConditionBalanced     ClusterConditionType = "Balanced"
	ClusterConditionManageConfig ClusterConditionType = "ManageConfig"
	ClusterConditionScaling      ClusterConditionType = "Scaling"
	ClusterConditionUpgrading    ClusterConditionType = "Upgrading"
	ClusterConditionHibernating  ClusterConditionType = "Hibernating"
)

type ClusterStatus struct {
	// ControlPaused indicates the operator pauses the control of the cluster.
	ControlPaused bool `json:"controlPaused,omitempty"`

	// Condition keeps ten most recent cluster conditions
	Conditions []ClusterCondition `json:"conditions,omitempty"`

	// A unique cluster identifier
	ClusterID string `json:"clusterId,omitempty"`
	// Size is the current size of the cluster
	Size int `json:"size"`
	// Members are the couchbase members in the cluster
	Members *MembersStatus `json:"members,omitempty"`
	// CurrentVersion is the current cluster version
	CurrentVersion string `json:"currentVersion,omitempty"`

	// Name of buckets active within cluster
	Buckets []BucketStatus `json:"buckets,omitempty"`

	// Name of users active within cluster
	Users []string `json:"users,omitempty"`

	// Name of groups active within cluster
	Groups []string `json:"groups,omitempty"`
}

type BucketStatus struct {
	BucketName         string `json:"name"`
	BucketType         string `json:"type"`
	BucketMemoryQuota  int64  `json:"memoryQuota"`
	BucketReplicas     int    `json:"replicas"`
	IoPriority         string `json:"ioPriority"`
	EvictionPolicy     string `json:"evictionPolicy"`
	ConflictResolution string `json:"conflictResolution"`
	EnableFlush        bool   `json:"enableFlush"`
	EnableIndexReplica bool   `json:"enableIndexReplica"`
	BucketPassword     string `json:"password"`
	CompressionMode    string `json:"compressionMode"`
}

type MemberStatusList []string

type MembersStatus struct {
	// Ready are the couchbase members that are ready to serve requests
	// The member names are the same as the couchbase pod names
	Ready MemberStatusList `json:"ready,omitempty"`
	// Unready are the couchbase members not ready to serve requests
	Unready MemberStatusList `json:"unready,omitempty"`
}

// Used to marshal and unmarshal information from the Upgrading condition.
type UpgradeStatus struct {
	TargetCount int
	TotalCount  int
}

const (
	// UpgradingMessageFormat is the message format used when the cluster is upgrading.
	UpgradingMessageFormat = "Cluster upgrading (progress %d/%d)"
)

type CouchbaseClusterMonitoringSpec struct {
	Prometheus *CouchbaseClusterMonitoringPrometheusSpec `json:"prometheus,omitempty"`
}

type CouchbaseClusterMonitoringPrometheusSpec struct {
	// Enabled is a boolean that enables/disables the metrics sidecar container.
	Enabled bool `json:"enabled,omitempty"`
	// Image is the metrics image to be used to collect metrics.
	// +kubebuilder:validation:Pattern="^[\\w_\\.\\-/]+:([\\w\\d]+-)?\\d+\\.\\d+.\\d+(-[\\w\\d]+)?$"
	Image string `json:"image,omitempty"`
	// Resources is the resource requirements for the metrics container.
	// This field cannot be updated once the cluster is created.
	// Will be populated by defaults if not specified.
	Resources *v1.ResourceRequirements `json:"resources,omitempty"`
	// AuthorizationSecret is the name of a Kubernetes secret that contains a
	// bearer token to authorize GET requests to the metrics endpoint
	AuthorizationSecret *string `json:"authorizationSecret,omitempty"`
}
