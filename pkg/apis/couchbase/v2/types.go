// nolint:godot
package v2

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// BucketName is the name of a bucket.
// +kubebuilder:validation:MaxLength=100
// +kubebuilder:validation:Pattern="^[a-zA-Z0-9-_%\\.]{1,100}$"
type BucketName string

// BucketScopeOrCollectionName is the name of a fully qualifed bucket, scope or collection.
// The _default scope or collection are not valid for this type.
// As these names are period separated, and buckets can contain periods, the latter need
// to be escaped.  This specification is based on cbbackupmgr.
// +kubebuilder:validation:Pattern="^([a-zA-Z0-9\\-_%]|\\\\.){1,100}(\\.[a-zA-Z0-9\\-][a-zA-Z0-9\\-%_]{0,29}(\\.[a-zA-Z0-9\\-][a-zA-Z0-9\\-%_]{0,29})?)?$"
type BucketScopeOrCollectionName string

// BucketScopeOrCollectionNameWithDefaults is the name of a fully qualifed bucket, scope or collection.
// The _default scope and collection are valid for this type.
// As these names are period separated, and buckets can contain periods, the latter need
// to be escaped.  This specification is based on cbbackupmgr.
// +kubebuilder:validation:Pattern="^(?:[a-zA-Z0-9\\-_%]|\\\\.){1,100}(\\._default(\\._default)?|\\.[a-zA-Z0-9\\-][a-zA-Z0-9\\-%_]{0,29}(\\.[a-zA-Z0-9\\-][a-zA-Z0-9\\-%_]{0,29})?)?$"
type BucketScopeOrCollectionNameWithDefaults string

// S3BucketURI is an Amazon object storage reference.
// +kubebuilder:validation:Pattern="^s3://[a-z0-9-\\.]{3,63}$"
type S3BucketURI string

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
	// CouchbaseEphemeralBucketEvictionPolicyNoEviction never evicts a document from
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

// CouchbaseBucketConflictResolution defines the XDCR conflict resolution for a bucket.
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

// CouchbaseBackup allows automatic backup of all data from a Couchbase cluster
// into persistent storage.
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

// CouchbaseBackupSpec is allows the specification of how a Couchbase backup is
// configured, including when backups are performed, how long they are retained
// for, and where they are backed up to.
type CouchbaseBackupSpec struct {
	// Strategy defines how to perform backups.  `full_only` will only perform full
	// backups, and you must define a schedule in the `spec.full` field.  `full_incremental`
	// will perform periodic full backups, and incremental backups in between.  You must
	// define full and incremental schedules in the `spec.full` and `spec.incremental` fields
	// respectively.  Care should be taken to ensure full and incremental schedules do not
	// overlap, taking into account the backup time, as this will cause failures as the jobs
	// attempt to mount the same backup volume. This field default to `full_incremental`.
	// Info: https://docs.couchbase.com/server/current/backup-restore/cbbackupmgr-strategies.html
	// +kubebuilder:default="full_incremental"
	Strategy Strategy `json:"strategy"`

	// Incremental is the schedule on when to take incremental backups.
	// Used in Full/Incremental backup strategies.
	Incremental *CouchbaseBackupSchedule `json:"incremental,omitempty"`

	// Full is the schedule on when to take full backups.
	// Used in Full/Incremental and FullOnly backup strategies.
	Full *CouchbaseBackupSchedule `json:"full,omitempty"`

	// Amount of successful jobs to keep.
	// +kubebuilder:default=3
	// +kubebuilder:validation:Minimum=0
	SuccessfulJobsHistoryLimit int32 `json:"successfulJobsHistoryLimit,omitempty"`

	// Amount of failed jobs to keep.
	// +kubebuilder:default=3
	// +kubebuilder:validation:Minimum=0
	FailedJobsHistoryLimit int32 `json:"failedJobsHistoryLimit,omitempty"`

	// Number of times a backup job should try to execute.
	// Once it hits the BackoffLimit it will not run until the next scheduled job.
	// +kubebuilder:default=2
	BackoffLimit int32 `json:"backoffLimit,omitempty"`

	// Number of hours to hold backups for, everything older will be deleted.  More info:
	// https://golang.org/pkg/time/#ParseDuration
	// +kubebuilder:default="720h"
	BackupRetention *metav1.Duration `json:"backupRetention,omitempty"`

	// Number of hours to hold script logs for, everything older will be deleted.  More info:
	// https://golang.org/pkg/time/#ParseDuration
	// +kubebuilder:default="168h"
	LogRetention *metav1.Duration `json:"logRetention,omitempty"`

	// Size allows the specification of a backup persistent volume, when using
	// volume based backup. More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:default="20Gi"
	// +kubebuilder:validation:Type=string
	Size *resource.Quantity `json:"size,omitempty"`

	// AutoScaling allows the volume size to be dynamically increased.
	// When specified, the backup volume will start with an initial size
	// as defined by `spec.size`, and increase as required.
	AutoScaling *CouchbaseBackupAutoScaling `json:"autoScaling,omitempty"`

	// Name of StorageClass to use.
	StorageClassName *string `json:"storageClassName,omitempty"`

	// Name of S3 bucket to backup to. If non-empty this overrides local backup.
	S3Bucket S3BucketURI `json:"s3bucket,omitempty"`

	// How many threads to use during the backup.  This field defaults to 1.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	Threads int `json:"threads,omitempty"`

	// Services allows control over what services are included in the backup.
	// By default, all service data and metadata are included.  Modifications
	// to this field will only take effect on the next full backup.
	// +kubebuilder:default="x-couchbase-object"
	Services CouchbaseBackupServiceFilter `json:"services,omitempty"`

	// Data allows control over what key-value/document data is included in the
	// backup.  By default, all data is included.  Modifications
	// to this field will only take effect on the next full backup.
	Data *CouchbaseBackupDataFilter `json:"data,omitempty"`
}

// CouchbaseBackupServiceFilter allows backup filtering per-service.
// It may look the same as the restore one, implying aggregation of a common struct,
// but I've taken some liberties to make the names more consistent with the rest of
// the product.  We can converge with a V3 CRD, backups are more common than restores
// so by getting it right now, we need to do less coversion in the future.
type CouchbaseBackupServiceFilter struct {
	// BucketConfig enables the backup of bucket configuration.
	// This field defaults to `true`.
	// +kubebuilder:default=true
	BucketConfig *bool `json:"bucketConfig,omitempty"`

	// Views enables the backup of view definitions for all buckets.
	// This field defaults to `true`.
	// +kubebuilder:default=true
	Views *bool `json:"views,omitempty"`

	// GSIndexes enables the backup of global secondary index definitions for all buckets.
	// This field defaults to `true`.
	// +kubebuilder:default=true
	GSIndexes *bool `json:"gsIndexes,omitempty"`

	// FTSIndexes enables the backup of full-text search index definitions for all buckets.
	// This field defaults to `true`.
	// +kubebuilder:default=true
	FTSIndexes *bool `json:"ftsIndexes,omitempty"`

	// FTSAliases enables the backup of full-text search alias definitions.
	// This field defaults to `true`.
	// +kubebuilder:default=true
	FTSAliases *bool `json:"ftsAliases,omitempty"`

	// Data enables the backup of key-value data/documents for all buckets.
	// This can be further refined with the couchbasebackups.spec.data configuration.
	// This field defaults to `true`.
	// +kubebuilder:default=true
	Data *bool `json:"data,omitempty"`

	// Analytics enables the backup of analytics data.
	// This field defaults to `true`.
	// +kubebuilder:default=true
	Analytics *bool `json:"analytics,omitempty"`

	// Eventing enables the backup of eventing service metadata.
	// This field defaults to `true`.
	// +kubebuilder:default=true
	Eventing *bool `json:"eventing,omitempty"`

	// ClusterAnalytics enables the backup of cluster-wide analytics data, for example synonyms.
	// This field defaults to `true`.
	// +kubebuilder:default=true
	ClusterAnalytics *bool `json:"clusterAnalytics,omitempty"`

	// BucketQuery enables the backup of query metadata for all buckets.
	// This field defaults to `true`.
	// +kubebuilder:default=true
	BucketQuery *bool `json:"bucketQuery,omitempty"`

	// ClusterQuery enables the backup of cluster level query metadata.
	// This field defaults to `true`.
	// +kubebuilder:default=true
	ClusterQuery *bool `json:"clusterQuery,omitempty"`
}

// CouchbaseBackupDataFilter allows filtering of backup data by bucket, scope or collection.
type CouchbaseBackupDataFilter struct {
	// Include defines the buckets, scopes or collections that are included in the backup.
	// When this field is set, it implies that by default nothing will be backed up,
	// and data items must be explicitly included.  You may define an inclusion as a bucket
	// -- `my-bucket`, a scope -- `my-bucket.my-scope`, or a collection -- `my-bucket.my-scope.my-collection`.
	// Buckets may contain periods, and therefore must be escaped -- `my\.bucket.my-scope`, as
	// period is the separator used to delimit scopes and collections.  Included data cannot overlap
	// e.g. specifying `my-bucket` and `my-bucket.my-scope` is illegal.  This field cannot
	// be used at the same time as excluded items.
	// +listType=set
	// +kubebuilder:validation:MinItems=1
	Include []BucketScopeOrCollectionNameWithDefaults `json:"include,omitempty"`

	// Exclude defines the buckets, scopes or collections that are excluded from the backup.
	// When this field is set, it implies that by default everything will be backed up,
	// and data items can be explicitly excluded.  You may define an exclusion as a bucket
	// -- `my-bucket`, a scope -- `my-bucket.my-scope`, or a collection -- `my-bucket.my-scope.my-collection`.
	// Buckets may contain periods, and therefore must be escaped -- `my\.bucket.my-scope`, as
	// period is the separator used to delimit scopes and collections.  Excluded data cannot overlap
	// e.g. specifying `my-bucket` and `my-bucket.my-scope` is illegal.  This field cannot
	// be used at the same time as included items.
	// +listType=set
	// +kubebuilder:validation:MinItems=1
	Exclude []BucketScopeOrCollectionNameWithDefaults `json:"exclude,omitempty"`
}

type CouchbaseBackupAutoScaling struct {
	// Limit imposes a hard limit on the size we can autoscale to.  When not
	// specified no bounds are imposed. More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:validation:Type=string
	Limit *resource.Quantity `json:"limit,omitempty"`

	// ThresholdPercent determines the point at which a volume is autoscaled.
	// This represents the percentage of free space remaining on the volume,
	// when less than this threshold, it will trigger a volume expansion.
	// For example, if the volume is 100Gi, and the threshold 20%, then a resize
	// will be triggered when the used capacity exceeds 80Gi, and free space is
	// less than 20Gi.  This field defaults to 20 if not specified.
	// +kubebuilder:default=20
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=99
	ThresholdPercent int `json:"thresholdPercent,omitempty"`

	// IncrementPercent controls how much the volume is increased each time the
	// threshold is exceeded, upto a maximum as defined by the limit.
	// This field defaults to 20 if not specified.
	// +kubebuilder:default=20
	// +kubebuilder:validation:Minimum=0
	IncrementPercent int `json:"incrementPercent,omitempty"`
}

// CouchbaseBackupStatus provides status notifications about the Couchbase backup
// including when the last backup occurred, whether is succeeded or not, the run
// time of the backup and the size of the backup.
type CouchbaseBackupStatus struct {
	// CapacityUsed tells us how much of the PVC we are using. More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:validation:Type=string
	CapacityUsed *resource.Quantity `json:"capacityUsed,omitempty"`

	// Location of Backup Archive.
	Archive string `json:"archive,omitempty"`

	// Repo is where we are currently performing operations.
	Repo string `json:"repo,omitempty"`

	// Backups gives us a full list of all backups
	// and their respective repository locations.
	Backups []BackupStatus `json:"backups,omitempty"`

	// Running indicates whether a backup is currently being performed.
	Running bool `json:"running"`

	// Failed indicates whether the most recent backup has failed.
	Failed bool `json:"failed"`

	// DEPRECATED - field may no longer be populated.
	// Output reports useful information from the backup_script.
	Output string `json:"output,omitempty"`

	// DEPRECATED - field may no longer be populated.
	// Pod tells us which pod is running/ran last.
	Pod string `json:"pod,omitempty"`

	// DEPRECATED - field may no longer be populated.
	// Job tells us which job is running/ran last.
	Job string `json:"job,omitempty"`

	// DEPRECATED - field may no longer be populated.
	// Cronjob tells us which Cronjob the job belongs to.
	CronJob string `json:"cronjob,omitempty"`

	// Duration tells us how long the last backup took.  More info:
	// https://golang.org/pkg/time/#ParseDuration
	Duration *metav1.Duration `json:"duration,omitempty"`

	// LastFailure tells us the time the last failed backup failed.
	LastFailure *metav1.Time `json:"lastFailure,omitempty"`

	// LastSuccess gives us the time the last successful backup finished.
	LastSuccess *metav1.Time `json:"lastSuccess,omitempty"`

	// LastRun tells us the time the last backup job started.
	LastRun *metav1.Time `json:"lastRun,omitempty"`
}

// +kubebuilder:validation:Enum=full_incremental;full_only
type Strategy string

const (
	// Similar to Periodic but we create a new backup repository and take a full backup instead of merging.
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
	// Schedule takes a cron schedule in string format.
	Schedule string `json:"schedule"`
}

type BackupStatus struct {
	// Name of the repository.
	Name string `json:"name"`

	// Full backup inside the repository.
	Full string `json:"full,omitempty"`

	// Incremental backups inside the repository.
	Incrementals []string `json:"incrementals,omitempty"`
}

// CouchbaseBackupRestore allows the restoration of all Couchbase cluster data from
// a CouchbaseBackup resource.
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

// CouchbaseBackupRestoreSpec allows the specification of data restoration to be
// configured.  This includes the backup and repository to restore data from, and
// the time range of data to be restored.
type CouchbaseBackupRestoreSpec struct {
	// The backup resource name associated with this restore, or the backup PVC
	// name to restore from.
	Backup string `json:"backup"`

	// Repo is the backup folder to restore from.  If no repository is specified,
	// the backup container will choose the latest.
	Repo string `json:"repo"`

	// Start denotes the first backup to restore from.  This may be specified as
	// an integer index (starting from 1), a string specifying a short date
	// DD-MM-YYYY, the backup name, or one of either `start` or `oldest` keywords.
	Start *StrOrInt `json:"start,omitempty"`

	// End denotes the last backup to restore from.  Omitting this field will only
	// restore the backup referenced by start.  This may be specified as
	// an integer index (starting from 1), a string specifying a short date
	// DD-MM-YYYY, the backup name, or one of either `start` or `oldest` keywords.
	End *StrOrInt `json:"end,omitempty"`

	// Number of hours to hold restore script logs for, everything older will be deleted.
	// More info:
	// https://golang.org/pkg/time/#ParseDuration
	// +kubebuilder:default="168h"
	LogRetention *metav1.Duration `json:"logRetention,omitempty"`

	// Number of times the restore job should try to execute.
	// +kubebuilder:default=2
	BackoffLimit int32 `json:"backoffLimit,omitempty"`

	// Name of S3 bucket to restore from. If non-empty this overrides local backup.
	S3Bucket S3BucketURI `json:"s3bucket,omitempty"`

	// DEPRECATED - by spec.data.
	// Specific buckets can be explicitly included or excluded in the restore,
	// as well as bucket mappings.  This field is now ignored.
	// +kubebuilder:pruning:PreserveUnknownFields
	Buckets *runtime.RawExtension `json:"buckets,omitempty"`

	// This list accepts a certain set of parameters that will disable that data and prevent it being restored.
	// +kubebuilder:default="x-couchbase-object"
	Services CouchbaseBackupRestoreServices `json:"services,omitempty"`

	// Data allows control over what key-value/document data is included in the
	// restore.  By default, all data is included.
	Data *CouchbaseBackupRestoreDataFilter `json:"data,omitempty"`

	// How many threads to use during the restore.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	Threads int `json:"threads,omitempty"`
}

// CouchbaseBackupRestoreDataFilter allows filtering of restore data by bucket, scope or collection.
type CouchbaseBackupRestoreDataFilter struct {
	// Include defines the buckets, scopes or collections that are included in the restore.
	// When this field is set, it implies that by default nothing will be restored,
	// and data items must be explicitly included.  You may define an inclusion as a bucket
	// -- `my-bucket`, a scope -- `my-bucket.my-scope`, or a collection -- `my-bucket.my-scope.my-collection`.
	// Buckets may contain periods, and therefore must be escaped -- `my\.bucket.my-scope`, as
	// period is the separator used to delimit scopes and collections.  Included data cannot overlap
	// e.g. specifying `my-bucket` and `my-bucket.my-scope` is illegal.  This field cannot
	// be used at the same time as excluded items.
	// +listType=set
	// +kubebuilder:validation:MinItems=1
	Include []BucketScopeOrCollectionNameWithDefaults `json:"include,omitempty"`

	// Exclude defines the buckets, scopes or collections that are excluded from the backup.
	// When this field is set, it implies that by default everything will be backed up,
	// and data items can be explicitly excluded.  You may define an exclusion as a bucket
	// -- `my-bucket`, a scope -- `my-bucket.my-scope`, or a collection -- `my-bucket.my-scope.my-collection`.
	// Buckets may contain periods, and therefore must be escaped -- `my\.bucket.my-scope`, as
	// period is the separator used to delimit scopes and collections.  Excluded data cannot overlap
	// e.g. specifying `my-bucket` and `my-bucket.my-scope` is illegal.  This field cannot
	// be used at the same time as included items.
	// +listType=set
	// +kubebuilder:validation:MinItems=1
	Exclude []BucketScopeOrCollectionNameWithDefaults `json:"exclude,omitempty"`

	// Map allows data items in the restore to be remapped to a different named container.
	// Buckets can be remapped to other buckets e.g. "source=target", scopes and collections
	// can be remapped to other scopes and collections within the same bucket only e.g.
	// "bucket.scope=bucket.other" or "bucket.scope.collection=bucket.scope.other".  Map
	// sources may only be specified once, and may not overlap.
	// +listType=map
	// +listMapKey=source
	Map []RestoreMapping `json:"map,omitempty"`

	// FilterKeys only restores documents whose names match the provided regular expression.
	FilterKeys string `json:"filterKeys,omitempty"`

	// FilterValues only restores documents whose values match the provided regular expression.
	FilterValues string `json:"filterValues,omitempty"`
}

// RestoreMapping allows data to be migrated on restore.
type RestoreMapping struct {
	// Source defines the data source of the mapping, this may be either
	// a bucket, scope or collection.
	Source BucketScopeOrCollectionNameWithDefaults `json:"source"`

	// Target defines the data target of the mapping, this may be either
	// a bucket, scope or collection, and must refer to the same type
	// as the restore source.
	Target BucketScopeOrCollectionNameWithDefaults `json:"target"`
}

type CouchbaseBackupRestoreServices struct {
	// BucketConfig restores all bucket configuration settings.
	// If you are restoring to cluster with managed buckets, then this
	// option may conflict with existing bucket settings, and the results
	// are undefined, so avoid use.  This option is intended for use
	// with unmanaged buckets.  Note that bucket durability settings are
	// not restored in versions less than and equal to 1.1.0, and will
	// need to be manually applied.  This field defaults to false.
	BucketConfig bool `json:"bucketConfig,omitempty"`

	// Views restores views from the backup.  This field defaults to true.
	// +kubebuilder:default=true
	Views *bool `json:"views,omitempty"`

	// GSIIndex restores document indexes from the backup.  This field
	// defaults to true.
	// +kubebuilder:default=true
	GSIIndex *bool `json:"gsiIndex,omitempty"`

	// FTIndex restores full-text search indexes from the backup.  This
	// field defaults to true.
	// +kubebuilder:default=true
	FTIndex *bool `json:"ftIndex,omitempty"`

	// FTAlias restores full-text search aliases from the backup.  This
	// field defaults to true.
	// +kubebuilder:default=true
	FTAlias *bool `json:"ftAlias,omitempty"`

	// Data restores document data from the backup.  This field defaults
	// to true.
	// +kubebuilder:default=true
	Data *bool `json:"data,omitempty"`

	// Analytics restores analytics datasets from the backup.  This field
	// defaults to true.
	// +kubebuilder:default=true
	Analytics *bool `json:"analytics,omitempty"`

	// Eventing restores eventing functions from the backup.  This field
	// defaults to true.
	// +kubebuilder:default=true
	Eventing *bool `json:"eventing,omitempty"`

	// ClusterAnalytics enables the backup of cluster-wide analytics data, for example synonyms.
	// This field defaults to `true`.
	// +kubebuilder:default=true
	ClusterAnalytics *bool `json:"clusterAnalytics,omitempty"`

	// BucketQuery enables the backup of query metadata for all buckets.
	// This field defaults to `true`.
	// +kubebuilder:default=true
	BucketQuery *bool `json:"bucketQuery,omitempty"`

	// ClusterQuery enables the backup of cluster level query metadata.
	// This field defaults to `true`.
	// +kubebuilder:default=true
	ClusterQuery *bool `json:"clusterQuery,omitempty"`
}

// struct we use in CouchbaseBackupRestoreSpec to enforce type-safeness
type StrOrInt struct {
	// Str references an absolute backup by name.
	Str *string `json:"str,omitempty"`

	// Int references a relative backup by index.
	// +kubebuilder:validation:Minimum=1
	Int *int `json:"int,omitempty"`
}

// CouchbaseBackupRestoreStatus provides status indications of a restore from
// backup.  This includes whether or not the restore is running, whether the
// restore succeed or not, and the duration the restore took.
type CouchbaseBackupRestoreStatus struct {
	// Location of Backup Archive.
	Archive string `json:"archive,omitempty"`

	// Repo is where we are currently performing operations.
	Repo string `json:"repo,omitempty"`

	// Backups gives us a full list of all backups
	// and their respective repository locations.
	Backups []BackupStatus `json:"backups,omitempty"`

	// Running indicates whether a restore is currently being performed.
	Running bool `json:"running"`

	// Failed indicates whether the most recent restore has failed.
	Failed bool `json:"failed"`

	// DEPRECATED - field may no longer be populated.
	// Output reports useful information from the backup process.
	Output string `json:"output,omitempty"`

	// DEPRECATED - field may no longer be populated.
	// Pod tells us which pod is running/ran last.
	Pod string `json:"pod,omitempty"`

	// DEPRECATED - field may no longer be populated.
	// Job tells us which job is running/ran last.
	Job string `json:"job,omitempty"`

	// Duration tells us how long the last restore took.  More info:
	// https://golang.org/pkg/time/#ParseDuration
	Duration *metav1.Duration `json:"duration,omitempty"`

	// LastFailure tells us the time the last failed restore failed.
	LastFailure *metav1.Time `json:"lastFailure,omitempty"`

	// LastSuccess gives us the time the last successful restore finished.
	LastSuccess *metav1.Time `json:"lastSuccess,omitempty"`

	// LastRun tells us the time the last restore job started.
	LastRun *metav1.Time `json:"lastRun,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseBackupRestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseBackupRestore `json:"items"`
}

// ScopeOrCollectionName is a generic type to capture a valid
// scope or collection name.  These must consist of 1-251 characters,
// include only A-Z, a-z, 0-9, -, _ or %, and must not start with
// _ (which is an internal marker) or % (which is probably an escape
// character in language X).
// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=251
// +kubebuilder:validation:Pattern="^[a-zA-Z0-9\\-][a-zA-Z0-9\\-%_]{0,250}$"
type ScopeOrCollectionName string

// DefaultScopeOrCollection is the name of the default scope and collection.
const DefaultScopeOrCollection = "_default"

// CouchbaseCollection represent the finest grained size of data storage in Couchbase.
// Collections contain all documents and indexes in the system.  Collections also form
// the finest grain basis for role-based access control (RBAC) and cross-datacenter
// replication (XDCR).  In order to be considered by the Operator, every collection
// must be referenced by a `CouchbaseScope` or `CouchbaseScopeGroup` resource.
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:categories=all;couchbase
// +kubebuilder:resource:scope=Namespaced
type CouchbaseCollection struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec defines the desired state of the resource.
	// +optional
	// +kubebuilder:default="x-couchbase-object"
	Spec CouchbaseCollectionSpec `json:"spec"`
}

// CouchbaseCollectionSpecCommon is a set of common parameters for all collections,
// be they part of a single collection or a group.
type CouchbaseCollectionSpecCommon struct {
	// MaxTTL defines how long a document is permitted to exist for, without
	// modification, until it is automatically deleted.  This field takes precedence over
	// any TTL defined at the bucket level.  This is a default, and maximum
	// time-to-live and may be set to a lower value by the client.  If the client specifies
	// a higher value, then it is truncated to the maximum durability.  Documents are
	// removed by Couchbase, after they have expired, when either accessed, the expiry
	// pager is run, or the bucket is compacted.  When set to 0, then documents are not
	// expired by default.  This field must be a duration in the range 0-2147483648s,
	// defaulting to 0.  More info:
	// https://golang.org/pkg/time/#ParseDuration
	MaxTTL *metav1.Duration `json:"maxTTL,omitempty"`
}

type CouchbaseCollectionSpec struct {
	CouchbaseCollectionSpecCommon `json:",inline"`

	// Name specifies the name of the collection.  By default, the metadata.name is
	// used to define the collection name, however, due to the limited character set,
	// this field can be used to override the default and provide the full functionality.
	// Additionally the `metadata.name` field is a DNS label, and thus limited to 63
	// characters, this field must be used if the name is longer than this limit.
	// Collection names must be 1-251 characters in length, contain only [a-zA-Z0-9_-%]
	// and not start with either _ or %.
	Name ScopeOrCollectionName `json:"name,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseCollectionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseCollection `json:"items"`
}

// CouchbaseCollectionGroup represent the finest grained size of data storage in Couchbase.
// Collections contain all documents and indexes in the system.  Collections also form
// the finest grain basis for role-based access control (RBAC) and cross-datacenter
// replication (XDCR).  In order to be considered by the Operator, every collection group
// must be referenced by a `CouchbaseScope` or `CouchbaseScopeGroup` resource.  Unlike the
// CouchbaseCollection resource, a collection group represents multiple collections, with
// common configuration parameters, to be expressed as a single resource, minimizing required
// configuration and Kubernetes API traffic.  It also forms the basis of Couchbase RBAC
// security boundaries.
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:categories=all;couchbase
// +kubebuilder:resource:scope=Namespaced
type CouchbaseCollectionGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec defines the desired state of the resource.
	Spec CouchbaseCollectionGroupSpec `json:"spec"`
}

type CouchbaseCollectionGroupSpec struct {
	CouchbaseCollectionSpecCommon `json:",inline"`

	// Names specifies the names of the collections.  Unlike CouchbaseCollection, which
	// specifies a single collection, a collection group specifies multiple, and the
	// collection group must specify at least one collection name.
	// Any collection names specified must be unique.
	// Collection names must be 1-251 characters in length, contain only [a-zA-Z0-9_-%]
	// and not start with either _ or %.
	// +kubebuilder:validation:MinimumItems=1
	// +listType=set
	Names []ScopeOrCollectionName `json:"names"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseCollectionGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseCollectionGroup `json:"items"`
}

// CouchbaseScope represents a logical unit of data storage that sits between buckets and
// collections e.g. a bucket may contain multiple scopes, and a scope may contain multiple
// collections.  At present, scopes are not nested, so provide only a single level of
// abstraction.  Scopes provide a coarser grained basis for role-based access control (RBAC)
// and cross-datacenter replication (XDCR) than collections, but finer that buckets.
// In order to be considered by the Operator, a scope must be referenced by either a
// `CouchbaseBucket` or `CouchbaseEphemeralBucket` resource.
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:categories=all;couchbase
// +kubebuilder:resource:scope=Namespaced
type CouchbaseScope struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec defines the desired state of the resource.
	// +optional
	// +kubebuilder:default="x-couchbase-object"
	Spec CouchbaseScopeSpec `json:"spec"`
}

// CouchbaseScopeSpecCommon contains common configuration shared across single scopes
// or groups of scopes.
type CouchbaseScopeSpecCommon struct {
	// Collections defines how to collate collections included in this scope or scope group.
	// Any of the provided methods may be used to collate a set of collections to
	// manage.  Collated collections must have unique names, otherwise it is
	// considered ambiguous, and an error condition.
	Collections *CollectionSelector `json:"collections,omitempty"`
}

type CouchbaseScopeSpec struct {
	CouchbaseScopeSpecCommon `json:",inline"`

	// Name specifies the name of the scope.  By default, the metadata.name is
	// used to define the scope name, however, due to the limited character set,
	// this field can be used to override the default and provide the full functionality.
	// Additionally the `metadata.name` field is a DNS label, and thus limited to 63
	// characters, this field must be used if the name is longer than this limit.
	// Scope names must be 1-251 characters in length, contain only [a-zA-Z0-9_-%]
	// and not start with either _ or %.
	Name ScopeOrCollectionName `json:"name,omitempty"`

	// DefaultScope indicates whether this resource represents the default scope
	// for a bucket.  When set to `true`, this allows the user to refer to and
	// manage collections within the default scope.  When not defined, the Operator
	// will implicitly manage the default scope as the default scope can not be
	// deleted from Couchbase Server.  The Operator defined default scope will
	// also have the `persistDefaultCollection` flag set to `true`.  Only one
	// default scope is permitted to be contained in a bucket.
	DefaultScope bool `json:"defaultScope,omitempty"`
}

type CollectionLocalObjectReference struct {
	// Kind indicates the kind of resource that is being referenced.  A scope
	// can only reference `CouchbaseCollection` and `CouchbaseCollectionGroup`
	// resource kinds.  This field defaults to `CouchbaseCollection` if not
	// specified.
	// +kubebuilder:validation:Enum=CouchbaseCollection;CouchbaseCollectionGroup
	// +kubebuilder:default=CouchbaseCollection
	Kind CouchbaseRoleAccessKind `json:"kind,omitempty"`

	// Name is the name of the Kubernetes resource name that is being referenced.
	// Legal collection names have a maximum length of 251
	// characters and may be composed of any character from "a-z", "A-Z", "0-9" and "_-%".
	Name ScopeOrCollectionName `json:"name"`
}

type CollectionSelector struct {
	// Managed indicates whether collections within this scope are managed.
	// If not then you can dynamically create and delete collections with
	// the Couchbase UI or SDKs.
	Managed bool `json:"managed,omitempty"`

	// PreserveDefaultCollection indicates whether the Operator should manage the
	// default collection within the default scope.  The default collection can
	// be deleted, but can not be recreated by Couchbase Server.  By setting this
	// field to `true`, the Operator will implicitly manage the default collection
	// within the default scope.  The default collection cannot be modified and
	// will have no document time-to-live (TTL).  When set to `false`, the operator
	// will not manage the default collection, which will be deleted and cannot be
	// used or recreated.
	PreserveDefaultCollection bool `json:"preserveDefaultCollection,omitempty"`

	// Resources is an explicit list of named resources that will be considered
	// for inclusion in this scope or scopes.  If a resource reference doesn't
	// match a resource, then no error conditions are raised due to undefined
	// resource creation ordering and eventual consistency.
	Resources []CollectionLocalObjectReference `json:"resources,omitempty"`

	// Selector allows resources to be implicitly considered for inclusion in this
	// scope or scopes.  More info:
	// https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#labelselector-v1-meta
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseScopeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseScope `json:"items"`
}

// CouchbaseScopeGroup represents a logical unit of data storage that sits between buckets and
// collections e.g. a bucket may contain multiple scopes, and a scope may contain multiple
// collections.  At present, scopes are not nested, so provide only a single level of
// abstraction.  Scopes provide a coarser grained basis for role-based access control (RBAC)
// and cross-datacenter replication (XDCR) than collections, but finer that buckets.
// In order to be considered by the Operator, a scope must be referenced by either a
// `CouchbaseBucket` or `CouchbaseEphemeralBucket` resource.
// Unlike `CouchbaseScope` resources, scope groups represents multiple scopes, with the same
// common set of collections, to be expressed as a single resource, minimizing required
// configuration and Kubernetes API traffic.  It also forms the basis of Couchbase RBAC
// security boundaries.
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:categories=all;couchbase
// +kubebuilder:resource:scope=Namespaced
type CouchbaseScopeGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec defines the desired state of the resource.
	Spec CouchbaseScopeGroupSpec `json:"spec"`
}

type CouchbaseScopeGroupSpec struct {
	CouchbaseScopeSpecCommon `json:",inline"`

	// Names specifies the names of the scopes.  Unlike CouchbaseScope, which
	// specifies a single scope, a scope group specifies multiple, and the
	// scope group must specify at least one scope name.
	// Any scope names specified must be unique.
	// Scope names must be 1-251 characters in length, contain only [a-zA-Z0-9_-%]
	// and not start with either _ or %.
	// +kubebuilder:validation:MinimumItems=1
	// +listType=set
	Names []ScopeOrCollectionName `json:"names"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseScopeGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseScopeGroup `json:"items"`
}

type ScopeLocalObjectReference struct {
	// Kind indicates the kind of resource that is being referenced.  A scope
	// can only reference `CouchbaseScope` and `CouchbaseScopeGroup`
	// resource kinds.  This field defaults to `CouchbaseScope` if not
	// specified.
	// +kubebuilder:validation:Enum=CouchbaseScope;CouchbaseScopeGroup
	// +kubebuilder:default=CouchbaseScope
	Kind CouchbaseRoleAccessKind `json:"kind,omitempty"`

	// Name is the name of the Kubernetes resource name that is being referenced.
	// Legal scope names have a maximum length of 251
	// characters and may be composed of any character from "a-z", "A-Z", "0-9" and "_-%".
	Name ScopeOrCollectionName `json:"name"`
}

type ScopeSelector struct {
	// Managed defines whether scopes are managed for this bucket.
	// This field is `false` by default, and the Operator will take no actions that
	// will affect scopes and collections in this bucket.  The default scope and
	// collection will be present.  When set to `true`, the Operator will manage
	// user defined scopes, and optionally, their collections as defined by the
	// `CouchbaseScope`, `CouchbaseScopeGroup`, `CouchbaseCollection` and
	// `CouchbaseCollectionGroup` resource documentation.  If this field is set to
	// `false` while the  already managed, then the Operator will leave whatever
	// configuration is already present.
	Managed bool `json:"managed,omitempty"`

	// Resources is an explicit list of named resources that will be considered
	// for inclusion in this bucket.  If a resource reference doesn't
	// match a resource, then no error conditions are raised due to undefined
	// resource creation ordering and eventual consistency.
	Resources []ScopeLocalObjectReference `json:"resources,omitempty"`

	// Selector allows resources to be implicitly considered for inclusion in this
	// bucket.  More info:
	// https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#labelselector-v1-meta
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

// The CouchbaseBucket resource defines a set of documents in Couchbase server.
// A Couchbase client connects to and operates on a bucket, which provides independent
// management of a set documents and a security boundary for role based access control.
// A CouchbaseBucket provides replication and persistence for documents contained by it.
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
	// +optional
	// +kubebuilder:default="x-couchbase-object"
	Spec CouchbaseBucketSpec `json:"spec"`
}

// CouchbaseBucketSpec is the specification for a Couchbase bucket resource, and
// allows the bucket to be customized.
type CouchbaseBucketSpec struct {
	// Name is the name of the bucket within Couchbase server.  By default the Operator
	// will use the `metadata.name` field to define the bucket name.  The `metadata.name`
	// field only supports a subset of the supported character set.  When specified, this
	// field overrides `metadata.name`.  Legal bucket names have a maximum length of 100
	// characters and may be composed of any character from "a-z", "A-Z", "0-9" and "-_%\.".
	Name BucketName `json:"name,omitempty"`

	// MemoryQuota is a memory limit to the size of a bucket.  When this limit is exceeded,
	// documents will be evicted from memory to disk as defined by the eviction policy.  The
	// memory quota is defined per Couchbase pod running the data service.  This field defaults
	// to, and must be greater than or equal to 100Mi.  More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:default="100Mi"
	// +kubebuilder:validation:Type=string
	MemoryQuota *resource.Quantity `json:"memoryQuota,omitempty"`

	// Replicas defines how many copies of documents Couchbase server maintains.  This directly
	// affects how fault tolerant a Couchbase cluster is.  With a single replica, the cluster
	// can tolerate one data pod going down and still service requests without data loss.  The
	// number of replicas also affect memory use.  With a single replica, the effective memory
	// quota for documents is halved, with two replicas it is one third.  The number of replicas
	// must be between 0 and 3, defaulting to 1.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=3
	Replicas int `json:"replicas,omitempty"`

	// IOPriority controls how many threads a bucket has, per pod, to process reads and writes.
	// This field must be "low" or "high", defaulting to "low".  Modification of this field will
	// cause a temporary service disruption as threads are restarted.
	// +kubebuilder:default="low"
	IoPriority CouchbaseBucketIOPriority `json:"ioPriority,omitempty"`

	// EvictionPolicy controls how Couchbase handles memory exhaustion.  Value only eviction
	// flushes documents to disk but maintains document metadata in memory in order to improve
	// query performance.  Full eviction removes all data from memory after the document is
	// flushed to disk.  This field must be "valueOnly" or "fullEviction", defaulting to
	// "valueOnly".
	// +kubebuilder:default="valueOnly"
	EvictionPolicy CouchbaseBucketEvictionPolicy `json:"evictionPolicy,omitempty"`

	// ConflictResolution defines how XDCR handles concurrent write conflicts.  Sequence number
	// based resolution selects the document with the highest sequence number as the most recent.
	// Timestamp based resolution selects the document that was written to most recently as the
	// most recent.  This field must be "seqno" (sequence based), or "lww" (timestamp based),
	// defaulting to "seqno".
	// +kubebuilder:default="seqno"
	ConflictResolution CouchbaseBucketConflictResolution `json:"conflictResolution,omitempty"`

	// EnableFlush defines whether a client can delete all documents in a bucket.
	// This field defaults to false.
	EnableFlush bool `json:"enableFlush,omitempty"`

	// EnableIndexReplica defines whether indexes for this bucket are replicated.
	// This field defaults to false.
	EnableIndexReplica bool `json:"enableIndexReplica,omitempty"`

	// CompressionMode defines how Couchbase server handles document compression.  When
	// off, documents are stored in memory, and transferred to the client uncompressed.
	// When passive, documents are stored compressed in memory, and transferred to the
	// client compressed when requested.  When active, documents are stored compresses
	// in memory and when transferred to the client.  This field must be "off", "passive"
	// or "active", defaulting to "passive".  Be aware "off" in YAML 1.2 is a boolean, so
	// must be quoted as a string in configuration files.
	// +kubebuilder:default="passive"
	CompressionMode CouchbaseBucketCompressionMode `json:"compressionMode,omitempty"`

	// MiniumumDurability defines how durable a document write is by default, and can
	// be made more durable by the client.  This feature enables ACID transactions.
	// When none, Couchbase server will respond when the document is in memory, it will
	// become eventually consistent across the cluster.  When majority, Couchbase server will
	// respond when the document is replicated to at least half of the pods running the
	// data service in the cluster.  When majorityAndPersistActive, Couchbase server will
	// respond when the document is replicated to at least half of the pods running the
	// data service in the cluster and the document has been persisted to disk on the
	// document master pod.  When persistToMajority, Couchbase server will respond when
	// the document is replicated and persisted to disk on at least half of the pods running
	// the data service in the cluster.  This field must be either "none", "majority",
	// "majorityAndPersistActive" or "persistToMajority", defaulting to "none".
	MinimumDurability CouchbaseBucketMinimumDurability `json:"minimumDurability,omitempty"`

	// MaxTTL defines how long a document is permitted to exist for, without
	// modification, until it is automatically deleted.  This is a default and maximum
	// time-to-live and may be set to a lower value by the client.  If the client specifies
	// a higher value, then it is truncated to the maximum durability.  Documents are
	// removed by Couchbase, after they have expired, when either accessed, the expiry
	// pager is run, or the bucket is compacted.  When set to 0, then documents are not
	// expired by default.  This field must be a duration in the range 0-2147483648s,
	// defaulting to 0.  More info:
	// https://golang.org/pkg/time/#ParseDuration
	MaxTTL *metav1.Duration `json:"maxTTL,omitempty"`

	// Scopes defines whether the Operator manages scopes for the bucket or not, and
	// the set of scopes defined for the bucket.
	Scopes *ScopeSelector `json:"scopes,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseBucketList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseBucket `json:"items"`
}

// The CouchbaseEphemeralBucket resource defines a set of documents in Couchbase server.
// A Couchbase client connects to and operates on a bucket, which provides independent
// management of a set documents and a security boundary for role based access control.
// A CouchbaseEphemeralBucket provides in-memory only storage and replication for documents
// contained by it.
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
	// +optional
	// +kubebuilder:default="x-couchbase-object"
	Spec CouchbaseEphemeralBucketSpec `json:"spec"`
}

// CouchbaseEphemeralBucketSpec is the specification for an ephemeral Couchbase bucket
// resource, and allows the bucket to be customized.
type CouchbaseEphemeralBucketSpec struct {
	// Name is the name of the bucket within Couchbase server.  By default the Operator
	// will use the `metadata.name` field to define the bucket name.  The `metadata.name`
	// field only supports a subset of the supported character set.  When specified, this
	// field overrides `metadata.name`.  Legal bucket names have a maximum length of 100
	// characters and may be composed of any character from "a-z", "A-Z", "0-9" and "-_%\.".
	Name BucketName `json:"name,omitempty"`

	// MemoryQuota is a memory limit to the size of a bucket.  When this limit is exceeded,
	// documents will be evicted from memory defined by the eviction policy.  The memory quota
	// is defined per Couchbase pod running the data service.  This field defaults to, and must
	// be greater than or equal to 100Mi.  More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:default="100Mi"
	// +kubebuilder:validation:Type=string
	MemoryQuota *resource.Quantity `json:"memoryQuota,omitempty"`

	// Replicas defines how many copies of documents Couchbase server maintains.  This directly
	// affects how fault tolerant a Couchbase cluster is.  With a single replica, the cluster
	// can tolerate one data pod going down and still service requests without data loss.  The
	// number of replicas also affect memory use.  With a single replica, the effective memory
	// quota for documents is halved, with two replicas it is one third.  The number of replicas
	// must be between 0 and 3, defaulting to 1.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=3
	Replicas int `json:"replicas,omitempty"`

	// IOPriority controls how many threads a bucket has, per pod, to process reads and writes.
	// This field must be "low" or "high", defaulting to "low".  Modification of this field will
	// cause a temporary service disruption as threads are restarted.
	// +kubebuilder:default="low"
	IoPriority CouchbaseBucketIOPriority `json:"ioPriority,omitempty"`

	// EvictionPolicy controls how Couchbase handles memory exhaustion.  No eviction means
	// that Couchbase server will make this bucket read-only when memory is exhausted in
	// order to avoid data loss.  NRU eviction will delete documents that haven't been used
	// recently in order to free up memory. This field must be "noEviction" or "nruEviction",
	// defaulting to "noEviction".
	// +kubebuilder:default="noEviction"
	EvictionPolicy CouchbaseEphemeralBucketEvictionPolicy `json:"evictionPolicy,omitempty"`

	// ConflictResolution defines how XDCR handles concurrent write conflicts.  Sequence number
	// based resolution selects the document with the highest sequence number as the most recent.
	// Timestamp based resolution selects the document that was written to most recently as the
	// most recent.  This field must be "seqno" (sequence based), or "lww" (timestamp based),
	// defaulting to "seqno".
	// +kubebuilder:default="seqno"
	ConflictResolution CouchbaseBucketConflictResolution `json:"conflictResolution,omitempty"`

	// EnableFlush defines whether a client can delete all documents in a bucket.
	// This field defaults to false.
	EnableFlush bool `json:"enableFlush,omitempty"`

	// CompressionMode defines how Couchbase server handles document compression.  When
	// off, documents are stored in memory, and transferred to the client uncompressed.
	// When passive, documents are stored compressed in memory, and transferred to the
	// client compressed when requested.  When active, documents are stored compresses
	// in memory and when transferred to the client.  This field must be "off", "passive"
	// or "active", defaulting to "passive".  Be aware "off" in YAML 1.2 is a boolean, so
	// must be quoted as a string in configuration files.
	// +kubebuilder:default="passive"
	CompressionMode CouchbaseBucketCompressionMode `json:"compressionMode,omitempty"`

	// MiniumumDurability defines how durable a document write is by default, and can
	// be made more durable by the client.  This feature enables ACID transactions.
	// When none, Couchbase server will respond when the document is in memory, it will
	// become eventually consistent across the cluster.  When majority, Couchbase server will
	// respond when the document is replicated to at least half of the pods running the
	// data service in the cluster.  This field must be either "none" or "majority",
	// defaulting to "none".
	MinimumDurability CouchbaseEphemeralBucketMinimumDurability `json:"minimumDurability,omitempty"`

	// MaxTTL defines how long a document is permitted to exist for, without
	// modification, until it is automatically deleted.  This is a default and maximum
	// time-to-live and may be set to a lower value by the client.  If the client specifies
	// a higher value, then it is truncated to the maximum durability.  Documents are
	// removed by Couchbase, after they have expired, when either accessed, the expiry
	// pager is run, or the bucket is compacted.  When set to 0, then documents are not
	// expired by default.  This field must be a duration in the range 0-2147483648s,
	// defaulting to 0.  More info:
	// https://golang.org/pkg/time/#ParseDuration
	MaxTTL *metav1.Duration `json:"maxTTL,omitempty"`

	// Scopes defines whether the Operator manages scopes for the bucket or not, and
	// the set of scopes defined for the bucket.
	Scopes *ScopeSelector `json:"scopes,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseEphemeralBucketList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseEphemeralBucket `json:"items"`
}

// The CouchbaseMemcachedBucket resource defines a set of documents in Couchbase server.
// A Couchbase client connects to and operates on a bucket, which provides independent
// management of a set documents and a security boundary for role based access control.
// A CouchbaseEphemeralBucket provides in-memory only storage for documents contained by it.
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:categories=all;couchbase
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="memory quota",type="string",JSONPath=".spec.memoryQuota"
// +kubebuilder:printcolumn:name="age",type="date",JSONPath=".metadata.creationTimestamp"
type CouchbaseMemcachedBucket struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +optional
	// +kubebuilder:default="x-couchbase-object"
	Spec CouchbaseMemcachedBucketSpec `json:"spec"`
}

// CouchbaseMemcachedBucketSpec is the specification for a Memcached bucket
// resource, and allows the bucket to be customized.
type CouchbaseMemcachedBucketSpec struct {
	// Name is the name of the bucket within Couchbase server.  By default the Operator
	// will use the `metadata.name` field to define the bucket name.  The `metadata.name`
	// field only supports a subset of the supported character set.  When specified, this
	// field overrides `metadata.name`.  Legal bucket names have a maximum length of 100
	// characters and may be composed of any character from "a-z", "A-Z", "0-9" and "-_%\.".
	Name BucketName `json:"name,omitempty"`

	// MemoryQuota is a memory limit to the size of a bucket. The memory quota
	// is defined per Couchbase pod running the data service.  This field defaults to, and must
	// be greater than or equal to 100Mi.  More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:default="100Mi"
	// +kubebuilder:validation:Type=string
	MemoryQuota *resource.Quantity `json:"memoryQuota,omitempty"`

	// EnableFlush defines whether a client can delete all documents in a bucket.
	// This field defaults to false.
	EnableFlush bool `json:"enableFlush,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseMemcachedBucketList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseMemcachedBucket `json:"items"`
}

// The CouchbaseReplication resource represents a Couchbase-to-Couchbase, XDCR replication
// stream from a source bucket to a destination bucket.  This provides off-site backup,
// migration, and disaster recovery.
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
	// The explicit mappings to use for replication which are optional.
	// For Scopes and Collection replication support we can specify a set of implicit and
	// explicit mappings to use. If none is specified then it is assumed to be existing
	// bucket level replication.
	// https://docs.couchbase.com/server/current/learn/clusters-and-availability/xdcr-with-scopes-and-collections.html#explicit-mapping
	ExplicitMapping CouchbaseExplicitMappingSpec `json:"explicitMapping,omitempty"`
}

// The CouchbaseScopeMigration resource represents the use of the special migration mapping
// within XDCR to take a filtered list from the default scope and collection of the source bucket,
// replicate it to named scopes and collections within the target bucket.
// The bucket-to-bucket replication cannot duplicate any used by the CouchbaseReplication resource,
// as these two types of replication are mutually exclusive between buckets.
// https://docs.couchbase.com/server/current/learn/clusters-and-availability/xdcr-with-scopes-and-collections.html#migration
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:categories=all;couchbase
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="bucket",type="string",JSONPath=".spec.bucket"
// +kubebuilder:printcolumn:name="remote bucket",type="string",JSONPath=".spec.remoteBucket"
// +kubebuilder:printcolumn:name="paused",type="boolean",JSONPath=".spec.paused"
// +kubebuilder:printcolumn:name="age",type="date",JSONPath=".metadata.creationTimestamp"
type CouchbaseMigrationReplication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CouchbaseReplicationSpec `json:"spec"`
	// The migration mappings to use, should never be empty as that is just an implicit bucket-to-bucket replication then.
	MigrationMapping CouchbaseMigrationMappingSpec `json:"migrationMapping"`
}

// CompressionType represents all allowable XDCR compression modes.
type CompressionType string

const (
	// CompressionTypeNone applies no compression
	CompressionTypeNone CompressionType = "None"

	// CompressionTypeAuto automatically applies compression based on internal heuristics.
	CompressionTypeAuto CompressionType = "Auto"
)

// For the replication we can use the default scope or collection so we are reusing
// the type for ScopeOrCollectionName but with extra support for _default
// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=251
// +kubebuilder:validation:Pattern="^(_default|[a-zA-Z0-9\\-][a-zA-Z0-9\\-%_]{0,250})$"
type ScopeOrCollectionNameIncludingDefault string

// For replication we can source and target either a scope or a scope.collection which is
// referred to as a keyspace. We use the same validation as the ScopeOrCollectionName type
// but also support usage of _default scopes and collections.
// https://docs.couchbase.com/server/current/learn/clusters-and-availability/xdcr-with-scopes-and-collections.html
type CouchbaseReplicationKeyspace struct {
	// The scope to use.
	Scope ScopeOrCollectionNameIncludingDefault `json:"scope"`
	// The optional collection within the scope. May be empty to just work at scope level.
	Collection ScopeOrCollectionNameIncludingDefault `json:"collection,omitempty"`
}

// The various rules we want to define for an explicit mapping between scopes and collections in the
// source and target buckets.
type CouchbaseExplicitMappingSpec struct {
	// The list of explicit replications to carry out including any nested implicit replications:
	// specifying a scope implicitly replicates all collections within it.
	// There should be no duplicates, including more-specific duplicates, e.g. if you specify replication
	// of a scope then you can only deny replication of collections within it.
	AllowRules []CouchbaseAllowReplicationMapping `json:"allowRules,omitempty"`
	// The list of explicit replications to prevent including any nested implicit denials:
	// specifying a scope implicitly denies all collections within it.
	// There should be no duplicates, including more-specific duplicates, e.g. if you specify denial of
	// replication of a scope then you can only specify replication of collections within it.
	DenyRules []CouchbaseDenyReplicationMapping `json:"denyRules,omitempty"`
}

// CouchbaseAllowReplicationMapping is to cover Scope and Collection explicit replication.
// If a scope is defined then it implicitly allows all collections unless a more specific
// CouchbaseDenyReplicationMapping rule is present to block it.
// Once a rule is defined at scope level it should not be redefined at collection level.
// https://docs.couchbase.com/server/current/learn/clusters-and-availability/xdcr-with-scopes-and-collections.html
type CouchbaseAllowReplicationMapping struct {
	// The source keyspace: where to replicate from.
	// Source and target must match whether they have a collection or not, i.e. you cannot
	// replicate from a scope to a collection.
	SourceKeyspace CouchbaseReplicationKeyspace `json:"sourceKeyspace"`
	// The target keyspace: where to replicate to.
	// Source and target must match whether they have a collection or not, i.e. you cannot
	// replicate from a scope to a collection.
	TargetKeyspace CouchbaseReplicationKeyspace `json:"targetKeyspace"`
}

// Provide rules to block implicit replication at scope or collection level.
// You may want to implicitly map all scopes or collections except a specific one (or set) so this
// is a better way to express that by creating rules just for those to deny.
type CouchbaseDenyReplicationMapping struct {
	// The source keyspace: where to block replication from.
	SourceKeyspace CouchbaseReplicationKeyspace `json:"sourceKeyspace"`
}

// Provide all the migration mapping replication details and rules.
type CouchbaseMigrationMappingSpec struct {
	// The migration mappings to use, should never be empty as that is just an implicit bucket-to-bucket replication then.
	// +kubebuilder:validation:MinimumItems=1
	Mappings []CouchbaseMigrationMapping `json:"mappings"`
}

// Indicates whether this is using migration mapping or not.
// This is only valid when using the default scope/collection.
type CouchbaseMigrationMapping struct {
	// A filter to select from the source default scope and collection.
	// Defaults to select everything in the default scope and collection.
	// +kubebuilder:default="_default._default"
	Filter string `json:"filter,omitempty"`
	// The destination of our migration, must be a scope and collection.
	TargetKeyspace CouchbaseReplicationKeyspace `json:"targetKeyspace"`
}

// CouchbaseReplicationSpec allows configuration of an XDCR replication.
type CouchbaseReplicationSpec struct {
	// Bucket is the source bucket to replicate from.  This refers to the Couchbase
	// bucket name, not the resource name of the bucket.  A bucket with this name must
	// be defined on this cluster.  Legal bucket names have a maximum length of 100
	// characters and may be composed of any character from "a-z", "A-Z", "0-9" and "-_%\.".
	Bucket BucketName `json:"bucket"`

	// RemoteBucket is the remote bucket name to synchronize to.  This refers to the
	// Couchbase bucket name, not the resource name of the bucket.  Legal bucket names
	// have a maximum length of 100 characters and may be composed of any character from
	// "a-z", "A-Z", "0-9" and "-_%\.".
	RemoteBucket BucketName `json:"remoteBucket"`

	// CompressionType is the type of compression to apply to the replication.
	// When None, no compression will be applied to documents as they are
	// transferred between clusters.  When Auto, Couchbase server will automatically
	// compress documents as they are transferred to reduce bandwidth requirements.
	// This field must be one of "None" or "Auto", defaulting to "Auto".
	// +kubebuilder:default="Auto"
	// +kubebuilder:validation:Enum=None;Auto
	CompressionType CompressionType `json:"compressionType,omitempty"`

	// FilterExpression allows certain documents to be filtered out of the replication.
	FilterExpression string `json:"filterExpression,omitempty"`

	// Paused allows a replication to be stopped and restarted without having to
	// restart the replication from the beginning.
	Paused bool `json:"paused,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseReplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseReplication `json:"items"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseMigrationReplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseMigrationReplication `json:"items"`
}

// CouchbaseUser allows the automation of Couchbase user management.
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

// CouchbaseUserSpec allows the specification of Couchbase user configuration.
type CouchbaseUserSpec struct {
	// Full Name of Couchbase user.
	FullName string `json:"fullName,omitempty"`

	// The domain which provides user authentication.
	// +kubebuilder:validation:Enum=local;external
	AuthDomain AuthDomain `json:"authDomain"`

	// Name of Kubernetes secret with password for Couchbase domain.
	AuthSecret string `json:"authSecret,omitempty"`
}

// CouchbaseUserList is a list of Couchbase users.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseUserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseUser `json:"items"`
}

// CouchbaseGroup allows the automation of Couchbase group management.
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:categories=all;couchbase
// +kubebuilder:resource:scope=Namespaced
type CouchbaseGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CouchbaseGroupSpec `json:"spec"`
}

// CouchbaseGroupSpec allows the specification of Couchbase group configuration.
type CouchbaseGroupSpec struct {
	// Roles is a list of roles that this group is granted.
	Roles []Role `json:"roles"`

	// LDAPGroupRef is a reference to an LDAP group.
	LDAPGroupRef string `json:"ldapGroupRef,omitempty"`
}

// RoleName is a type-safe enumeration of all supported role names.
type RoleName string

const (
	RoleFullAdmin    RoleName = "admin"
	RoleClusterAdmin RoleName = "cluster_admin"
	// RoleSecurityAdmin Does not exist in 7.0.0.
	RoleSecurityAdmin                       RoleName = "security_admin"
	RoleSecurityAdminLocal                  RoleName = "security_admin_local"
	RoleSecurityAdminExternal               RoleName = "security_admin_external"
	RoleReadOnlyAdmin                       RoleName = "ro_admin"
	RoleExternalStatsReader                 RoleName = "external_stats_reader"
	RoleXDCRAdmin                           RoleName = "replication_admin"
	RoleQueryCurlAccess                     RoleName = "query_external_access"
	RoleQuestySystemAccess                  RoleName = "query_system_catalog"
	RoleQueryManageGlobalFunctions          RoleName = "query_manage_global_functions"
	RoleQueryExecuteGlobalFunctions         RoleName = "query_execute_global_functions"
	RoleQueryManageFunctions                RoleName = "query_manage_functions"
	RoleQueryExecuteFunctions               RoleName = "query_execute_functions"
	RoleQueryManageGlobalExternalFunctions  RoleName = "query_manage_global_external_functions"
	RoleQueryExecuteGlobalExternalFunctions RoleName = "query_execute_global_external_functions"
	RoleQueryManageExternalFunctions        RoleName = "query_manage_external_functions"
	RoleQueryExecuteExternalFunctions       RoleName = "query_execute_external_functions"
	RoleAnalyticsReader                     RoleName = "analytics_reader"
	RoleBucketAdmin                         RoleName = "bucket_admin"
	RoleScopeAdmin                          RoleName = "scope_admin"
	RoleViewsAdmin                          RoleName = "views_admin"
	RoleSearchAdmin                         RoleName = "fts_admin"
	RoleApplicationAccess                   RoleName = "bucket_full_access"
	RoleDataReader                          RoleName = "data_reader"
	RoleDataWriter                          RoleName = "data_writer"
	RoleDCPReader                           RoleName = "data_dcp_reader"
	RoleBackup                              RoleName = "data_backup"
	RoleMonitor                             RoleName = "data_monitoring"
	RoleXDCRInbound                         RoleName = "replication_target"
	RoleAnalyticsManager                    RoleName = "analytics_manager"
	RoleViewsReader                         RoleName = "views_reader"
	RoleSearchReader                        RoleName = "fts_searcher"
	RoleQuerySelect                         RoleName = "query_select"
	RoleQueryUpdate                         RoleName = "query_update"
	RoleQueryInsert                         RoleName = "query_insert"
	RoleQueryDelete                         RoleName = "query_delete"
	RoleQueryManageIndex                    RoleName = "query_manage_index"
	RoleSyncGateway                         RoleName = "mobile_sync_gateway"
	RoleBackupAdmin                         RoleName = "backup_admin"
	RoleAnalyticsSelect                     RoleName = "analytics_select"
	RoleAnalyticsAdmin                      RoleName = "analytics_admin"
)

type Role struct {
	// Name of role.
	// +kubebuilder:validation:Enum=admin;cluster_admin;security_admin;ro_admin;replication_admin;query_external_access;query_system_catalog;analytics_reader;bucket_admin;views_admin;fts_admin;bucket_full_access;data_reader;data_writer;data_dcp_reader;data_backup;data_monitoring;replication_target;analytics_manager;views_reader;fts_searcher;query_select;query_update;query_insert;query_delete;query_manage_index;mobile_sync_gateway
	Name RoleName `json:"name"`

	// Bucket name for bucket admin roles.  When not specified for a role that can be scoped
	// to a specific bucket, the role will apply to all buckets in the cluster.
	// Deprecated:  Couchbase Autonomous Operator 2.3
	// +kubebuilder:validation:Pattern="^\\*$|^[a-zA-Z0-9-_%\\.]+$"
	Bucket string `json:"bucket,omitempty"`

	// Bucket level access to apply to specified role. The bucket must exist.  When not specified,
	// the bucket field will be checked. If both are empty and the role can be scoped to a specific bucket, the role
	// will apply to all buckets in the cluster
	Buckets BucketRoleSpec `json:"buckets,omitempty"`

	// Scope level access to apply to specified role.  The scope must exist.  When not specified,
	// the role will apply to selected bucket or all buckets in the cluster.
	Scopes ScopeRoleSpec `json:"scopes,omitempty"`

	// Collection level access to apply to the specified role.  The collection must exist.
	// When not specified, the role is subject to scope or bucket level access.
	Collections CollectionRoleSpec `json:"collections,omitempty"`
}

type BucketRoleSpec struct {
	// Resources is an explicit list of named bucket resources that will be considered
	// for inclusion in this role.  If a resource reference doesn't
	// match a resource, then no error conditions are raised due to undefined
	// resource creation ordering and eventual consistency.
	Resources []BucketLocalObjectReference `json:"resources,omitempty"`
	// Selector allows resources to be implicitly considered for inclusion in this
	// role.  More info:
	// https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#labelselector-v1-meta
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

type BucketLocalObjectReference struct {
	// Kind indicates the kind of resource that is being referenced.  A Role
	// can only reference `CouchbaseBucket` kind.  This field defaults
	// to `CouchbaseBucket` if not specified.
	// +kubebuilder:validation:Enum=CouchbaseBucket
	// +kubebuilder:default=CouchbaseBucket
	Kind CouchbaseRoleAccessKind `json:"kind,omitempty"`

	// Name is the name of the Kubernetes resource name that is being referenced.
	Name string `json:"name"`
}

type ScopeRoleSpec struct {

	// Resources is an explicit list of named resources that will be considered
	// for inclusion in this scope or scopes.  If a resource reference doesn't
	// match a resource, then no error conditions are raised due to undefined
	// resource creation ordering and eventual consistency.
	Resources []ScopeLocalObjectReference `json:"resources,omitempty"`

	// Selector allows resources to be implicitly considered for inclusion in this
	// scope or scopes.  More info:
	// https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#labelselector-v1-meta
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

type CouchbaseRoleAccessKind string

const (
	CouchbaseScopeKind           CouchbaseRoleAccessKind = "CouchbaseScope"
	CouchbaseScopeGroupKind      CouchbaseRoleAccessKind = "CouchbaseScopeGroup"
	CouchbaseCollectionKind      CouchbaseRoleAccessKind = "CouchbaseCollection"
	CouchbaseCollectionGroupKind CouchbaseRoleAccessKind = "CouchbaseCollectionGroup"
)

type CollectionRoleSpec struct {

	// Resources is an explicit list of named resources that will be considered
	// for inclusion in this collection or collections.  If a resource reference doesn't
	// match a resource, then no error conditions are raised due to undefined
	// resource creation ordering and eventual consistency.
	Resources []CollectionLocalObjectReference `json:"resources,omitempty"`

	// Selector allows resources to be implicitly considered for inclusion in this
	// collection or collections.  More info:
	// https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#labelselector-v1-meta
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

// CouchbaseGroupList is a list of Couchbase users.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseGroup `json:"items"`
}

// CouchbaseRoleBinding allows association of Couchbase users with groups.
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:categories=all;couchbase
// +kubebuilder:resource:scope=Namespaced
type CouchbaseRoleBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CouchbaseRoleBindingSpec `json:"spec"`
}

// CouchbaseRoleBindingSpec defines the group of subjects i.e. users, and the
// role i.e. group they are a member of.
type CouchbaseRoleBindingSpec struct {
	// List of users to bind a role to.
	Subjects []CouchbaseRoleBindingSubject `json:"subjects"`

	// CouchbaseGroup being bound to subjects.
	RoleRef CouchbaseRoleBindingRef `json:"roleRef"`
}

// +kubebuilder:validation:Enum=CouchbaseUser
type RoleBindingSubjectType string

const (
	// RoleBindingSubjectTypeUser applies role to Couchbase User
	RoleBindingSubjectTypeUser RoleBindingSubjectType = "CouchbaseUser"
)

// +kubebuilder:validation:Enum=CouchbaseGroup
type RoleBindingReferenceType string

const (
	// RoleBindingReferenceTypeGroup applies role to Couchbase Group
	RoleBindingReferenceTypeGroup RoleBindingReferenceType = "CouchbaseGroup"
)

type CouchbaseRoleBindingSubject struct {
	// Couchbase user/group kind.
	Kind RoleBindingSubjectType `json:"kind"`

	// Name of Couchbase user resource.
	Name string `json:"name"`
}

type CouchbaseRoleBindingRef struct {
	// Kind of role to use for binding.
	Kind RoleBindingReferenceType `json:"kind"`

	// Name of role resource to use for binding.
	Name string `json:"name"`
}

// CouchbaseRoleBindingList is a list of Couchbase users.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseRoleBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseRoleBinding `json:"items"`
}

// CouchbaseAutoscaler provides an interface for the Kubernetes Horizontal Pod Autoscaler
// to interactive with the Couchbase cluster and provide autoscaling.  This resource is
// not defined by the end user, and is managed by the Operator.
// +genclient
// +genclient:method=GetScale,verb=get,subresource=scale,result=k8s.io/api/autoscaling/v1.Scale
// +genclient:method=UpdateScale,verb=update,subresource=scale,input=k8s.io/api/autoscaling/v1.Scale,result=k8s.io/api/autoscaling/v1.Scale
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:categories=all;couchbase
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:resource:shortName=cba
// +kubebuilder:printcolumn:name="size",type="string",JSONPath=".spec.size"
// +kubebuilder:printcolumn:name="servers",type="string",JSONPath=".spec.servers"
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.size,statuspath=.status.size,selectorpath=.status.labelSelector
type CouchbaseAutoscaler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CouchbaseAutoscalerSpec   `json:"spec"`
	Status            CouchbaseAutoscalerStatus `json:"status,omitempty"`
}

// CouchbaseAutoscalerSpec allows control over an autoscaling group.
type CouchbaseAutoscalerSpec struct {
	// Servers specifies the server group that this autoscaler belongs to.
	// +kubebuilder:validation:MinLength=1
	Servers string `json:"servers"`

	// Size allows the server group to be dynamically scaled.
	// +kubebuilder:validation:Minimum=0
	Size int `json:"size"`
}

// CouchbaseAutoscalerStatus provides information to the HPA to assist with scaling
// server groups.
type CouchbaseAutoscalerStatus struct {
	// LabelSelector allows the HPA to select resources to monitor for resource
	// utilization in order to trigger scaling.
	LabelSelector string `json:"labelSelector"`

	// Size is the current size of the server group.
	// +kubebuilder:validation:Minimum=1
	Size int `json:"size"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// CouchbaseAutoscalerList is a list of Couchbase Autoscaler resources.
type CouchbaseAutoscalerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseAutoscaler `json:"items"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// CouchbaseClusterList is a list of Couchbase clusters.
type CouchbaseClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseCluster `json:"items"`
}

// The CouchbaseCluster resource represents a Couchbase cluster.  It allows configuration
// of cluster topology, networking, storage and security options.
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
// +kubebuilder:validation:Enum=PrioritizeDataIntegrity;PrioritizeUptime
type RecoveryPolicy string

const (
	// PrioritizeDataIntegrity listens to Couchbase Server, assuming it knows
	// best what it is safe to do.  This is the default value.
	PrioritizeDataIntegrity RecoveryPolicy = "PrioritizeDataIntegrity"

	// PrioritizeUptime ignores Couchbase server after the auto-failover time
	// period and may result in data loss.  This is perfect for in-memory caches
	// as they can tolerate such behaviour.  It also handles things like ephemeral
	// pods in a supported cluster e.g. losing all of your query nodes it not a
	// big problem.
	PrioritizeUptime RecoveryPolicy = "PrioritizeUptime"
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

// RollingUpgradeConstraints allows the upgrade to be constrained.
type RollingUpgradeConstraints struct {
	// MaxUpgradablePercent allows the number of pods affected by an upgrade at any
	// one time to be increased.  By default a rolling upgrade will
	// upgrade one pod at a time.  This field allows that limit to be removed.
	// This field must be an integer percentage, e.g. "10%", in the range 1% to 100%.
	// Percentages are relative to the total cluster size, and rounded down to
	// the nearest whole number, with a minimum of 1.  For example, a 10 pod
	// cluster, and 25% allowed to upgrade, would yield 2.5 pods per iteration,
	// rounded down to 2.
	// The smallest of `maxUpgradable` and `maxUpgradablePercent` takes precedence if
	// both are defined.
	// +kubebuilder:validation:Pattern="^(100|[1-9][0-9]|[1-9])%$"
	MaxUpgradablePercent string `json:"maxUpgradablePercent,omitempty"`

	// MaxUpgradable allows the number of pods affected by an upgrade at any
	// one time to be increased.  By default a rolling upgrade will
	// upgrade one pod at a time.  This field allows that limit to be removed.
	// This field must be greater than zero.
	// The smallest of `maxUpgradable` and `maxUpgradablePercent` takes precedence if
	// both are defined.
	// +kubebuilder:validation:Minimum=1
	MaxUpgradable int `json:"maxUpgradable,omitempty"`
}

// HibernationStrategy defines how aggressive to be when putting a cluster to sleep.
// +kubebuilder:validation:Enum=Immediate
type HibernationStrategy string

const (
	// ImmediateHibernation is a forced hibernation and immediate terminates all
	// pods.  This can be used in cases where clients are keeping the cluster
	// awake by committing write traffic.
	ImmediateHibernation HibernationStrategy = "Immediate"
)

// ClusterSpec is the specification for a CouchbaseCluster resources, and allows
// the cluster to be customized.
type ClusterSpec struct {
	// Image is the container image name that will be used to launch Couchbase
	// server instances.  Updating this field will cause an automatic upgrade of
	// the cluster.
	// +kubebuilder:validation:Pattern="^(.*?(:\\d+)?/)?.*?/.*?(:.*?\\d+\\.\\d+\\.\\d+.*|@sha256:[0-9a-f]{64})$"
	Image string `json:"image"`

	// Paused is to pause the control of the operator for the Couchbase cluster.
	// This does not pause the cluster itself, instead stopping the operator from
	// taking any action.
	Paused bool `json:"paused,omitempty"`

	// Hibernate is whether to hibernate the cluster.
	Hibernate bool `json:"hibernate,omitempty"`

	// HibernationStrategy defines how to hibernate the cluster.  When Immediate
	// the Operator will immediately delete all pods and take no further action until
	// the hibernate field is set to false.
	HibernationStrategy *HibernationStrategy `json:"hibernationStrategy,omitempty"`

	// RecoveryPolicy controls how aggressive the Operator is when recovering cluster
	// topology.  When PrioritizeDataIntegrity, the Operator will delegate failover
	// exclusively to Couchbase server, relying on it to only allow recovery when safe to
	// do so.  When PrioritizeUptime, the Operator will wait for a period after the
	// expected auto-failover of the cluster, before forcefully failing-over the pods.
	// This may cause data loss, and is only expected to be used on clusters with ephemeral
	// data, where the loss of the pod means that the data is known to be unrecoverable.
	// This field must be either "PrioritizeDataIntegrity" or "PrioritizeUptime", defaulting
	// to "PrioritizeDataIntegrity".
	RecoveryPolicy *RecoveryPolicy `json:"recoveryPolicy,omitempty"`

	// UpgradeStrategy controls how aggressive the Operator is when performing a cluster
	// upgrade.  When a rolling upgrade is requested, pods are upgraded one at a time.  This
	// strategy is slower, however less disruptive.  When an immediate upgrade strategy is
	// requested, all pods are upgraded at the same time.  This strategy is faster, but more
	// disruptive.  This field must be either "RollingUpgrade" or "ImmediateUpgrade", defaulting
	// to "RollingUpgrade".
	UpgradeStrategy *UpgradeStrategy `json:"upgradeStrategy,omitempty"`

	// When `spec.upgradeStrategy` is set to `RollingUpgrade` it will, by default, upgrade one pod
	// at a time.  If this field is specified then that number can be increased.
	RollingUpgrade *RollingUpgradeConstraints `json:"rollingUpgrade,omitempty"`

	// AutoResourceAllocation populates pod resource requests based on the services running
	// on that pod.  When enabled, this feature will calculate the memory request as the
	// total of service allocations defined in `spec.cluster`, plus an overhead defined
	// by `spec.autoResourceAllocation.overheadPercent`.Changing individual allocations for
	// a service will cause a cluster upgrade as allocations are modified in the underlying
	// pods.  This field also allows default pod CPU requests and limits to be applied.
	// All resource allocations can be overridden by explicitly configuring them in the
	// `spec.servers.resources` field.
	AutoResourceAllocation *AutoResourceAllocation `json:"autoResourceAllocation,omitempty"`

	// AntiAffinity forces the Operator to schedule different Couchbase server pods on
	// different Kubernetes nodes.  Anti-affinity reduces the likelihood of unrecoverable
	// failure in the event of a node issue.  Use of anti-affinity is highly recommended for
	// production clusters.
	AntiAffinity bool `json:"antiAffinity,omitempty"`

	// ClusterSettings define Couchbase cluster-wide settings such as memory allocation,
	// failover characteristics and index settings.
	// +optional
	// +kubebuilder:default="x-couchbase-object"
	ClusterSettings ClusterConfig `json:"cluster"`

	// SoftwareUpdateNotifications enables software update notifications in the UI.
	// When enabled, the UI will alert when a Couchbase server upgrade is available.
	SoftwareUpdateNotifications bool `json:"softwareUpdateNotifications,omitempty"`

	// VolumeClaimTemplates define the desired characteristics of a volume
	// that can be requested/claimed by a pod, for example the storage class to
	// use and the volume size.  Volume claim templates are referred to by name
	// by server class volume mount configuration.
	VolumeClaimTemplates []PersistentVolumeClaimTemplate `json:"volumeClaimTemplates,omitempty"`

	// ServerGroups define the set of availability zones you want to distribute
	// pods over, and construct Couchbase server groups for.  By default, most
	// cloud providers will label nodes with the key "topology.kubernetes.io/zone",
	// the values associated with that key are used here to provide explicit
	// scheduling by the Operator.  You may manually label nodes using the
	// "topology.kubernetes.io/zone" key, to provide failure-domain
	// aware scheduling when none is provided for you.  Global server groups are
	// applied to all server classes, and may be overridden on a per-server class
	// basis to give more control over scheduling and server groups.
	// +listType=set
	ServerGroups []string `json:"serverGroups,omitempty"`

	// SecurityContext allows the configuration of the security context for all
	// Couchbase server pods.  When using persistent volumes you may need to set
	// the fsGroup field in order to write to the volume.  For non-root clusters
	// you must also set runAsUser to 1000, corresponding to the Couchbase user
	// in official container images.  More info:
	// https://kubernetes.io/docs/tasks/configure-pod-container/security-context/
	SecurityContext *v1.PodSecurityContext `json:"securityContext,omitempty"`

	// Platform gives a hint as to what platform we are running on and how
	// to configure services.  This field must be one of "aws", "gke" or "azure".
	Platform PlatformType `json:"platform,omitempty"`

	// Security defines Couchbase cluster security options such as the administrator
	// account username and password, and user RBAC settings.
	Security CouchbaseClusterSecuritySpec `json:"security"`

	// Networking defines Couchbase cluster networking options such as network
	// topology, TLS and DDNS settings.
	Networking CouchbaseClusterNetworkingSpec `json:"networking,omitempty"`

	// Logging defines Operator logging options.
	Logging CouchbaseClusterLoggingSpec `json:"logging,omitempty"`

	// Servers defines server classes for the Operator to provision and manage.
	// A server class defines what services are running and how many members make
	// up that class.  Specifying multiple server classes allows the Operator to
	// provision clusters with Multi-Dimensional Scaling (MDS).  At least one server
	// class must be defined, and at least one server class must be running the data
	// service.
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MinItems=1
	Servers []ServerConfig `json:"servers"`

	// Buckets defines whether the Operator should manage buckets, and how to lookup
	// bucket resources.
	Buckets Buckets `json:"buckets,omitempty"`

	// XDCR defines whether the Operator should manage XDCR, remote clusters and how
	// to lookup replication resources.
	XDCR XDCR `json:"xdcr,omitempty"`

	// Monitoring defines any Operator managed integration into 3rd party monitoring
	// infrastructure.
	Monitoring *CouchbaseClusterMonitoringSpec `json:"monitoring,omitempty"`

	// Backup defines whether the Operator should manage automated backups, and how
	// to lookup backup resources.
	Backup Backup `json:"backup,omitempty"`

	// DEPRECATED - This option only exists for backwards compatibility and no longer
	// restricts autoscaling to ephemeral services.
	// EnablePreviewScaling enables autoscaling for stateful services and buckets.
	EnablePreviewScaling bool `json:"enablePreviewScaling,omitempty"`

	// AutoscaleStabilizationPeriod defines how long after a rebalance the
	// corresponding HorizontalPodAutoscaler should remain in maintenance mode.
	// During maintenance mode all autoscaling is disabled since every HorizontalPodAutoscaler
	// associated with the cluster becomes inactive.
	// Since certain metrics can be unpredictable when Couchbase is rebalancing or upgrading,
	// setting a stabilization period helps to prevent scaling recommendations from the
	// HorizontalPodAutoscaler for a provided period of time.
	//
	// Values must be a valid Kubernetes duration of 0s or higher:
	// https://golang.org/pkg/time/#ParseDuration
	// A value of 0, puts the cluster in maintenance mode during rebalance but
	// immediately exits this mode once the rebalance has completed.
	// When undefined, the HPA is never put into maintenance mode during rebalance.
	AutoscaleStabilizationPeriod *metav1.Duration `json:"autoscaleStabilizationPeriod,omitempty"`

	// EnableOnlineVolumeExpansion enables online expansion of Persistent Volumes.
	// You can only expand a PVC if its storage class's "allowVolumeExpansion" field is set to true.
	// Additionally, Kubernetes feature "ExpandInUsePersistentVolumes" must be enabled in order to
	// expand the volumes which are actively bound to Pods.
	// Volumes can only be expanded and not reduced to a smaller size.
	// See: https://kubernetes.io/docs/concepts/storage/persistent-volumes/#resizing-an-in-use-persistentvolumeclaim
	//
	// If "EnableOnlineVolumeExpansion" is enabled for use within an environment that does
	// not actually support online volume and file system expansion then the cluster will fallback to
	// rolling upgrade procedure to create a new set of Pods for use with resized Volumes.
	// More info:  https://kubernetes.io/docs/concepts/storage/persistent-volumes/#expanding-persistent-volumes-claims
	EnableOnlineVolumeExpansion bool `json:"enableOnlineVolumeExpansion,omitempty"`
}

type PersistentVolumeClaimTemplate struct {
	ObjectMeta NamedObjectMeta              `json:"metadata"`
	Spec       v1.PersistentVolumeClaimSpec `json:"spec"`
}

type Backup struct {
	// Managed defines whether backups are managed by us or the clients.
	Managed bool `json:"managed,omitempty"`

	// The Backup Image to run on backup pods.
	Image string `json:"image"`

	// The Service Account to run backup (and restore) pods under.
	// Without this backup pods will not be able to update status.
	ServiceAccount string `json:"serviceAccountName,omitempty"`

	// NodeSelector defines which nodes to constrain the pods that
	// run any backup and restore operations to.
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Resources is the resource requirements for the backup and restore
	// containers.  Will be populated by defaults if not specified.
	Resources *v1.ResourceRequirements `json:"resources,omitempty"`

	// Selector allows CouchbaseBackup and CouchbaseBackupRestore
	// resources to be filtered based on labels.
	Selector *metav1.LabelSelector `json:"selector,omitempty"`

	// Tolerations specifies all backup and restore pod tolerations.
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`

	// ImagePullSecrets allow you to use an image from private
	// repositories and non-dockerhub ones.
	ImagePullSecrets []v1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// S3Secret contains the region and credentials for operating backups in S3.
	// This field must be popluated when the `spec.s3bucket` field is specified
	// for a backup or restore resource.
	S3Secret string `json:"s3Secret,omitempty"`
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
	// BindSecret is the name of a Kubernetes secret to use containing password for LDAP user binding
	BindSecret string `json:"bindSecret"`

	// TLSSecret is the name of a Kubernetes secret to use for LDAP ca cert.
	TLSSecret string `json:"tlsSecret,omitempty"`

	// Enables using LDAP to authenticate users.
	// +kubebuilder:default=true
	AuthenticationEnabled bool `json:"authenticationEnabled,omitempty"`

	// Enables use of LDAP groups for authorization.
	AuthorizationEnabled bool `json:"authorizationEnabled,omitempty"`

	// List of LDAP hosts.
	// +kubebuilder:validation:MinItems=1
	Hosts []string `json:"hosts"`

	// LDAP port.
	// This is typically 389 for LDAP, and 636 for LDAPS.
	// +kubebuilder:default=389
	Port int `json:"port"`

	// Encryption method to communicate with LDAP servers.
	// Can be StartTLSExtension, TLS, or false.
	Encryption LDAPEncryption `json:"encryption,omitempty"`

	// Whether server certificate validation be enabled
	EnableCertValidation bool `json:"serverCertValidation,omitempty"`

	// CA Certificate in PEM format to be used in LDAP server certificate validation
	CACert string `json:"cacert,omitempty"`

	// LDAP query, to get the users' groups by username in RFC4516 format.  More info:
	// https://docs.couchbase.com/server/current/manage/manage-security/configure-ldap.html
	GroupsQuery string `json:"groupsQuery,omitempty"`

	// DN to use for searching users and groups synchronization. More info:
	// https://docs.couchbase.com/server/current/manage/manage-security/configure-ldap.html
	BindDN string `json:"bindDN,omitempty"`

	// User to distinguished name (DN) mapping. If none is specified,
	// the username is used as the user’s distinguished name.  More info:
	// https://docs.couchbase.com/server/current/manage/manage-security/configure-ldap.html
	UserDNMapping LDAPUserDNMapping `json:"userDNMapping,omitempty"`

	// If enabled Couchbase server will try to recursively search for groups
	// for every discovered ldap group. groups_query will be user for the search.
	// More info:
	// https://docs.couchbase.com/server/current/manage/manage-security/configure-ldap.html
	NestedGroupsEnabled bool `json:"nestedGroupsEnabled,omitempty"`

	// Maximum number of recursive groups requests the server is allowed to perform.
	// Requires NestedGroupsEnabled.  Values between 1 and 100: the default is 10.
	// More info:
	// https://docs.couchbase.com/server/current/manage/manage-security/configure-ldap.html
	// +kubebuilder:default=10
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	NestedGroupsMaxDepth uint64 `json:"nestedGroupsMaxDepth,omitempty"`

	// Lifetime of values in cache in milliseconds. Default 300000 ms.  More info:
	// https://docs.couchbase.com/server/current/manage/manage-security/configure-ldap.html
	// +kubebuilder:default=30000
	CacheValueLifetime uint64 `json:"cacheValueLifetime,omitempty"`
}

type LDAPUserDNMapping struct {
	// This field specifies list of templates to use for providing username to DN mapping.
	// The template may contain a placeholder specified as `%u` to represent the Couchbase
	// user who is attempting to gain access.
	Template string `json:"template,omitempty"`

	// Query is the LDAP query to run to map from Couchbase user to LDAP distinguished name.
	Query string `json:"query,omitempty"`
}

type CouchbaseClusterSecuritySpec struct {
	// AdminSecret is the name of a Kubernetes secret to use for administrator authentication.
	// The admin secret must contain the keys "username" and "password".  The password data
	// must be at least 6 characters in length, and not contain the any of the characters
	// `()<>,;:\"/[]?={}`.
	AdminSecret string `json:"adminSecret"`

	// Couchbase RBAC Users
	RBAC RBAC `json:"rbac,omitempty"`

	// LDAP Settings
	LDAP *CouchbaseClusterLDAPSpec `json:"ldap,omitempty"`
}

// +kubebuilder:validation:Pattern="^\\d{1,3}\\.\\d{1,3}\\.\\d{1,3}\\.\\d{1,3}/\\d{1,2}$"
type IPv4Prefix string

type IPV4PrefixList []IPv4Prefix

// Standard objects metadata.  This is a curated version for use with Couchbase
// resource templates.
type ObjectMeta struct {
	// Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects. May match selectors of replication controllers
	// and services. More info: http://kubernetes.io/docs/user-guide/labels
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations is an unstructured key value map stored with a resource that
	// may be set by external tools to store and retrieve arbitrary metadata. They
	// are not queryable and should be preserved when modifying objects. More
	// info: http://kubernetes.io/docs/user-guide/annotations
	Annotations map[string]string `json:"annotations,omitempty"`
}

// Standard objects metadata.  This is a curated version for use with Couchbase
// resource templates.
type NamedObjectMeta struct {
	// Name must be unique within a namespace. Is required when creating
	// resources, although some resources may allow a client to request the
	// generation of an appropriate name automatically. Name is primarily intended
	// for creation idempotence and configuration definition. Cannot be updated.
	// More info: http://kubernetes.io/docs/user-guide/identifiers#names
	Name string `json:"name"`

	// Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects. May match selectors of replication controllers
	// and services. More info: http://kubernetes.io/docs/user-guide/labels
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations is an unstructured key value map stored with a resource that
	// may be set by external tools to store and retrieve arbitrary metadata. They
	// are not queryable and should be preserved when modifying objects. More
	// info: http://kubernetes.io/docs/user-guide/annotations
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ServiceTemplateSpec is a sanitized version of a service that exposes
// what we allow a user to modify.
type ServiceTemplateSpec struct {
	ObjectMeta `json:"metadata,omitempty"`
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

// AddressFamily allows you to select the IP protocol version.
// +kubebuilder:validation:Enum=IPv4;IPv6
type AddressFamily string

const (
	AFInet AddressFamily = "IPv4"

	AFInet6 AddressFamily = "IPv6"
)

type CouchbaseClusterNetworkingSpec struct {
	// AddressFamily allows the manual selection of the address family to use.
	// When this field is not set, Couchbase server will default to using IPv4
	// for internal communication and also support IPv6 on dual stack systems.
	// Setting this field to either IPv4 or IPv6 will force Couchbase to use the
	// selected protocol for internal communication, and also disable all other
	// protocols to provide added security and simplicty when defining firewall
	// rules.  Disabling of address families is only supported in Couchbase
	// Server 7.0.2+.
	AddressFamily *AddressFamily `json:"addressFamily,omitempty"`

	// ExposeAdminConsole creates a service referencing the admin console.
	// The service is configured by the adminConsoleServiceTemplate field.
	ExposeAdminConsole bool `json:"exposeAdminConsole,omitempty"`

	// DEPRECATED - not required by Couchbase Server 6.5.0 onward.
	// AdminConsoleServices is a selector to choose specific services to expose via the admin
	// console. This field may contain any of "data", "index", "query", "search", "eventing"
	// and "analytics".  Each service may only be included once.
	// +listType=set
	AdminConsoleServices []Service `json:"adminConsoleServices,omitempty"`

	// AdminConsoleServiceTemplate provides a template used by the Operator to create
	// and manage the admin console service.  This allows services to be annotated, the
	// service type defined and any other options that Kubernetes provides.  When using
	// a LoadBalancer service type, TLS and dynamic DNS must also be enabled. The Operator
	// reserves the right to modify or replace any field.  More info:
	// https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#service-v1-core
	AdminConsoleServiceTemplate *ServiceTemplateSpec `json:"adminConsoleServiceTemplate,omitempty"`

	// DEPRECATED - by adminConsoleServiceTemplate.
	// AdminConsoleServiceType defines whether to create a node port or load balancer service.
	// When using a LoadBalancer service type, TLS and dynamic DNS must also be enabled.
	// This field must be one of "NodePort" or "LoadBalancer", defaulting to "NodePort".
	// +kubebuilder:default="NodePort"
	// +kubebuilder:validation:Enum=NodePort;LoadBalancer
	AdminConsoleServiceType v1.ServiceType `json:"adminConsoleServiceType,omitempty"`

	// ExposedFeatures is a list of Couchbase features to expose when using a networking
	// model that exposes the Couchbase cluster externally to Kubernetes.  This field also
	// triggers the creation of per-pod services used by clients to connect to the Couchbase
	// cluster.  When admin, only the administrator port is exposed, allowing remote
	// administration.  When xdcr, only the services required for remote replication are exposed.
	// The xdcr feature is only required when the cluster is the destination of an XDCR
	// replication.  When client, all services are exposed as required for client SDK operation.
	// This field may contain any of "admin", "xdcr" and "client".  Each feature may only be
	// included once.
	// +listType=set
	ExposedFeatures []ExposedFeature `json:"exposedFeatures,omitempty"`

	// ExposedFeatureServiceTemplate provides a template used by the Operator to create
	// and manage per-pod services.  This allows services to be annotated, the
	// service type defined and any other options that Kubernetes provides.  When using
	// a LoadBalancer service type, TLS and dynamic DNS must also be enabled. The Operator
	// reserves the right to modify or replace any field.  More info:
	// https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#service-v1-core
	ExposedFeatureServiceTemplate *ServiceTemplateSpec `json:"exposedFeatureServiceTemplate,omitempty"`

	// DEPRECATED - by exposedFeatureServiceTemplate.
	// ExposedFeatureServiceType defines whether to create a node port or load balancer service.
	// When using a LoadBalancer service type, TLS and dynamic DNS must also be enabled.
	// This field must be one of "NodePort" or "LoadBalancer", defaulting to "NodePort".
	// +kubebuilder:default="NodePort"
	// +kubebuilder:validation:Enum=NodePort;LoadBalancer
	ExposedFeatureServiceType v1.ServiceType `json:"exposedFeatureServiceType,omitempty"`

	// DEPRECATED  - by exposedFeatureServiceTemplate.
	// ExposedFeatureTrafficPolicy defines how packets should be routed from a load balancer
	// service to a Couchbase pod.  When local, traffic is routed directly to the pod.  When
	// cluster, traffic is routed to any node, then forwarded on.  While cluster routing may be
	// slower, there are some situations where it is required for connectivity.  This field
	// must be either "Cluster" or "Local", defaulting to "Local",
	// +kubebuilder:validation:Enum=Cluster;Local
	ExposedFeatureTrafficPolicy *v1.ServiceExternalTrafficPolicyType `json:"exposedFeatureTrafficPolicy,omitempty"`

	// TLS defines the TLS configuration for the cluster including
	// server and client certificate configuration, and TLS security policies.
	TLS *TLSPolicy `json:"tls,omitempty"`

	// DNS defines information required for Dynamic DNS support.
	DNS *DNS `json:"dns,omitempty"`

	// DEPRECATED - by adminConsoleServiceTemplate and exposedFeatureServiceTemplate.
	// ServiceAnnotations allows services to be annotated with custom labels.
	// Operator annotations are merged on top of these so have precedence as
	// they are required for correct operation.
	ServiceAnnotations map[string]string `json:"serviceAnnotations,omitempty"`

	// DEPRECATED - by adminConsoleServiceTemplate and exposedFeatureServiceTemplate.
	// LoadBalancerSourceRanges applies only when an exposed service is of type
	// LoadBalancer and limits the source IP ranges that are allowed to use the
	// service.  Items must use IPv4 class-less interdomain routing (CIDR) notation
	// e.g. 10.0.0.0/16.
	LoadBalancerSourceRanges IPV4PrefixList `json:"loadBalancerSourceRanges,omitempty"`

	// NetworkPlatform is used to enable support for various networking
	// technologies.  This field must be one of "Istio".
	NetworkPlatform *NetworkPlatform `json:"networkPlatform,omitempty"`

	// DisableUIOverHTTP is used to explicitly enable and disable UI access over
	// the HTTP protocol.  If not specified, this field defaults to false.
	DisableUIOverHTTP bool `json:"disableUIOverHTTP,omitempty"`

	// DisableUIOverHTTPS is used to explicitly enable and disable UI access over
	// the HTTPS protocol.  If not specified, this field defaults to false.
	DisableUIOverHTTPS bool `json:"disableUIOverHTTPS,omitempty"`

	// WaitForAddressReachableDelay is used to defer operator checks that
	// ensure external addresses are reachable before new nodes are balanced
	// in to the cluster.  This prevents negative DNS caching while waiting
	// for external-DDNS controllers to propagate addresses.
	// +kubebuilder:default="2m"
	WaitForAddressReachableDelay *metav1.Duration `json:"waitForAddressReachableDelay,omitempty"`

	// WaitForAddressReachable is used to set the timeout between when polling of
	// external addresses is started, and when it is deemed a failure.  Polling of
	// DNS name availability inherently dangerous due to negative caching, so prefer
	// the use of an initial `waitForAddressReachableDelay` to allow propagation.
	// +kubebuilder:default="10m"
	WaitForAddressReachable *metav1.Duration `json:"waitForAddressReachable,omitempty"`
}

type CouchbaseClusterLoggingSpec struct {
	// LogRetentionTime gives the time to keep persistent log PVCs alive for.
	// +kubebuilder:validation:Pattern="^\\d+(ns|us|ms|s|m|h)$"
	LogRetentionTime string `json:"logRetentionTime,omitempty"`

	// LogRetentionCount gives the number of persistent log PVCs to keep.
	// +kubebuilder:validation:Minimum=0
	LogRetentionCount int `json:"logRetentionCount,omitempty"`

	// Specification of all logging configuration required to manage the sidecar containers in each pod.
	Server *CouchbaseClusterLoggingConfigurationSpec `json:"server,omitempty"`

	// Used to manage the audit configuration directly
	Audit *CouchbaseClusterAuditLoggingSpec `json:"audit,omitempty"`
}

// DNS contains information for Dynamic DNS support.
type DNS struct {
	// Domain is the domain to create pods in.  When populated the Operator
	// will annotate the admin console and per-pod services with the key
	// "external-dns.alpha.kubernetes.io/hostname".  These annotations can
	// be used directly by a Kubernetes External-DNS controller to replicate
	// load balancer service IP addresses into a public DNS server.
	Domain string `json:"domain,omitempty"`
}

// AutoResourceAllocation automatically populates Kubernetes resource requests.
type AutoResourceAllocation struct {
	// Enabled defines whether auto-resource allocation is enabled.
	Enabled bool `json:"enabled,omitempty"`

	// OverheadPercent defines the amount of memory above that required for individual
	// services on a pod.  For Couchbase Server this should be approximately 25%.
	// +kubebuilder:default=25
	// +kubebuilder:validation:Minimum=0
	OverheadPercent int `json:"overheadPercent,omitempty"`

	// CPURequests automatically populates the CPU requests across all Couchbase
	// server pods.  The default value of "2", is the minimum recommended number of
	// CPUs required to run Couchbase Server.  Explicitly specifying the CPU request
	// for a particular server class will override this value. More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:default="2"
	// +kubebuilder:validation:Type=string
	CPURequests *resource.Quantity `json:"cpuRequests,omitempty"`

	// CPULimits automatically populates the CPU limits across all Couchbase
	// server pods.  This field defaults to "4" CPUs.  Explicitly specifying the CPU
	// limit for a particular server class will override this value.  More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:default="4"
	// +kubebuilder:validation:Type=string
	CPULimits *resource.Quantity `json:"cpuLimits,omitempty"`
}

// CouchbaseClusterIndexStorageSetting describes the allowed storage engines for
// databsae indexes.
// +kubebuilder:validation:Enum=memory_optimized;plasma
type CouchbaseClusterIndexStorageSetting string

const (
	// CouchbaseClusterIndexStorageSettingMemoryOptimized uses indexes in memory.
	CouchbaseClusterIndexStorageSettingMemoryOptimized CouchbaseClusterIndexStorageSetting = "memory_optimized"

	// CouchbaseClusterIndexStorageSettingStandard uses indexes both in memory and on disk.
	CouchbaseClusterIndexStorageSettingStandard CouchbaseClusterIndexStorageSetting = "plasma"
)

type ClusterConfig struct {
	// ClusterName defines the name of the cluster, as displayed in the Couchbase UI.
	// By default, the cluster name is that specified in the CouchbaseCluster resource's
	// metadata.
	ClusterName string `json:"clusterName,omitempty"`

	// DataServiceMemQuota is the amount of memory that should be allocated to the data service.
	// This value is per-pod, and only applicable to pods belonging to server classes running
	// the data service.  This field must be a quantity greater than or equal to 256Mi.  This
	// field defaults to 256Mi.  More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:default="256Mi"
	// +kubebuilder:validation:Type=string
	DataServiceMemQuota *resource.Quantity `json:"dataServiceMemoryQuota,omitempty"`

	// IndexServiceMemQuota is the amount of memory that should be allocated to the index service.
	// This value is per-pod, and only applicable to pods belonging to server classes running
	// the index service.  This field must be a quantity greater than or equal to 256Mi.  This
	// field defaults to 256Mi.  More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:default="256Mi"
	// +kubebuilder:validation:Type=string
	IndexServiceMemQuota *resource.Quantity `json:"indexServiceMemoryQuota,omitempty"`

	// QueryServiceMemQuota is a dummy field.  By default, Couchbase server provides no
	// memory resource constraints for the query service, so this has no effect on Couchbase
	// server.  It is, however, used when the spec.autoResourceAllocation feature is enabled,
	// and is used to define the amount of memory reserved by the query service for use with
	// Kubernetes resource scheduling. More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:validation:Type=string
	QueryServiceMemQuota *resource.Quantity `json:"queryServiceMemoryQuota,omitempty"`

	// SearchServiceMemQuota is the amount of memory that should be allocated to the search service.
	// This value is per-pod, and only applicable to pods belonging to server classes running
	// the search service.  This field must be a quantity greater than or equal to 256Mi.  This
	// field defaults to 256Mi.  More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:default="256Mi"
	// +kubebuilder:validation:Type=string
	SearchServiceMemQuota *resource.Quantity `json:"searchServiceMemoryQuota,omitempty"`

	// EventingServiceMemQuota is the amount of memory that should be allocated to the eventing service.
	// This value is per-pod, and only applicable to pods belonging to server classes running
	// the eventing service.  This field must be a quantity greater than or equal to 256Mi.  This
	// field defaults to 256Mi.  More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:default="256Mi"
	// +kubebuilder:validation:Type=string
	EventingServiceMemQuota *resource.Quantity `json:"eventingServiceMemoryQuota,omitempty"`

	// AnalyticsServiceMemQuota is the amount of memory that should be allocated to the analytics service.
	// This value is per-pod, and only applicable to pods belonging to server classes running
	// the analytics service.  This field must be a quantity greater than or equal to 1Gi.  This
	// field defaults to 1Gi.  More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:default="1Gi"
	// +kubebuilder:validation:Type=string
	AnalyticsServiceMemQuota *resource.Quantity `json:"analyticsServiceMemoryQuota,omitempty"`

	// DEPRECATED - by indexer.
	// The index storage mode to use for secondary indexing.  This field must be one of
	// "memory_optimized" or "plasma", defaulting to "memory_optimized".  This field is
	// immutable and cannot be changed unless there are no server classes running the
	// index service in the cluster.
	// +kubebuilder:default="memory_optimized"
	IndexStorageSetting CouchbaseClusterIndexStorageSetting `json:"indexStorageSetting,omitempty"`

	// Data allows the data service to be configured.
	Data *CouchbaseClusterDataSettings `json:"data,omitempty"`

	// Indexer allows the indexer to be configured.
	Indexer *CouchbaseClusterIndexerSettings `json:"indexer,omitempty"`

	// Query allows the query service to be configured.
	Query *CouchbaseClusterQuerySettings `json:"query,omitempty"`

	// AutoFailoverTimeout defines how long Couchbase server will wait between a pod
	// being witnessed as down, until when it will failover the pod.  Couchbase server
	// will only failover pods if it deems it safe to do so, and not result in data
	// loss.  This field must be in the range 5-3600s, defaulting to 120s.
	// More info:  https://golang.org/pkg/time/#ParseDuration
	// +kubebuilder:default="120s"
	AutoFailoverTimeout *metav1.Duration `json:"autoFailoverTimeout,omitempty"`

	// AutoFailoverMaxCount is the maximum number of automatic failovers Couchbase server
	// will allow before not allowing any more.  This field must be between 1-3, default 3.
	// +kubebuilder:default=3
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=3
	AutoFailoverMaxCount uint64 `json:"autoFailoverMaxCount,omitempty"`

	// AutoFailoverOnDataDiskIssues defines whether Couchbase server should failover a pod
	// if a disk issue was detected.
	AutoFailoverOnDataDiskIssues bool `json:"autoFailoverOnDataDiskIssues,omitempty"`

	// AutoFailoverOnDataDiskIssuesTimePeriod defines how long to wait for transient errors
	// before failing over a faulty disk.  This field must be in the range 5-3600s, defaulting
	// to 120s.  More info:  https://golang.org/pkg/time/#ParseDuration
	// +kubebuilder:default="120s"
	AutoFailoverOnDataDiskIssuesTimePeriod *metav1.Duration `json:"autoFailoverOnDataDiskIssuesTimePeriod,omitempty"`

	// AutoFailoverServerGroup whether to enable failing over a server group.
	AutoFailoverServerGroup bool `json:"autoFailoverServerGroup,omitempty"`

	// AutoCompaction allows the configuration of auto-compaction, including on what
	// conditions disk space is reclaimed and when it is allowed to run.
	// +kubebuilder:default="x-couchbase-object"
	AutoCompaction *AutoCompaction `json:"autoCompaction,omitempty"`
}

// IndexerLogLevel controls the verbosity of indexer logs.
// +kubebuilder:validation:Enum=silent;fatal;error;warn;info;verbose;timing;debug;trace
type IndexerLogLevel string

const (
	IndexerLogLevelSilent  IndexerLogLevel = "silent"
	IndexerLogLevelFatal   IndexerLogLevel = "fatal"
	IndexerLogLevelError   IndexerLogLevel = "error"
	IndexerLogLevelWarn    IndexerLogLevel = "warn"
	IndexerLogLevelInfo    IndexerLogLevel = "info"
	IndexerLogLevelVerbose IndexerLogLevel = "verbose"
	IndexerLogLevelTiming  IndexerLogLevel = "timing"
	IndexerLogLevelDebug   IndexerLogLevel = "debug"
	IndexerLogLevelTrace   IndexerLogLevel = "trace"
)

// CouchbaseClusterIndexerSettings allow the indexer to be configured.
type CouchbaseClusterIndexerSettings struct {
	// Threads controls the number of processor threads to use for indexing.
	// A value of 0 means 1 per CPU.  This attribute must be greater
	// than or equal to 0, defaulting to 0.
	// +kubebuilder:validation:Minimum=0
	Threads int `json:"threads,omitempty"`

	// LogLevel controls the verbosity of indexer logs.  This field must be one of
	// "silent", "fatal", "error", "warn", "info", "verbose", "timing", "debug" or
	// "trace", defaulting to "info".
	// +kubebuilder:default="info"
	LogLevel IndexerLogLevel `json:"logLevel,omitempty"`

	// MaxRollbackPoints controls the number of checkpoints that can be rolled
	// back to.  The default is 2, with a minimum of 1.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=2
	MaxRollbackPoints int `json:"maxRollbackPoints,omitempty"`

	// MemorySnapshotInterval controls when memory indexes should be snapshotted.
	// This defaults to 200ms, and must be greater than or equal to 1ms.
	// +kubebuilder:default="200ms"
	MemorySnapshotInterval *metav1.Duration `json:"memorySnapshotInterval,omitempty"`

	// StableSnapshotInterval controls when disk indexes should be snapshotted.
	// This defaults to 5s, and must be greater than or equal to 1ms.
	// +kubebuilder:default="5s"
	StableSnapshotInterval *metav1.Duration `json:"stableSnapshotInterval,omitempty"`

	// StorageMode controls the underlying storage engine for indexes.  Once set
	// it can only be modified if there are no nodes in the cluster running the
	// index service.  The field must be one of "memory_optimized" or "plasma",
	// defaulting to "memory_optimized".
	// +kubebuilder:default="memory_optimized"
	StorageMode CouchbaseClusterIndexStorageSetting `json:"storageMode,omitempty"`
}

// CouchbaseClusterQuerySettings allow query tweaks.
type CouchbaseClusterQuerySettings struct {
	// BackfillEnabled allows the query service to backfill.
	// +kubebuilder:default=true
	BackfillEnabled *bool `json:"backfillEnabled,omitempty"`

	// TemporarySpace allows the temporary storage used by the query
	// service backfill, per-pod, to be modified.  This field requires
	// `backfillEnabled` to be set to true in order to have any effect.
	// More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:default="5Gi"
	// +kubebuilder:validation:Type=string
	TemporarySpace *resource.Quantity `json:"temporarySpace,omitempty"`

	// TemporarySpaceUnlimited allows the temporary storage used by
	// the query service backfill, per-pod, to be unconstrained.  This field
	// requires `backfillEnabled` to be set to true in order to have any effect.
	// This field overrides `temporarySpace`.
	TemporarySpaceUnlimited bool `json:"temporarySpaceUnlimited,omitempty"`
}

// CouchbaseClusterDataSettings allows data service tweaks.
type CouchbaseClusterDataSettings struct {
	// ReaderThreads allows the number of threads used by the data service,
	// per pod, to be altered.  This value must be between 4 and 64 threads,
	// and should only be increased where there are sufficient CPU resources
	// allocated for their use.  If not specified, this defaults to the
	// default value set by Couchbase Server.
	// +kubebuilder:validation:Minimum=4
	// +kubebuilder:validation:Maximum=64
	ReaderThreads int `json:"readerThreads,omitempty"`

	// ReaderThreads allows the number of threads used by the data service,
	// per pod, to be altered.  This setting is especially relevant when
	// using "durable writes", increasing this field will have a large
	// impact on performance.  This value must be between 4 and 64 threads,
	// and should only be increased where there are sufficient CPU resources
	// allocated for their use. If not specified, this defaults to the
	// default value set by Couchbase Server.
	// +kubebuilder:validation:Minimum=4
	// +kubebuilder:validation:Maximum=64
	WriterThreads int `json:"writerThreads,omitempty"`
}

// DatabaseFragmentationThreshold lists triggers for when database compaction should start.
type DatabaseFragmentationThreshold struct {
	// Percent is the percentage of disk fragmentation after which to decompaction will be
	// triggered. This field must be in the range 2-100, defaulting to 30.
	// +kubebuilder:default=30
	// +kubebuilder:validation:Minimum=2
	// +kubebuilder:validation:Maximum=100
	Percent *int `json:"percent,omitempty"`

	// Size is the amount of disk framentation, that once exceeded, will trigger decompaction.
	// More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:validation:Type=string
	Size *resource.Quantity `json:"size,omitempty"`
}

// ViewFragmentationThreshold lists triggers for when view compaction should start.
type ViewFragmentationThreshold struct {
	// Percent is the percentage of disk fragmentation after which to decompaction will be
	// triggered. This field must be in the range 2-100, defaulting to 30.
	// +kubebuilder:default=30
	// +kubebuilder:validation:Minimum=2
	// +kubebuilder:validation:Maximum=100
	Percent *int `json:"percent,omitempty"`

	// Size is the amount of disk framentation, that once exceeded, will trigger decompaction.
	// More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:validation:Type=string
	Size *resource.Quantity `json:"size,omitempty"`
}

// TimeWindow allows the user to restrict when compaction can occur.
type TimeWindow struct {
	// Start is a wallclock time, in the form HH:MM, when a compaction is permitted to start.
	// +kubebuilder:validation:Pattern="^(2[0-3]|[01]?[0-9]):([0-5]?[0-9])$"
	Start string `json:"start,omitempty"`

	// End is a wallclock time, in the form HH:MM, when a compaction should stop.
	// +kubebuilder:validation:Pattern="^(2[0-3]|[01]?[0-9]):([0-5]?[0-9])$"
	End string `json:"end,omitempty"`

	// AbortCompactionOutsideWindow stops compaction processes when the
	// process moves outside the window.
	AbortCompactionOutsideWindow bool `json:"abortCompactionOutsideWindow,omitempty"`
}

// AutoCompaction define auto compaction settings.
type AutoCompaction struct {
	// DatabaseFragmentationThreshold defines triggers for when database compaction should start.
	// +optional
	// +kubebuilder:default="x-couchbase-object"
	DatabaseFragmentationThreshold DatabaseFragmentationThreshold `json:"databaseFragmentationThreshold,omitempty"`

	// ViewFragmentationThreshold defines triggers for when view compaction should start.
	// +optional
	// +kubebuilder:default="x-couchbase-object"
	ViewFragmentationThreshold ViewFragmentationThreshold `json:"viewFragmentationThreshold,omitempty"`

	// ParallelCompaction controls whether database and view compactions can happen
	// in parallel.
	ParallelCompaction bool `json:"parallelCompaction,omitempty"`

	// TimeWindow allows restriction of when compaction can occur.
	TimeWindow TimeWindow `json:"timeWindow,omitempty"`

	// TombstonePurgeInterval controls how long to wait before purging tombstones.
	// This field must be in the range 1h-1440h, defaulting to 72h.
	// More info:  https://golang.org/pkg/time/#ParseDuration
	// +kubebuilder:default="72h"
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
	// Secret references a secret containing the CA certificate (data key "ca"),
	// and optionally a client certificate (data key "certificate") and key
	// (data key "key").
	Secret *string `json:"secret"`
}

// RemoteCluster is a reference to a remote cluster for XDCR.
type RemoteCluster struct {
	// Name of the remote cluster.
	Name string `json:"name"`

	// UUID of the remote cluster.  The UUID of a CouchbaseCluster resource
	// is advertised in the status.clusterId field of the resource.
	// +kubebuilder:validation:Pattern="^[0-9a-f]{32}$"
	UUID string `json:"uuid"`

	// Hostname is the connection string to use to connect the remote cluster.
	// +kubebuilder:validation:Pattern="^((couchbase|couchbases|http|https)://)?[0-9a-zA-Z\\-\\.]+(:\\d+)?(\\?network=[^&]+)?$"
	Hostname string `json:"hostname"`

	// AuthenticationSecret is a secret used to authenticate when establishing a
	// remote connection.  It is only required when not using mTLS.  The secret
	// must contain a username (secret key "username") and password (secret key
	// "password").
	AuthenticationSecret *string `json:"authenticationSecret,omitempty"`

	// Replications are replication streams from this cluster to the remote one.
	// This field defines how to look up CouchbaseReplication resources.  By default
	// any CouchbaseReplication resources in the namespace will be considered.
	Replications Replications `json:"replications,omitempty"`

	// TLS if specified references a resource containing the necessary certificate
	// data for an encrypted connection.
	TLS *RemoteClusterTLS `json:"tls,omitempty"`
}

// XDCR allows management of XDCR settings.
type XDCR struct {
	// Managed defines whether XDCR is managed by the operator or not.
	Managed bool `json:"managed,omitempty"`

	// RemoteClusters is a set of named remote clusters to establish replications to.
	// +listType=map
	// +listMapKey=name
	RemoteClusters []RemoteCluster `json:"remoteClusters,omitempty"`
}

type Buckets struct {
	// Managed defines whether buckets are managed by the Operator (true), or user managed (false).
	// When Operator managed, all buckets must be defined with either CouchbaseBucket,
	// CouchbaseEphemeralBucket or CouchbaseMemcachedBucket resources.  Manual addition
	// of buckets will be reverted by the Operator.  When user managed, the Operator
	// will not interrogate buckets at all.  This field defaults to false.
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

// PodTemplate is a v1.PodTemplateSpec with a metadata that will be preserved.
type PodTemplate struct {
	ObjectMeta `json:"metadata,omitempty"`
	Spec       v1.PodSpec `json:"spec,omitempty"`
}

type ServerConfig struct {
	// Size is the expected requested of the server class.  This field
	// must be greater than or equal to 1.
	// +kubebuilder:validation:Minimum=1
	Size int `json:"size"`

	// Name is a textual name for the server configuration and must be unique.
	// The name is used by the operator to uniquely identify a server class,
	// and map pods back to an intended configuration.
	Name string `json:"name"`

	// Services is the set of Couchbase services to run on this server class.
	// At least one class must contain the data service.  The field may contain
	// any of "data", "index", "query", "search", "eventing" or "analytics".
	// Each service may only be specified once.
	// +listType=set
	Services []Service `json:"services"`

	// ServerGroups define the set of availability zones you want to distribute
	// pods over, and construct Couchbase server groups for.  By default, most
	// cloud providers will label nodes with the key "topology.kubernetes.io/zone",
	// the values associated with that key are used here to provide explicit
	// scheduling by the Operator.  You may manually label nodes using the
	// "topology.kubernetes.io/zone" key, to provide failure-domain
	// aware scheduling when none is provided for you.  Global server groups are
	// applied to all server classes, and may be overridden on a per-server class
	// basis to give more control over scheduling and server groups.
	// +listType=set
	ServerGroups []string `json:"serverGroups,omitempty"`

	// Pod defines a template used to create pod for each Couchbase server
	// instance.  Modifying pod metadata such as labels and annotations will
	// update the pod in-place.  Any other modification will result in a cluster
	// upgrade in order to fulfill the request. The Operator reserves the right
	// to modify or replace any field.  More info:
	// https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.21/#pod-v1-core
	Pod *PodTemplate `json:"pod,omitempty"`

	// VolumeMounts define persistent volume claims to attach to pod.
	VolumeMounts *VolumeMounts `json:"volumeMounts,omitempty"`

	// Resources are the resource requirements for the Couchbase server container.
	// This field overrides any automatic allocation as defined by
	// `spec.autoResourceAllocation`.
	Resources v1.ResourceRequirements `json:"resources,omitempty"`

	// Env allows the setting of environment variables in the Couchbase server container.
	Env []v1.EnvVar `json:"env,omitempty"`

	// EnvFrom allows the setting of environment variables in the Couchbase server container.
	EnvFrom []v1.EnvFromSource `json:"envFrom,omitempty"`

	// AutoscaledEnabled defines whether the autoscaling feature is enabled for this class.
	// When true, the Operator will create a CouchbaseAutoscaler resource for this
	// server class.  The CouchbaseAutoscaler implements the Kubernetes scale API and
	// can be controlled by the Kubernetes horizontal pod autoscaler (HPA).
	AutoscaleEnabled bool `json:"autoscaleEnabled,omitempty"`
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
	// DefaultClaim is a persistent volume that encompasses all Couchbase persistent
	// data, including document storage, indexes and logs.  The default volume can be
	// used with any server class.  Use of the default claim allows the Operator to
	// recover failed pods from the persistent volume far quicker than if the pod were
	// using ephemeral storage.  The default claim cannot be used at the same time
	// as the logs claim within the same server class.  This field references a volume
	// claim template name as defined in "spec.volumeClaimTemplates".
	DefaultClaim string `json:"default,omitempty"`

	// DataClaim is a persistent volume that encompasses key/value storage associated
	// with the data service.  The data claim can only be used on server classes running
	// the data service, and must be used in conjunction with the default claim.  This
	// field allows the data service to use different storage media (e.g. SSD) to
	// improve performance of this service.  This field references a volume
	// claim template name as defined in "spec.volumeClaimTemplates".
	DataClaim string `json:"data,omitempty"`

	// IndexClaim s a persistent volume that encompasses index storage associated
	// with the index and search services.  The index claim can only be used on server classes running
	// the index or search services, and must be used in conjunction with the default claim.  This
	// field allows the index and/or search service to use different storage media (e.g. SSD) to
	// improve performance of this service. This field references a volume
	// claim template name as defined in "spec.volumeClaimTemplates".
	// Whilst this references index primarily, note that the full text search (FTS) service
	// also uses this same mount.
	IndexClaim string `json:"index,omitempty"`

	// AnalyticsClaims are persistent volumes that encompass analytics storage associated
	// with the analytics service.  Analytics claims can only be used on server classes
	// running the analytics service, and must be used in conjunction with the default claim.
	// This field allows the analytics service to use different storage media (e.g. SSD), and
	// scale horizontally, to improve performance of this service.  This field references a volume
	// claim template name as defined in "spec.volumeClaimTemplates".
	AnalyticsClaims []string `json:"analytics,omitempty"`

	// LogsClaim is a persistent volume that encompasses only Couchbase server logs to aid
	// with supporting the product.  The logs claim can only be used on server classes running
	// the following services: query, search & eventing.  The logs claim cannot be used at the same
	// time as the default claim within the same server class.  This field references a volume
	// claim template name as defined in "spec.volumeClaimTemplates".
	// Whilst the logs claim can be used with the search service, the recommendation is to use the
	// default claim for these. The reason for this is that a failure of these nodes will require
	// indexes to be rebuilt and subsequent performance impact.
	LogsClaim string `json:"logs,omitempty"`
}

// NodeToNodeEncryptionType is used to define the level of node-to-node encryption.
// +kubebuilder:validation:Enum=ControlPlaneOnly;All;Strict
type NodeToNodeEncryptionType string

const (
	// NodeToNodeControlPlaneOnly is faster but at the expense of exposing your data.
	NodeToNodeControlPlaneOnly NodeToNodeEncryptionType = "ControlPlaneOnly"

	// NodeToNodeAll all traffic should be over TLS.
	NodeToNodeAll NodeToNodeEncryptionType = "All"

	// NodeToNodeStrict all traffic is over TLS and non-TLS ports are shut down.
	NodeToNodeStrict NodeToNodeEncryptionType = "Strict"
)

// TLSVersion defines the minimum TLS version to use.
// +kubebuilder:validation:Enum=TLS1.0;TLS1.1;TLS1.2;TLS1.3
type TLSVersion string

const (
	// Insecure, don't use.
	TLS10 TLSVersion = "TLS1.0"

	// Insecure, don't use.
	TLS11 TLSVersion = "TLS1.1"

	// Obsolete, don't use.
	TLS12 TLSVersion = "TLS1.2"

	// Latest and greatest!
	TLS13 TLSVersion = "TLS1.3"
)

// TLSPolicy defines the TLS policy of an Couchbase cluster
type TLSPolicy struct {
	// Static enables user to generate static x509 certificates and keys,
	// put them into Kubernetes secrets, and specify them here.  Static secrets
	// are very Couchbase specific.
	Static *StaticTLS `json:"static,omitempty"`

	// SecretSource enables the user to specify a secret conforming to the Kubernetes TLS
	// secret specification.
	SecretSource *TLSSecretSource `json:"secretSource,omitempty"`

	// ClientCertificatePolicy defines the client authentication policy to use.
	// If set, the Operator expects TLS configuration to contain a valid certificate/key pair
	// for the Administrator account.
	ClientCertificatePolicy *ClientCertificatePolicy `json:"clientCertificatePolicy,omitempty"`

	// ClientCertificatePaths defines where to look in client certificates in order
	// to extract the user name.
	ClientCertificatePaths []ClientCertificatePath `json:"clientCertificatePaths,omitempty"`

	// NodeToNodeEncryption specifies whether to encrypt data between Couchbase nodes
	// within the same cluster.  This may come at the expense of performance.  When
	// control plane only encryption is used, only cluster management traffic is encrypted
	// between nodes.  When all, all traffic is encrypted, including database documents.
	// When strict mode is used, it is the same as all, but also disables all plaintext
	// ports.  Strict mode is only available on Couchbase Server versions 7.1 and greater.
	// This field must be either "ControlPlaneOnly", "All", or "Strict".
	NodeToNodeEncryption *NodeToNodeEncryptionType `json:"nodeToNodeEncryption,omitempty"`

	// TLSMinimumVersion specifies the minimum TLS version the Couchbase server can
	// negotiate with a client.  Must be one of TLS1.0, TLS1.1 TLS1.2 or TLS1.3,
	// defaulting to TLS1.2.  TLS1.3 is only valid for Couchbase Server 7.1.0 onward.
	// +kubebuilder:default=TLS1.2
	TLSMinimumVersion TLSVersion `json:"tlsMinimumVersion,omitempty"`

	// CipherSuites specifies a list of cipher suites for Couchbase server to select
	// from when negotiating TLS handshakes with a client.  Suites are not validated
	// by the Operator.  Run "openssl ciphers -v" in a Couchbase server pod to
	// interrogate supported values.
	// +listType=set
	CipherSuites []string `json:"cipherSuites,omitempty"`
}

type StaticTLS struct {
	// ServerSecret is a secret name containing TLS certs used by each Couchbase member pod
	// for the communication between Couchbase server and its clients.  The secret must
	// contain a certificate chain (data key "couchbase-operator.crt") and a private
	// key (data key "couchbase-operator.key").  The private key must be in the PKCS#1 RSA
	// format.  The certificate chain must have a required set of X.509v3 subject alternative
	// names for all cluster addressing modes.  See the Operator TLS documentation for more
	// information.
	ServerSecret string `json:"serverSecret,omitempty"`

	// OperatorSecret is a secret name containing TLS certs used by operator to
	// talk securely to this cluster.  The secret must contain a CA certificate (data key
	// ca.crt).  If client authentication is enabled, then the secret must also contain
	// a client certificate chain (data key "couchbase-operator.crt") and private key
	// (data key "couchbase-operator.key").
	OperatorSecret string `json:"operatorSecret,omitempty"`
}

type TLSSecretSource struct {
	// ServerSecretName specifies the secret name, in the same namespace as the cluster,
	// that contains server TLS data.  The secret is expected to contain "tls.crt" and
	// "tls.key" as per the kubernetes.io/tls secret type.  It also additionally
	// must contain "ca.crt".
	ServerSecretName string `json:"serverSecretName"`

	// ClientSecretName specifies the secret name, in the same namespace as the cluster,
	// the contains client TLS data.  The secret is expected to contain "tls.crt" and
	// "tls.key" as per the Kubernetes.io/tls secret type.
	ClientSecretName string `json:"clientSecretName,omitempty"`
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
	// This field must be either "subject.cn", "san.uri", "san.dnsname" or  "san.email".
	// +kubebuilder:validation:Pattern="^subject\\.cn|san\\.uri|san\\.dnsname|san\\.email$"
	Path string `json:"path"`

	// Prefix allows a prefix to be stripped from the username, once extracted from the
	// certificate path.
	Prefix string `json:"prefix,omitempty"`

	// Delimiter if specified allows a suffix to be stripped from the username, once
	// extracted from the certificate path.
	Delimiter string `json:"delimiter,omitempty"`
}

type ClusterCondition struct {
	// Type is the type of condition.
	Type ClusterConditionType `json:"type"`

	// Status is the status of the condition. Can be one of True, False, Unknown.
	Status v1.ConditionStatus `json:"status"`

	// Last time the condition status message updated.
	LastUpdateTime string `json:"lastUpdateTime,omitempty"`

	// Last time the condition transitioned from one status to another.
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`

	// Unique, one-word, CamelCase reason for the condition's last transition.
	Reason string `json:"reason,omitempty"`

	// A human readable message indicating details about the transition.
	Message string `json:"message,omitempty"`
}

// +kubebuilder:validation:Enum=Available;Balanced;ManageConfig;Scaling;ScalingUp;ScalingDown;Upgrading;Hibernating;Error;AutoscaleReady;
type ClusterConditionType string

const (
	ClusterConditionAvailable      ClusterConditionType = "Available"
	ClusterConditionBalanced       ClusterConditionType = "Balanced"
	ClusterConditionManageConfig   ClusterConditionType = "ManageConfig"
	ClusterConditionScaling        ClusterConditionType = "Scaling"
	ClusterConditionScalingUp      ClusterConditionType = "ScalingUp"
	ClusterConditionScalingDown    ClusterConditionType = "ScalingDown"
	ClusterConditionUpgrading      ClusterConditionType = "Upgrading"
	ClusterConditionHibernating    ClusterConditionType = "Hibernating"
	ClusterConditionError          ClusterConditionType = "Error"
	ClusterConditionAutoscaleReady ClusterConditionType = "AutoscaleReady"
)

// ClusterStatus defines any read-only status fields for the Couchbase server cluster.
type ClusterStatus struct {
	// ControlPaused indicates if the Operator has acknowledged and paused the
	// control of the cluster.
	ControlPaused bool `json:"controlPaused,omitempty"`

	// Current service state of the Couchbase cluster.
	Conditions []ClusterCondition `json:"conditions,omitempty"`

	// ClusterID is the unique cluster UUID.  This is generated every time
	// a new cluster is created, so may vary over the lifetime of a cluster
	// if it is recreated by disaster recovery mechanisms.
	ClusterID string `json:"clusterId,omitempty"`

	// Size is the current size of the cluster in terms of pods.  Individual
	// pod status conditions are listed in the members status.
	Size int `json:"size"`

	// Members are the Couchbase members in the cluster.
	Members *MembersStatus `json:"members,omitempty"`

	// CurrentVersion is the current Couchbase version.  This reflects the
	// version of the whole cluster, therefore during upgrade, it is only
	// updated when the upgrade has completed.
	CurrentVersion string `json:"currentVersion,omitempty"`

	// Allocations shows memory allocations within server classes.
	Allocations []ServerClassStatus `json:"allocations,omitempty"`

	// Buckets describes all the buckets managed by the cluster.
	Buckets []BucketStatus `json:"buckets,omitempty"`

	// Users describes all the users managed by the cluster.
	Users []string `json:"users,omitempty"`

	// Groups describes all the groups managed by the cluster.
	Groups []string `json:"groups,omitempty"`

	// Autscalers describes all the autoscalers managed by the cluster.
	Autoscalers []string `json:"autoscalers,omitempty"`
}

// ServerClassStatus summarizes memory allocations to make configuration easier.
type ServerClassStatus struct {
	// Name is the name of the server class defined in spec.servers
	Name string `json:"name"`

	// RequestedMemory, if set, defines the Kubernetes resource request for the server class.
	// More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:validation:Type=string
	RequestedMemory *resource.Quantity `json:"requestedMemory,omitempty"`

	// AllocatedMemory defines the total memory allocated for constrained Couchbase services.
	// More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:validation:Type=string
	AllocatedMemory *resource.Quantity `json:"allocatedMemory,omitempty"`

	// AllocatedMemoryPercent is set when memory resources are requested and define how much of
	// the requested memory is allocated to constrained Couchbase services.
	AllocatedMemoryPercent int `json:"allocatedMemoryPercent,omitempty"`

	// UnusedMemory is set when memory resources are requested and is the difference between
	// the requestedMemory and allocatedMemory.  More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:validation:Type=string
	UnusedMemory *resource.Quantity `json:"unusedMemory,omitempty"`

	// UnusedMemoryPercent is set when memory resources are requested and defines how much
	// requested memory is not allocated.  Couchbase server expects at least a 20% overhead.
	UnusedMemoryPercent int `json:"unusedMemoryPercent,omitempty"`

	// DataServiceAllocation is set when the data service is enabled for this class and
	// defines how much memory this service consumes per pod.  More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:validation:Type=string
	DataServiceAllocation *resource.Quantity `json:"dataServiceAllocation,omitempty"`

	// IndexServiceAllocation is set when the index service is enabled for this class and
	// defines how much memory this service consumes per pod.  More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:validation:Type=string
	IndexServiceAllocation *resource.Quantity `json:"indexServiceAllocation,omitempty"`

	// SearchServiceAllocation is set when the search service is enabled for this class and
	// defines how much memory this service consumes per pod.  More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:validation:Type=string
	SearchServiceAllocation *resource.Quantity `json:"searchServiceAllocation,omitempty"`

	// EventingServiceAllocation is set when the eventing service is enabled for this class and
	// defines how much memory this service consumes per pod.  More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:validation:Type=string
	EventingServiceAllocation *resource.Quantity `json:"eventingServiceAllocation,omitempty"`

	// AnalyticsServiceAllocation is set when the analytics service is enabled for this class and
	// defines how much memory this service consumes per pod.  More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:validation:Type=string
	AnalyticsServiceAllocation *resource.Quantity `json:"analyticsServiceAllocation,omitempty"`
}

type BucketStatus struct {
	// BucketName is the full name of the bucket.
	BucketName string `json:"name"`

	// BucketType is the type of the bucket.
	BucketType string `json:"type"`

	// BucketMemoryQuota is the bucket memory quota in megabytes.
	BucketMemoryQuota int64 `json:"memoryQuota"`

	// BucketReplicas is the number of data replicas.
	BucketReplicas int `json:"replicas"`

	// IoPriority is `low` or `high` depending on the number of threads
	// spawned for data processing.
	IoPriority string `json:"ioPriority"`

	// EvictionPolicy is relevant for `couchbase` and `ephemeral` bucket types
	// and indicates how documents are evicted from memory when it is exhausted.
	EvictionPolicy string `json:"evictionPolicy"`

	// ConflictResolution is relevant for `couchbase` and `ephemeral` bucket types
	// and indicates how to resolve conflicts when using multi-master XDCR.
	ConflictResolution string `json:"conflictResolution"`

	// EnableFlush is whether a client can delete all documents in a bucket.
	EnableFlush bool `json:"enableFlush"`

	// EnableIndexReplica is whether indexes against bucket documents are replicated.
	EnableIndexReplica bool `json:"enableIndexReplica"`

	// BucketPassword will never be populated.
	BucketPassword string `json:"password"`

	// CompressionMode defines how documents are compressed.
	CompressionMode string `json:"compressionMode"`
}

type MemberStatusList []string

type MembersStatus struct {
	// Ready are the Couchbase members that are clustered and ready to serve
	// client requests.  The member names are the same as the Couchbase pod names.
	Ready MemberStatusList `json:"ready,omitempty"`

	// Unready are the Couchbase members not clustered or unready to serve
	// client requests.  The member names are the same as the Couchbase pod names.
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
	// Prometheus provides integration with Prometheus monitoring.
	Prometheus *CouchbaseClusterMonitoringPrometheusSpec `json:"prometheus,omitempty"`
}

type CouchbaseClusterMonitoringPrometheusSpec struct {
	// Enabled is a boolean that enables/disables the metrics sidecar container.
	Enabled bool `json:"enabled,omitempty"`

	// Image is the metrics image to be used to collect metrics.
	// No validation is carried out as this can be any arbitrary repo and tag.
	Image string `json:"image"`

	// Resources is the resource requirements for the metrics container.
	// Will be populated by Kubernetes defaults if not specified.
	Resources *v1.ResourceRequirements `json:"resources,omitempty"`

	// AuthorizationSecret is the name of a Kubernetes secret that contains a
	// bearer token to authorize GET requests to the metrics endpoint
	AuthorizationSecret *string `json:"authorizationSecret,omitempty"`
}

type CouchbaseClusterLoggingConfigurationSpec struct {
	// Enabled is a boolean that enables the logging sidecar container.
	Enabled bool `json:"enabled,omitempty"`

	// A boolean which indicates whether the operator should manage the configuration or not.
	// If omitted then this defaults to true which means the operator will attempt to reconcile it to default values.
	// To use a custom configuration make sure to set this to false.
	// Note that the ownership of any Secret is not changed so if a Secret is created externally it can be updated by
	// the operator but it's ownership stays the same so it will be cleaned up when it's owner is.
	// +kubebuilder:default=true
	ManageConfiguration *bool `json:"manageConfiguration,omitempty"`

	// ConfigurationName is the name of the Secret to use holding the logging configuration in the namespace.
	// A Secret is used to ensure we can safely store credentials but this can be populated from plaintext if acceptable too.
	// If it does not exist then one will be created with defaults in the namespace so it can be easily updated whilst running.
	// Note that if running multiple clusters in the same kubernetes namespace then you should use a separate Secret for each,
	// otherwise the first cluster will take ownership (if created) and the Secret will be cleaned up when that cluster is
	// removed. If running clusters in separate namespaces then they will be separate Secrets anyway.
	// +kubebuilder:default="fluent-bit-config"
	ConfigurationName string `json:"configurationName,omitempty"`

	// Any specific logging sidecar container configuration.
	// +kubebuilder:default="x-couchbase-object"
	Sidecar *LogShipperSidecarSpec `json:"sidecar,omitempty"`
}

type LogShipperSidecarSpec struct {
	// Image is the image to be used to deal with logging as a sidecar.
	// No validation is carried out as this can be any arbitrary repo and tag.
	// It will default to the latest supported version of Fluent Bit.
	// +kubebuilder:default="couchbase/fluent-bit:1.1.1"
	Image string `json:"image,omitempty"`

	// ConfigurationMountPath is the location to mount the ConfigurationName Secret into the image.
	// If another log shipping image is used that needs a different mount then modify this.
	// Note that the configuration file must be called 'fluent-bit.conf' at the root of this path,
	// there is no provision for overriding the name of the config file passed as the
	// COUCHBASE_LOGS_CONFIG_FILE environment variable.
	// +kubebuilder:default="/fluent-bit/config/"
	ConfigurationMountPath string `json:"configurationMountPath,omitempty"`

	// Resources is the resource requirements for the sidecar container.
	// Will be populated by Kubernetes defaults if not specified.
	Resources *v1.ResourceRequirements `json:"resources,omitempty"`
}

// The AuditDisabledUser is actually a compound string intended to feed a two-element struct.
// Its value may be:
// 1. A local user, specified in the form localusername/local.
// 2. An external user, specified in the form externalusername/external.
// 3. An internal user, specified in the form @internalusername/local.
// We add a quick validation check to make sure these match and prevent being rejected by the API later.
// This is just a sanity check, the REST API may still reject the user for other reasons.
// +kubebuilder:validation:Pattern="^.+/(local|external)$"
type AuditDisabledUser string

// The CouchbaseClusterAuditLoggingSpec structure is primarily based on the needs of the Audit REST API.
// An overview of auditing can be found here: https://docs.couchbase.com/server/current/learn/security/auditing.html
// More details on the REST API can be found here: https://docs.couchbase.com/server/current/rest-api/rest-auditing.html
type CouchbaseClusterAuditLoggingSpec struct {
	// Enabled is a boolean that enables the audit capabilities.
	Enabled bool `json:"enabled,omitempty"`

	// The list of event ids to disable for auditing purposes.
	// This is passed to the REST API with no verification by the operator.
	// Refer to the documentation for details:
	// https://docs.couchbase.com/server/current/audit-event-reference/audit-event-reference.html
	DisabledEvents []int `json:"disabledEvents,omitempty"`

	// The list of users to ignore for auditing purposes.
	// This is passed to the REST API with minimal validation it meets an acceptable regex pattern.
	// Refer to the documentation for full details on how to configure this:
	// https://docs.couchbase.com/server/current/manage/manage-security/manage-auditing.html#ignoring-events-by-user
	DisabledUsers []AuditDisabledUser `json:"disabledUsers,omitempty"`

	// The interval to optionally rotate the audit log.
	// This is passed to the REST API, see here for details:
	// https://docs.couchbase.com/server/current/manage/manage-security/manage-auditing.html
	Rotation *CouchbaseClusterLogRotationSpec `json:"rotation,omitempty"`

	// Handle all optional garbage collection (GC) configuration for the audit functionality.
	// This is not part of the audit REST API, it is intended to handle GC automatically for the audit logs.
	// By default the Couchbase Server rotates the audit logs but does not clean up the rotated logs.
	// This is left as an operation for the cluster administrator to manage, the operator allows for us to automate this:
	// https://docs.couchbase.com/server/current/manage/manage-security/manage-auditing.html
	GarbageCollection *CouchbaseClusterAuditGarbageCollectionSpec `json:"garbageCollection,omitempty"`
}

type CouchbaseClusterLogRotationSpec struct {
	// The interval at which to rotate log files, defaults to 15 minutes.
	// +kubebuilder:default="15m"
	Interval *metav1.Duration `json:"interval,omitempty"`

	// Size allows the specification of a rotation size for the log, defaults to 20Mi.
	// More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:default="20Mi"
	// +kubebuilder:validation:Type=string
	Size *resource.Quantity `json:"size,omitempty"`
}

type CouchbaseClusterAuditGarbageCollectionSpec struct {
	// Provide the sidecar configuration required (if so desired) to automatically clean up audit logs.
	Sidecar *CouchbaseClusterAuditCleanupSidecarSpec `json:"sidecar,omitempty"`
}

type CouchbaseClusterAuditCleanupSidecarSpec struct {
	// Enable this sidecar by setting to true, defaults to being disabled.
	Enabled bool `json:"enabled,omitempty"`

	// Image is the image to be used to run the audit sidecar helper.
	// No validation is carried out as this can be any arbitrary repo and tag.
	// +kubebuilder:default="busybox:1.33.1"
	Image string `json:"image,omitempty"`

	// The minimum age of rotated log files to remove, defaults to one hour.
	// +kubebuilder:default="1h"
	Age *metav1.Duration `json:"age,omitempty"`

	// The interval at which to check for rotated log files to remove, defaults to 20 minutes.
	// +kubebuilder:default="20m"
	Interval *metav1.Duration `json:"interval,omitempty"`

	// Resources is the resource requirements for the cleanup container.
	// Will be populated by Kubernetes defaults if not specified.
	Resources *v1.ResourceRequirements `json:"resources,omitempty"`
}
