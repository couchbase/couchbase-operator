//nolint:godot
package v2

import (
	"encoding/json"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// BucketName is the name of a bucket.
// +kubebuilder:validation:MaxLength=100
// +kubebuilder:validation:Pattern="^[a-zA-Z0-9-_%\\.]{1,100}$"
type BucketName string

// CouchbaseStorageBackend can either be "couchstore" or "magma".
// Defaults to magma for server versions 8.0.0+. Defaults to
// couchstore for server versions earlier than 8.0.0.
// +kubebuilder:validation:Enum=couchstore;magma
type CouchbaseStorageBackend string

const (
	CouchbaseStorageBackendCouchstore CouchbaseStorageBackend = "couchstore"
	CouchbaseStorageBackendMagma      CouchbaseStorageBackend = "magma"
)

// DurabilityImpossibleFallback can either be "disabled" or "fallbackToActiveAck".
// This setting is only available for server versions 8.0.0+.
// +kubebuilder:validation:Enum=disabled;fallbackToActiveAck
type DurabilityImpossibleFallback string

const (
	DurabilityImpossibleFallbackDisabled DurabilityImpossibleFallback = "disabled"
	DurabilityImpossibleFallbackActive   DurabilityImpossibleFallback = "fallbackToActiveAck"
)

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
// +kubebuilder:validation:Pattern="^s3://[a-z0-9-\\.\\/]{3,63}$"
type S3BucketURI string

// ObjectStoreURI is a reference to a remote object store.
// This is the prefix of the object store and the bucket name.
// i.e s3://bucket or az://bucket.
// +kubebuilder:validation:Pattern="^(az|s3|gs)://.{3,}$"
type ObjectStoreURI string

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

// ObjectStore allows for backing up to a remote cloud storage.
type ObjectStoreSpec struct {
	// Whether to allow the backup SDK to attempt to authenticate
	// using the instance metadata api.
	// If set, will override `CouchbaseCluster.spec.backup.useIAM`.
	UseIAM *bool `json:"useIAM,omitempty"`
	// URI is a reference to a remote object store.
	// This is the prefix of the object store and the bucket name.
	// i.e s3://bucket, az://bucket or gs://bucket.
	URI ObjectStoreURI `json:"uri,omitempty"`

	// ObjStoreSecret must contain two fields, access-key-id, secret-access-key and optionally either region or refresh-token.
	// These correspond to the fields used by cbbackupmgr
	// https://docs.couchbase.com/server/current/backup-restore/cbbackupmgr-backup.html#optional-2
	Secret string `json:"secret,omitempty"`

	// Endpoint contains the configuration for connecting to a custom Azure/S3/GCP compliant object store.
	// If set will override `CouchbaseCluster.spec.backup.objectEndpoint`
	// See https://docs.couchbase.com/server/current/backup-restore/cbbackupmgr-cloud.html#compatible-object-stores
	Endpoint *ObjectEndpoint `json:"endpoint,omitempty"`
}

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
	// respectively. `periodic_merge` will first create an immediate full backup, after that,
	// it will only take incremental backups and then merge them. You must define incremental and merge
	// schedules in the `spec.incremental` and `spec.merge` fields respectively. The initial full backup
	// will be retried based on spec.backoffLimit, if it reaches this limit without success, then the
	// incremental, merge cycles won't run. Care should be taken to ensure full, incremental and merge
	// schedules do not overlap, taking into account the backup time, as this will cause failures as the
	// jobs attempt to mount the same backup volume. To cause a backup to occur immediately use
	// `immediate_incremental` or `immediate_full` for incremental or full backups respectively.
	// respectively. This field default to `full_incremental`.
	// Info: https://docs.couchbase.com/server/current/backup-restore/cbbackupmgr-strategies.html
	// +kubebuilder:default="full_incremental"
	Strategy Strategy `json:"strategy,omitempty"`

	// Incremental is the schedule on when to take incremental backups.
	// Used in Full/Incremental backup strategies.
	Incremental *CouchbaseBackupSchedule `json:"incremental,omitempty"`

	// Full is the schedule on when to take full backups.
	// Used in Full/Incremental and FullOnly backup strategies.
	Full *CouchbaseBackupSchedule `json:"full,omitempty"`

	// Merge is the schedule on when to merge incremental backups.
	// Used in PeriodicMerge backup strategy.
	Merge *CouchbaseBackupSchedule `json:"merge,omitempty"`

	// Amount of time to elapse before a completed job is deleted.
	// +kubebuilder:validation:Minimum=0
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`

	// Amount of successful jobs to keep.
	// +kubebuilder:default=3
	// +kubebuilder:validation:Minimum=0
	SuccessfulJobsHistoryLimit int32 `json:"successfulJobsHistoryLimit,omitempty"`

	// Amount of failed jobs to keep.
	// +kubebuilder:default=3
	// +kubebuilder:validation:Minimum=0
	FailedJobsHistoryLimit int32 `json:"failedJobsHistoryLimit,omitempty"`

	// Number of times a backup job should try to execute. The time between each
	// attempt increases exponentially starting at 10s doubling each time and caps
	// out at 6 minutes.
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
	// +kubebuilder:validation:Type=string
	// +kubebuilder:default="20Gi"
	Size *resource.Quantity `json:"size,omitempty"`

	// AutoScaling allows the volume size to be dynamically increased.
	// When specified, the backup volume will start with an initial size
	// as defined by `spec.size`, and increase as required.
	AutoScaling *CouchbaseBackupAutoScaling `json:"autoScaling,omitempty"`

	// Name of StorageClass to use.
	StorageClassName *string `json:"storageClassName,omitempty"`

	// DEPRECATED - by spec.objectStore.uri
	// Name of S3 bucket to backup to. If non-empty this overrides local backup.
	S3Bucket S3BucketURI `json:"s3bucket,omitempty"`

	// ObjectStore allows for backing up to a remote cloud storage.
	ObjectStore *ObjectStoreSpec `json:"objectStore,omitempty"`

	// How many threads to use during the backup.  This field defaults to 1.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=0
	Threads int `json:"threads,omitempty"`

	// Services allows control over what services are included in the backup.
	// By default, all service data and metadata are included apart from users.
	// Modifications to this field will only take effect on the next full backup.
	// +kubebuilder:default="x-couchbase-object"
	Services CouchbaseBackupServiceFilter `json:"services,omitempty"`

	// Data allows control over what key-value/document data is included in the
	// backup.  By default, all data is included.  Modifications
	// to this field will only take effect on the next full backup.
	Data *CouchbaseBackupDataFilter `json:"data,omitempty"`

	// EphemeralVolume sets backup to use an ephemeral volume instead
	// of a persistent volume. This is used when backing up to a remote
	// cloud provider, where a persistent volume is not needed.
	// +kubebuilder:default=false
	EphemeralVolume bool `json:"ephemeralVolume,omitempty"`

	// DefaultRecoveryMethod specifies how cbbackupmgr should
	// recover from broken backup/restore attempts.
	// +kubebuilder:default="none"
	DefaultRecoveryMethod DefaultRecoveryType `json:"defaultRecoveryMethod,omitempty"`

	// ForceDeleteLockFile is used to force delete the lock file.
	// This is used to force delete the lock file when the backup is deleted.
	// +kubebuilder:default=false
	ForceDeleteLockfile bool `json:"-" annotation:"forceDeleteLockfile"`

	// AdditionalArgs is used to pass additional arguments to the backup script container.
	AdditionalArgs string `json:"-" annotation:"additionalArgs"`

	// Env defines environment variables to be set on the backup container.
	// These can be used to configure cbbackupmgr behavior via environment variables.
	// +optional
	Env []v1.EnvVar `json:"env,omitempty"`
}

// +kubebuilder:validation:Enum=none;resume;purge
type DefaultRecoveryType string

const (
	DefaultRecoveryTypeNone   DefaultRecoveryType = "none"
	DefaultRecoveryTypePurge  DefaultRecoveryType = "purge"
	DefaultRecoveryTypeResume DefaultRecoveryType = "resume"
)

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

	// Users enables the backup of users including their roles and permissions. This is
	// only available for Couchbase Server 7.6 and later. This field defaults to `false`.
	// +kubebuilder:default=false
	Users *bool `json:"users,omitempty"`
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
	// Changes from this field will only takes effect on a full backup.
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
	// Changes from this field will only takes effect on a full backup.
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

// +kubebuilder:validation:Enum=full_incremental;full_only;immediate_incremental;immediate_full;periodic_merge
type Strategy string

const (
	// Similar to Periodic but we create a new backup repository and take a full backup instead of merging.
	FullIncremental Strategy = "full_incremental"

	// Expensive Full Backup only recommended for small clusters.
	FullOnly Strategy = "full_only"

	// Full only but now
	ImmediateFull Strategy = "immediate_full"

	// Incremental but now
	ImmediateIncremental Strategy = "immediate_incremental"

	// Periodic merge strategy
	PeriodicMerge Strategy = "periodic_merge"
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
	Backup string `json:"backup,omitempty"`

	// Repo is the backup folder to restore from.  If no repository is specified,
	// the backup container will choose the latest.
	Repo string `json:"repo,omitempty"`

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

	// DEPRECATED - by spec.objectStore.uri
	// Name of S3 bucket to restore from. If non-empty this overrides local backup.
	S3Bucket S3BucketURI `json:"s3bucket,omitempty"`

	// The remote destination for backup.
	ObjectStore *ObjectStoreSpec `json:"objectStore,omitempty"`

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

	// Forces data in the Couchbase cluster to be overwritten even if the data in the cluster is newer.
	// By default, the system does not force updates,
	// and all updates use Couchbase's conflict resolution mechanism to ensure
	// that if newer data exists on the cluster,
	// older restored data does not overwrite it.
	// However, if `couchbasebackuprestores.spec.forceUpdates` is true,
	// then the backup record will _always_ overwrite the cluster record,
	// regardless of Couchbase's conflict resolution.
	ForceUpdates bool `json:"forceUpdates,omitempty"`

	// Overwrites the already existing users in the cluster when  user restoration is enabled (spec.services.users).
	// The default behavior of backup/restore of users is to skip already existing users.
	// This is only available for Couchbase Server 7.6 and later.
	// This field defaults to `false`.
	// +kubebuilder:default=false
	OverwriteUsers bool `json:"overwriteUsers,omitempty"`

	// StagingVolume contains configuration related to the
	// ephemeral volume used as staging when restoring from a cloud backup.
	// +kubebuilder:default={size: "20Gi"}
	StagingVolume *CouchbaseBackupStagingVolume `json:"stagingVolume,omitempty"`

	// Number of seconds to elapse before a completed job is deleted.
	// +kubebuilder:validation:Minimum=0
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`

	// PreserveRestoreRecord indicates whether the restore record should be preserved
	// after the restore job has completed.
	// +kubebuilder:default=false
	PreserveRestoreRecord bool `json:"preserveRestoreRecord,omitempty"`

	// AdditionalArgs is used to pass additional arguments to the backup script container.
	AdditionalArgs string `json:"-" annotation:"additionalArgs"`

	// Env defines environment variables to be set on the restore container.
	// These can be used to configure cbbackupmgr behavior via environment variables.
	// +optional
	Env []v1.EnvVar `json:"env,omitempty"`
}

type CouchbaseBackupStagingVolume struct {
	// Size allows the specification of a staging volume. More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// The ephemeral volume will only be used when restoring from a cloud provider,
	// if the backup job was created using ephemeral storage.
	// Otherwise the restore job will share a staging volume with the backup job.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:default="20Gi"
	Size *resource.Quantity `json:"size,omitempty"`

	// Name of StorageClass to use.
	StorageClassName *string `json:"storageClassName,omitempty"`
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

	// Users restores cluster level users, including their roles and permissions. This is
	// only available for Couchbase Server 7.6 and later. This field defaults to `false`.
	// +kubebuilder:default=false
	Users *bool `json:"users,omitempty"`
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

	// Completed indicates whether the restore has been successfully completed.
	Completed bool `json:"completed"`

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

// ScopeOrCollectionNameList is the type for a list of scope or collection names
// to provide helper functions.
type ScopeOrCollectionNameList []ScopeOrCollectionName

// DefaultScopeOrCollection is the name of the default scope and collection.
const DefaultScopeOrCollection = "_default"

// SystemScope is the name of the default scope and collection.
const SystemScope = "_system"

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
	// History defines whether change history is retained for the collection.
	// If this field is set, it will override the historyRetention.collectionDefault bucket level value.
	// This is only supported with storageBackend=magma at the bucket level.
	History *bool `json:"history,omitempty" annotation:"history"`

	MaxTTLWithNegativeOverride `json:",inline"`
}

// MaxTTLWithNegativeOverride acts as a field for maxTTL values that can either be set as a duration or "-1", which will set the duration to -1 seconds
type MaxTTLWithNegativeOverride struct {
	// MaxTTL defines how long a document is permitted to exist for, without
	// modification, until it is automatically deleted.  This field takes precedence over
	// any TTL defined at the bucket level.  This is a default, and maximum
	// time-to-live and may be set to a lower value by the client.  If the client specifies
	// a higher value, then it is truncated to the maximum durability.  Documents are
	// removed by Couchbase, after they have expired, when either accessed, the expiry
	// pager is run, or the bucket is compacted.  When set to 0, then documents are not
	// expired by default.  This field must either be a duration in the range 0-2147483648s or "-1",
	// defaulting to 0. If set to "-1", the collection's bucket will be prevented from setting a
	// default expiration on the collection's documents. While this field can be changed on the CRD,
	// it will not be updated on the collection if the Couchbase Server version is pre 7.6.0.
	// More info: https://golang.org/pkg/time/#ParseDuration.
	MaxTTL *metav1.Duration `json:"maxTTL,omitempty"`
}

func (d *MaxTTLWithNegativeOverride) UnmarshalMaxTTLWithNegativeOverride(data []byte) error {
	var jsonData map[string]interface{}
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return err
	}

	maxTTL, exists := jsonData["maxTTL"]
	if !exists {
		d.MaxTTL = &metav1.Duration{Duration: 0}
		return nil
	}

	val, _ := maxTTL.(string)

	if val == "-1" {
		d.MaxTTL = &metav1.Duration{Duration: -1 * time.Second}
		return nil
	}

	duration, err := time.ParseDuration(val)
	if err != nil {
		return err
	}

	d.MaxTTL = &metav1.Duration{Duration: duration}

	return nil
}

type CouchbaseCollectionSpec struct {
	CouchbaseCollectionSpecCommon `json:",inline" annotation:",inline"`

	// Name specifies the name of the collection.  By default, the metadata.name is
	// used to define the collection name, however, due to the limited character set,
	// this field can be used to override the default and provide the full functionality.
	// Additionally the `metadata.name` field is a DNS label, and thus limited to 63
	// characters, this field must be used if the name is longer than this limit.
	// Collection names must be 1-251 characters in length, contain only [a-zA-Z0-9_-%]
	// and not start with either _ or %.
	Name ScopeOrCollectionName `json:"name,omitempty"`
}

func (p *CouchbaseCollectionSpec) UnmarshalJSON(data []byte) error {
	var temp struct {
		Name    ScopeOrCollectionName `json:"name,omitempty"`
		History *bool                 `json:"history,omitempty"`
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	p.Name = temp.Name
	p.History = temp.History

	if err := p.UnmarshalMaxTTLWithNegativeOverride(data); err != nil {
		return err
	}

	return nil
}

func (p *CouchbaseCollectionSpec) MarshalJSON() ([]byte, error) {
	temp := struct {
		Name    ScopeOrCollectionName `json:"name,omitempty"`
		History *bool                 `json:"history,omitempty"`
		MaxTTL  string                `json:"maxTTL,omitempty"`
	}{
		Name:    p.Name,
		History: p.History,
	}

	if p.MaxTTL != nil {
		if p.MaxTTL.Duration == -1*time.Second {
			temp.MaxTTL = "-1"
		} else {
			temp.MaxTTL = p.MaxTTL.Duration.String()
		}
	}

	return json.Marshal(temp)
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

func (p *CouchbaseCollectionGroupSpec) UnmarshalJSON(data []byte) error {
	var temp struct {
		Names   []ScopeOrCollectionName `json:"names"`
		History *bool                   `json:"history,omitempty"`
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	p.Names = temp.Names
	p.History = temp.History

	if err := p.UnmarshalMaxTTLWithNegativeOverride(data); err != nil {
		return err
	}

	return nil
}

func (p *CouchbaseCollectionGroupSpec) MarshalJSON() ([]byte, error) {
	temp := struct {
		Names   []ScopeOrCollectionName `json:"names"`
		History *bool                   `json:"history,omitempty"`
		MaxTTL  string                  `json:"maxTTL,omitempty"`
	}{
		Names:   p.Names,
		History: p.History,
	}

	if p.MaxTTL != nil {
		if p.MaxTTL.Duration == -1*time.Second {
			temp.MaxTTL = "-1"
		} else {
			temp.MaxTTL = p.MaxTTL.Duration.String()
		}
	}

	return json.Marshal(temp)
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

// CouchbaseCollectionKind is the type of collection we are referring to.
// +kubebuilder:validation:Enum=CouchbaseCollection;CouchbaseCollectionGroup
type CouchbaseCollectionKind string

const (
	CouchbaseCollectionKindCollection      CouchbaseCollectionKind = "CouchbaseCollection"
	CouchbaseCollectionKindCollectionGroup CouchbaseCollectionKind = "CouchbaseCollectionGroup"
)

type CollectionLocalObjectReference struct {
	// Kind indicates the kind of resource that is being referenced.  A scope
	// can only reference `CouchbaseCollection` and `CouchbaseCollectionGroup`
	// resource kinds.  This field defaults to `CouchbaseCollection` if not
	// specified.
	// +kubebuilder:default=CouchbaseCollection
	Kind CouchbaseCollectionKind `json:"kind,omitempty"`

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
	// https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#labelselector-v1-meta
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

// CouchbaseScopeKind is the kind of scope we are referring to.
// +kubebuilder:validation:Enum=CouchbaseScope;CouchbaseScopeGroup
type CouchbaseScopeKind string

const (
	CouchbaseScopeKindScope      CouchbaseScopeKind = "CouchbaseScope"
	CouchbaseScopeKindScopeGroup CouchbaseScopeKind = "CouchbaseScopeGroup"
)

type ScopeLocalObjectReference struct {
	// Kind indicates the kind of resource that is being referenced.  A scope
	// can only reference `CouchbaseScope` and `CouchbaseScopeGroup`
	// resource kinds.  This field defaults to `CouchbaseScope` if not
	// specified.
	// +kubebuilder:default=CouchbaseScope
	Kind CouchbaseScopeKind `json:"kind,omitempty"`

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
	// https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#labelselector-v1-meta
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

	// SampleBucket indicates whether the bucket should be treated as a sample bucket.
	// If set to "true", the bucket name will define the sample bucket used and the bucket will be created with the sample bucket configuration, not the CRD specification.
	// SampleBuckets have a memory quota of 200Mi and a couchstore storage backend. If this annotation is changed to false or removed, the bucket will then be updated with the CRD specification. Sample buckets are
	// This annotation cannot be added to an existing bucket and should not be used for production clusters.
	SampleBucket bool `json:"-" annotation:"sampleBucket"`

	// StorageBackend to be assigned to and used by the bucket. Only valid for Couchbase Server 7.0.0 onward.
	// Two different backend storage mechanisms can be used - "couchstore" or "magma", defaulting to "couchstore".
	// Note: "magma" is only valid for Couchbase Server 7.1.0 onward.
	StorageBackend CouchbaseStorageBackend `json:"storageBackend,omitempty"`

	// NumVBuckets defines the number of virtual buckets (vBuckets) to be used by the bucket.
	// Can be either 128 or 1024 for magma buckets. This setting can only be adjusted for magma buckets.
	// +kubebuilder:default=1024
	NumVBuckets int `json:"numVBuckets,omitempty"`

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

	// OnlineEvictionPolicyChange controls whether eviction policy changes can be made online
	// without requiring a bucket restart. If set the eviction policy change will only take effect
	// on the bucket nodes after a swap rebalance, delta recovery, or full recovery. If EnableBucketMigrationRoutines is set to true,
	// on the cluster the operator will perform the swap rebalances. This field defaults to false.
	// This field is only supported for Couchbase Server 8.0.0+.
	// +kubebuilder:validation:Optional
	OnlineEvictionPolicyChange bool `json:"onlineEvictionPolicyChange,omitempty"`

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

	// HistoryRetention configures settings for bucket history retention and default values for associated collections.
	HistoryRetentionSettings *HistoryRetentionSettings `json:"historyRetention,omitempty" annotation:"historyRetention"`

	// MagmaSeqTreeDataBlockSize is the block size, in bytes, for Magma seqIndex blocks.
	MagmaSeqTreeDataBlockSize *uint64 `json:"-" annotation:"magmaSeqTreeDataBlockSize"`

	// MagmaKeyTreeDataBlockSize is the block size, in bytes, for Magma keyIndex blocks.
	MagmaKeyTreeDataBlockSize *uint64 `json:"-" annotation:"magmaKeyTreeDataBlockSize"`

	// Rank determines the bucket's place in the order in which the rebalance process
	// handles the buckets on the cluster. The higher a bucket's assigned integer
	// (in relation to the integers assigned other buckets), the sooner in the
	// rebalance process the bucket is handled. This assignment of rank allows a
	// cluster's most mission-critical data to be rebalanced with top priority.
	// This option is only supported for Couchbase Server 7.6.0+.
	// +kubebuilder:default=0
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	Rank int `json:"rank,omitempty"`

	// AutoCompaction allows the configuration of auto-compaction settings, including on what
	// conditions disk space is reclaimed and when it is allowed to run, on a per-bucket basis.
	// If any of these fields are configured, those that are not configured here will take the value set at the cluster level.
	// Excluding this field (which is the default), will set the autoCompactionSettings to false and the bucket will use cluster defaults.
	// +optional
	AutoCompaction *AutoCompactionSpecBucket `json:"autoCompaction,omitempty" annotation:"autoCompaction"`

	// EnableCrossClusterVersioning allows the bucket to be configured to allow cross-cluster versioning.
	// This feature is only supported for Couchbase Server 7.6.0+. Once it has been set to true, it cannot be toggled to false.
	EnableCrossClusterVersioning *bool `json:"enableCrossClusterVersioning,omitempty" annotation:"enableCrossClusterVersioning"`

	// VersionPruningWindowHrs defines the number of hours to retain version history for a bucket.
	// This field must be an integer larger than 23, defaulting to 720 (30 days).
	// This feature is only supported for Couchbase Server 7.6.0+.
	VersionPruningWindowHrs *uint64 `json:"versionPruningWindowHrs,omitempty" annotation:"versionPruningWindowHrs"`

	// AccessScannerEnabled allows the bucket to be configured to allow enabling and disabling the access scanner.
	// This feature is only supported for Couchbase Server 8.0.0+. It is set to true by default.
	// +kubebuilder:default=true
	AccessScannerEnabled *bool `json:"accessScannerEnabled,omitempty"`

	// ExpiryPagerSleepTime defines the time between Expiry Pager runs.
	// It defaults to 10 minutes. This field is only supported for Couchbase Server 8.0.0+.
	// +kubebuilder:default="10m"
	ExpiryPagerSleepTime *metav1.Duration `json:"expiryPagerSleepTime,omitempty"`

	// WarmupBehavior defines the behavior of the bucket when it is being warmed up.
	// It defaults to "background". This field is only supported for Couchbase Server 8.0.0+.
	// +kubebuilder:validation:Enum=none;background;blocking
	// +kubebuilder:default="background"
	WarmupBehavior CouchbaseBucketWarmupBehavior `json:"warmupBehavior,omitempty"`

	// MemoryLowWatermark defines the memory low watermark for the bucket.
	// It must be between 50 and 89. It must also be less than spec.memoryHighWatermark.
	// It defaults to 75. This field is only supported for Couchbase Server 8.0.0+.
	// +kubebuilder:default=75
	// +kubebuilder:validation:Minimum=50
	// +kubebuilder:validation:Maximum=89
	MemoryLowWatermark *int `json:"memoryLowWatermark,omitempty"`

	// MemoryHighWatermark defines the memory high watermark for the bucket.
	// It must be between 51 and 90. It must also be greater than spec.memoryLowWatermark.
	// It defaults to 85. This field is only supported for Couchbase Server 8.0.0+.
	// +kubebuilder:default=85
	// +kubebuilder:validation:Minimum=51
	// +kubebuilder:validation:Maximum=90
	MemoryHighWatermark *int `json:"memoryHighWatermark,omitempty"`

	// DurabilityImpossibleFallback defines whether to report writes as durable even if not enough replicas are written to.
	// This feature is only supported for Couchbase Server 8.0.0+. Defaults to disabled.
	DurabilityImpossibleFallback DurabilityImpossibleFallback `json:"durabilityImpossibleFallback,omitempty"`

	// EncryptionAtRest defines the encryption at rest settings for the bucket.
	// This field is only supported for Couchbase Server 8.0.0+.
	// +optional
	EncryptionAtRest *BucketEncryptionAtRestConfiguration `json:"encryptionAtRest,omitempty"`
}

type BucketEncryptionAtRestConfiguration struct {
	// Key is the name of the encryption key to use for encryption at rest.
	KeyName string `json:"keyName"`

	// RotationInterval is the interval at which the encryption key will be rotated.
	// Must be greater or equal to 7 days. Default is 30 days.
	// +kubebuilder:default="720h"
	RotationInterval *metav1.Duration `json:"rotationInterval,omitempty"`

	// KeyLifetime is the lifetime of the encryption key.
	// Must be greater or equal to 30 days. Default is 365 days.
	// +kubebuilder:default="8760h"
	KeyLifetime *metav1.Duration `json:"keyLifetime,omitempty"`
}

type HistoryRetentionSettings struct {
	// Seconds defines how many seconds of history an individual vbucket should aim to retain on disk. This field defaults to 0.
	// This is only supported on buckets with storageBackend=magma.
	Seconds uint64 `json:"seconds,omitempty" annotation:"seconds"`
	// Bytes defines how much history an individual vbucket should aim to retain on disk in bytes. This field defaults to 0 and has a minimum working value of 2147483648.
	// This is only supported on buckets with storageBackend=magma.
	Bytes uint64 `json:"bytes,omitempty" annotation:"bytes"`
	// CollectionHistoryDefault determines whether history retention is enabled for newly created collections by default. This field defaults to true.
	// This is only supported on buckets with storageBackend=magma.
	// +kubebuilder:default=true
	CollectionDefault *bool `json:"collectionHistoryDefault,omitempty" annotation:"collectionHistoryDefault"`
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

	// SampleBucket indicates whether the bucket should be treated as a sample bucket.
	// If set to "true", the bucket name will define the sample bucket used and the bucket will be created with the sample bucket configuration, not the CRD specification.
	// SampleBuckets have a memory quota of 200Mi and a couchstore storage backend. If this annotation is changed to false or removed, the bucket will then be updated with the CRD specification. Sample buckets are
	// This annotation cannot be added to an existing bucket and should not be used for production clusters.
	SampleBucket bool `json:"-" annotation:"sampleBucket"`

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

	// Rank determines the bucket's place in the order in which the rebalance process
	// handles the buckets on the cluster. The higher a bucket's assigned integer
	// (in relation to the integers assigned other buckets), the sooner in the
	// rebalance process the bucket is handled. This assignment of rank allows a
	// cluster's most mission-critical data to be rebalanced with top priority.
	// cluster’s most mission-critical data to be rebalanced with top priority.
	// This option is only supported for Couchbase Server 7.6.0+.
	// +kubebuilder:default=0
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	Rank int `json:"rank,omitempty"`

	// EnableCrossClusterVersioning allows the bucket to be configured to allow cross-cluster versioning.
	// This feature is only supported for Couchbase Server 7.6.0+. Once it has been set to true, it cannot be toggled to false.
	EnableCrossClusterVersioning *bool `json:"enableCrossClusterVersioning,omitempty" annotation:"enableCrossClusterVersioning"`

	// VersionPruningWindowHrs defines the number of hours to retain version history for a bucket.
	// This field must be an integer larger than 23, defaulting to 720 (30 days).
	VersionPruningWindowHrs *uint64 `json:"versionPruningWindowHrs,omitempty" annotation:"versionPruningWindowHrs"`

	// ExpiryPagerSleepTime defines the time between Expiry Pager runs.
	// It defaults to 10 minutes.
	// +kubebuilder:default="10m"
	ExpiryPagerSleepTime *metav1.Duration `json:"expiryPagerSleepTime,omitempty"`

	// WarmupBehavior defines the behavior of the bucket when it is being warmed up.
	// It defaults to "background".
	// +kubebuilder:validation:Enum=none;background;blocking
	// +kubebuilder:default="background"
	WarmupBehavior CouchbaseBucketWarmupBehavior `json:"warmupBehavior,omitempty"`

	// MemoryLowWatermark defines the memory low watermark for the bucket.
	// It must be between 50 and 89. It must also be less than spec.memoryHighWatermark.
	// It defaults to 75.
	// +kubebuilder:default=75
	// +kubebuilder:validation:Minimum=50
	// +kubebuilder:validation:Maximum=89
	MemoryLowWatermark *int `json:"memoryLowWatermark,omitempty"`

	// MemoryHighWatermark defines the memory high watermark for the bucket.
	// It must be between 51 and 90. It must also be greater than spec.memoryLowWatermark.
	// It defaults to 85.
	// +kubebuilder:default=85
	// +kubebuilder:validation:Minimum=51
	// +kubebuilder:validation:Maximum=90
	MemoryHighWatermark *int `json:"memoryHighWatermark,omitempty"`

	// DurabilityImpossibleFallback defines whether to report write as durable even if not enough replicas are written to.
	// This feature is only supported for Couchbase Server 8.0.0+. Defaults to disabled.
	DurabilityImpossibleFallback DurabilityImpossibleFallback `json:"durabilityImpossibleFallback,omitempty"`

	// OnlineEvictionPolicyChange controls whether eviction policy changes can be made online
	// without requiring a bucket restart. If set the eviction policy change will only take effect
	// on the bucket nodes after a swap rebalance, delta recovery, or full recovery. If EnableBucketMigrationRoutines is set to true,
	// on the cluster the operator will perform the swap rebalances. This field defaults to false.
	// This field is only supported for Couchbase Server 8.0.0+.
	// +kubebuilder:validation:Optional
	OnlineEvictionPolicyChange bool `json:"onlineEvictionPolicyChange,omitempty"`
}

type CouchbaseBucketWarmupBehavior string

const (
	CouchbaseBucketWarmupBehaviorNone       CouchbaseBucketWarmupBehavior = "none"
	CouchbaseBucketWarmupBehaviorBackground CouchbaseBucketWarmupBehavior = "background"
	CouchbaseBucketWarmupBehaviorBlocking   CouchbaseBucketWarmupBehavior = "blocking"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseEphemeralBucketList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseEphemeralBucket `json:"items"`
}

// DEPRECATED - Memcached buckets are now deprecated in Couchbase Server and should not be used, they will be removed in a future release.
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

	// SampleBucket indicates whether the bucket should be treated as a sample bucket.
	// If set to "true", the bucket name will define the sample bucket used and the bucket will be created with the sample bucket configuration, not the CRD specification.
	// SampleBuckets have a memory quota of 200Mi and a couchstore storage backend. If this annotation is changed to false or removed, the bucket will then be updated with the CRD specification. Sample buckets are
	// This annotation cannot be added to an existing bucket and should not be used for production clusters.
	SampleBucket bool `json:"-" annotation:"sampleBucket"`

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

// ColMappingRules represents collection mapping rules for XDCR explicit mapping or migration.
// It's a map where keys are source collections/scopes and values are target collections/scopes.
// Nil values represent denial rules (don't replicate).
// The rules follow Couchbase Server priority order:
// Priority 0: scope.collection to scope.collection affirmation
// Priority 1: scope.collection denial (null value)
// Priority 2: scope to scope affirmation
// Priority 3: scope denial (null value)
type ColMappingRules map[string]*string

// MergeFunctionMappingRules represents merge function mapping rules for XDCR conflict resolution.
// It's a map where keys are collection specifiers (scope.collection) and values are merge function names.
// Nil values can be used to explicitly unset merge functions for specific collections.
type MergeFunctionMappingRules map[string]*string

// CouchbaseReplicationSpec allows configuration of an XDCR replication.
type CouchbaseReplicationSpec struct {
	// === CORE IMMUTABLE FIELDS (validation runner enforced) ===

	// Bucket is the source bucket to replicate from. This refers to the Couchbase
	// bucket name, not the resource name of the bucket. A bucket with this name must
	// be defined on this cluster.  Legal bucket names have a maximum length of 100
	// characters and may be composed of any character from "a-z", "A-Z", "0-9" and "-_%\.".
	Bucket BucketName `json:"bucket"`

	// RemoteBucket is the remote bucket name to synchronize to. This refers to the
	// Couchbase bucket name, not the resource name of the bucket. Legal bucket names
	// have a maximum length of 100 characters and may be composed of any character from
	// "a-z", "A-Z", "0-9" and "-_%\.".
	RemoteBucket BucketName `json:"remoteBucket"`

	// FilterSkipRestream controls whether replication restarts after filterExpression changes.
	// When false (default), replication restarts after filter changes. When true, continues without restart.
	// +kubebuilder:default=false
	FilterSkipRestream *bool `json:"filterSkipRestream,omitempty"`

	// === PER-REPLICATION MUTABLE SETTINGS (all pointers) ===

	// Per-replication-only settings (cannot be set globally)

	// ColMappingRules defines collection-related rules for explicit mapping or migration.
	ColMappingRules *ColMappingRules `json:"colMappingRules,omitempty"`

	// CollectionsExplicitMapping enables explicit mapping rules from colMappingRules.
	// When true, replication uses explicit mapping rules and implicit bucket-level mapping does not occur.
	// +kubebuilder:default=false
	CollectionsExplicitMapping *bool `json:"collectionsExplicitMapping,omitempty"`

	// CollectionsMigrationMode enables migration mode for default collection documents.
	// When true, documents from source default collection are replicated to collections determined by mapping rules.
	// +kubebuilder:default=false
	CollectionsMigrationMode *bool `json:"collectionsMigrationMode,omitempty"`

	// FilterExpression is a filter expression to match against documents in the source bucket.
	// Each document that produces a successful match is replicated.
	FilterExpression *string `json:"filterExpression,omitempty"`

	// PauseRequested indicates whether the replication has been issued a pause request.
	// +kubebuilder:default=false
	PauseRequested *bool `json:"pauseRequested,omitempty"`

	// MergeFunctionMapping maps collection specifiers (scope.collection) to merge function names for custom conflict resolution.
	// Nil values can be used to explicitly unset merge functions for specific collections.
	MergeFunctionMapping MergeFunctionMappingRules `json:"mergeFunctionMapping,omitempty"`

	// Shared settings (can override global defaults)

	// CheckpointInterval is the interval in seconds between checkpoints.
	// +kubebuilder:validation:Minimum=60
	// +kubebuilder:validation:Maximum=14400
	CheckpointInterval *int32 `json:"checkpointInterval,omitempty"`

	// CollectionsOSOMode optimizes for out-of-order mutations streaming (performance toggle).
	// This field defaults to true.
	CollectionsOSOMode *bool `json:"collectionsOSOMode,omitempty"`

	// CompressionType is the compression used for XDCR traffic.
	// +kubebuilder:validation:Enum=Auto;None
	CompressionType *string `json:"compressionType,omitempty"`

	// DesiredLatency is the target latency (ms) for high-priority replications.
	// This field defaults to 50.
	DesiredLatency *int32 `json:"desiredLatency,omitempty"`

	// DocBatchSizeKb is the size (KB) of document batches sent.
	// +kubebuilder:validation:Minimum=10
	// +kubebuilder:validation:Maximum=10000
	DocBatchSizeKb *int32 `json:"docBatchSizeKb,omitempty"`

	// FailureRestartInterval is the seconds to wait before restarting after a failure.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=300
	FailureRestartInterval *int32 `json:"failureRestartInterval,omitempty"`

	// FilterBinary specifies whether binary documents should be replicated.
	FilterBinary *bool `json:"filterBinary,omitempty"`

	// FilterBypassExpiry when true, TTL is removed before replication.
	FilterBypassExpiry *bool `json:"filterBypassExpiry,omitempty"`

	// FilterBypassUncommittedTxn when true, documents with uncommitted txn xattrs are not replicated.
	FilterBypassUncommittedTxn *bool `json:"filterBypassUncommittedTxn,omitempty"`

	// FilterDeletion when true, delete mutations are filtered out (not replicated).
	FilterDeletion *bool `json:"filterDeletion,omitempty"`

	// FilterExpiration when true, expiry mutations are filtered out.
	FilterExpiration *bool `json:"filterExpiration,omitempty"`

	// HlvPruningWindowSec is the HLV pruning window (sec) for hybrid logical vector conflict resolution.
	// +kubebuilder:validation:Minimum=1
	HlvPruningWindowSec *int32 `json:"hlvPruningWindowSec,omitempty"`

	// JSFunctionTimeoutMs is the timeout for JS custom conflict-resolution functions (ms).
	// +kubebuilder:validation:Minimum=1
	JSFunctionTimeoutMs *int32 `json:"jsFunctionTimeoutMs,omitempty"`

	// LogLevel is the logging verbosity for XDCR.
	// +kubebuilder:validation:Enum=Error;Info;Debug;Trace
	LogLevel *string `json:"logLevel,omitempty"`

	// Mobile enables mobile (Sync Gateway) active-active mode.
	// This feature is available in Couchbase Server 7.6.4 and later
	// +kubebuilder:validation:Enum=Off;Active
	Mobile *string `json:"mobile,omitempty" annotation:"mobile"`

	// NetworkUsageLimit is the upper limit for replication network usage (MB/s).
	// +kubebuilder:validation:Minimum=0
	NetworkUsageLimit *int32 `json:"networkUsageLimit,omitempty"`

	// OptimisticReplicationThreshold is the size threshold below which documents replicate optimistically.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=20971520
	OptimisticReplicationThreshold *int32 `json:"optimisticReplicationThreshold,omitempty"`

	// Priority is the resource priority for replication streams.
	// +kubebuilder:validation:Enum=High;Medium;Low
	Priority *string `json:"priority,omitempty"`

	// RetryOnRemoteAuthErr defines whether to retry connections when remote auth fails.
	RetryOnRemoteAuthErr *bool `json:"retryOnRemoteAuthErr,omitempty"`

	// RetryOnRemoteAuthErrMaxWaitSec is the max wait seconds for retrying remote auth failures.
	// Only effective if retryOnRemoteAuthErr is true.
	// +kubebuilder:validation:Minimum=1
	RetryOnRemoteAuthErrMaxWaitSec *int32 `json:"retryOnRemoteAuthErrMaxWaitSec,omitempty"`

	// SourceNozzlePerNode is the number of source nozzles (parallelism) per source node.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	SourceNozzlePerNode *int32 `json:"sourceNozzlePerNode,omitempty"`

	// StatsInterval is the interval for statistics updates (ms).
	// +kubebuilder:validation:Minimum=200
	// +kubebuilder:validation:Maximum=600000
	StatsInterval *int32 `json:"statsInterval,omitempty"`

	// TargetNozzlePerNode is the number of target nozzles per target node (parallelism).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	TargetNozzlePerNode *int32 `json:"targetNozzlePerNode,omitempty"`

	// WorkerBatchSize is the number of mutations per worker batch.
	// +kubebuilder:validation:Minimum=500
	// +kubebuilder:validation:Maximum=10000
	WorkerBatchSize *int32 `json:"workerBatchSize,omitempty"`

	// ConflictLogging is the configuration for conflict logging.
	// This feature is available in Couchbase Server 8.0.0 and later.
	ConflictLogging *CouchbaseConflictLoggingSpec `json:"conflictLogging,omitempty"`
}

// CouchbaseConflictLoggingSpec defines the configuration for conflict logging.
type CouchbaseConflictLoggingSpec struct {
	// Enabled defines whether conflict logging is enabled.
	Enabled bool `json:"enabled,omitempty"`

	// LogCollection defines the collection to log conflicts to.
	LogCollection CouchbaseConflictLogCollection `json:"logCollection,omitempty"`

	// LoggingRules defines the list of logging rules for conflict logging. The rules can
	// be scoped to a specific scope or a specific collection in a scope. The rules can disable
	// logging, log to the default collection defined at `spec.conflictLogging.logCollection`, or
	// log to a different collection.
	LoggingRules CouchbaseLoggingRulesSpec `json:"loggingRules,omitempty"`
}

// CouchbaseLoggingRulesSpec defines the list of logging rules for conflict logging.
type CouchbaseLoggingRulesSpec struct {
	// DefaultCollectionRules defines the rules for logging to the default collection.
	DefaultCollectionRules []CouchbaseConflictKeyspace `json:"defaultCollectionRules,omitempty"`

	// NoLoggingRules defines the rules for disabling logging to for conflicts in a specific scope or collection.
	NoLoggingRules []CouchbaseConflictKeyspace `json:"noLoggingRules,omitempty"`

	// CustomCollectionRules defines the rules for logging to a different collection.
	CustomCollectionRules []CouchbaseConflictCustomCollectionRule `json:"customCollectionRules,omitempty"`
}

// CouchbaseConflictKeyspace defines a scope or collection to apply the rule to.
type CouchbaseConflictKeyspace struct {
	// Scope defines the scope to apply the rule to.
	Scope ScopeOrCollectionNameIncludingDefault `json:"scope,omitempty"`

	// Collection defines the collection to apply the rule to.
	Collection ScopeOrCollectionNameIncludingDefault `json:"collection,omitempty"`
}

// CouchbaseConflictLogCollection defines the collection to log conflicts to.
type CouchbaseConflictLogCollection struct {
	// Bucket defines the bucket to log conflicts to.
	Bucket BucketName `json:"bucket,omitempty"`

	// Scope defines the scope to log conflicts to.
	Scope ScopeOrCollectionNameIncludingDefault `json:"scope,omitempty"`

	// Collection defines the collection to log conflicts to.
	Collection ScopeOrCollectionNameIncludingDefault `json:"collection,omitempty"`
}

// CouchbaseConflictCustomCollectionRule defines a rule for conflict logging that logs to a different collection.
type CouchbaseConflictCustomCollectionRule struct {
	// Scope defines the scope to apply the rule to.
	Scope ScopeOrCollectionNameIncludingDefault `json:"scope"`

	// Collection defines the collection to apply the rule to.
	Collection ScopeOrCollectionNameIncludingDefault `json:"collection,omitempty"`

	// LogCollection defines the collection to log conflicts to.
	LogCollection CouchbaseConflictLogCollection `json:"logCollection"`
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

	// Username of the couchbase user.
	Name string `json:"name,omitempty"`

	// The domain which provides user authentication.
	// +kubebuilder:validation:Enum=local;external
	AuthDomain AuthDomain `json:"authDomain"`

	// Name of Kubernetes secret to be used as a user's initial password for the Couchbase domain.
	// Once a user has been created, any further changes to this field will be ignored.
	AuthSecret string `json:"authSecret,omitempty"`

	// Locked defines whether the user is locked.
	// This field is only available for Couchbase Server 8.0.0+.
	Locked *bool `json:"locked,omitempty"`

	// Password allows user specific password settings to be set.
	// This field is only available for Couchbase Server 8.0.0+.
	Password *CouchbaseUserPasswordSpec `json:"password,omitempty"`
}

type CouchbaseUserPasswordSpec struct {
	// RequireInitialChange defines whether a user will be required to change
	// their password the first time they login and is only effective when a user is first being created.
	// This field is only available for Couchbase Server 8.0.0+.
	RequireInitialChange *bool `json:"requireInitialChange,omitempty"`

	// ExpiresAt allows setting a timestamp when a user's password will expire. After that timestamp has passed, the user
	// will be required to change their password.
	// If set to a timestamp in the past, the user's password must have been changed since then or they
	// will be required to change their password.
	// This field is only available for Couchbase Server 8.0.0+.
	ExpiresAt *metav1.Time `json:"expiresAt,omitempty"`

	// ExpiresAfter allows setting a fixed duration after a user changes their password when they will be required to change their password again.
	// Consider that this duration will be checked against the users last password change date, not the current date and time and could therefore
	// result in the user being required to change their password immediately.
	// This field is only available for Couchbase Server 8.0.0+. More info:
	// https://golang.org/pkg/time/#ParseDuration
	ExpiresAfter *metav1.Duration `json:"expiresAfter,omitempty"`
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
	RoleSecurityAdmin     RoleName = "security_admin"
	RoleUserAdminLocal    RoleName = "user_admin_local"
	RoleUserAdminExternal RoleName = "user_admin_external"
	// Deprecated: These roles are removed in Morpheus (8.0+) and replaced by security_admin + user_admin_*
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
	RoleSyncGatewayApplication              RoleName = "sync_gateway_app"
	RoleSyncGatewayApplicationReadOnly      RoleName = "sync_gateway_app_ro"
	RoleSyncGatewayArchitect                RoleName = "sync_gateway_configurator"
	RoleSyncDevOps                          RoleName = "sync_gateway_dev_ops"
	RoleSyncReplicator                      RoleName = "sync_gateway_replicator"
	RoleBackupAdmin                         RoleName = "backup_admin"
	RoleAnalyticsSelect                     RoleName = "analytics_select"
	RoleAnalyticsAdmin                      RoleName = "analytics_admin"
	RoleEventingAdmin                       RoleName = "eventing_admin"
	RoleEventingManageFunctions             RoleName = "eventing_manage_functions"
	RoleQueryUseSequentialScans             RoleName = "query_use_sequential_scans"
	RoleQueryUseSequences                   RoleName = "query_use_sequences"
	RoleQueryManageSequences                RoleName = "query_manage_sequences"
)

type Role struct {
	// Name of role.
	// +kubebuilder:validation:Enum=admin;analytics_admin;analytics_manager;analytics_reader;analytics_select;backup_admin;bucket_admin;bucket_full_access;cluster_admin;data_backup;data_dcp_reader;data_monitoring;data_reader;data_writer;eventing_admin;eventing_manage_functions;external_stats_reader;fts_admin;fts_searcher;mobile_sync_gateway;query_delete;query_execute_external_functions;query_execute_functions;query_execute_global_external_functions;query_execute_global_functions;query_external_access;query_insert;query_manage_external_functions;query_manage_functions;query_manage_global_external_functions;query_manage_global_functions;query_manage_index;query_manage_sequences;query_select;query_system_catalog;query_update;query_use_sequences;query_use_sequential_scans;replication_admin;replication_target;ro_admin;scope_admin;security_admin;security_admin_external;security_admin_local;sync_gateway_app;sync_gateway_app_ro;sync_gateway_configurator;sync_gateway_dev_ops;sync_gateway_replicator;user_admin_external;user_admin_local;views_admin;views_reader
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
	// https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#labelselector-v1-meta
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

// CouchbaseBucketKind represents the kind of bucket being referred to.
// +kubebuilder:validation:Enum=CouchbaseBucket
type CouchbaseBucketKind string

const (
	CouchbaseBucketKindBucket CouchbaseBucketKind = "CouchbaseBucket"
)

type BucketLocalObjectReference struct {
	// Kind indicates the kind of resource that is being referenced.  A Role
	// can only reference `CouchbaseBucket` kind.  This field defaults
	// to `CouchbaseBucket` if not specified.
	// +kubebuilder:default=CouchbaseBucket
	Kind CouchbaseBucketKind `json:"kind,omitempty"`

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
	// https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#labelselector-v1-meta
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

type CollectionRoleSpec struct {
	// Resources is an explicit list of named resources that will be considered
	// for inclusion in this collection or collections.  If a resource reference doesn't
	// match a resource, then no error conditions are raised due to undefined
	// resource creation ordering and eventual consistency.
	Resources []CollectionLocalObjectReference `json:"resources,omitempty"`

	// Selector allows resources to be implicitly considered for inclusion in this
	// collection or collections.  More info:
	// https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#labelselector-v1-meta
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
// to interact with the Couchbase cluster and provide autoscaling.  This resource is
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
	// Internal services for connection to external CB clusters
	N2NService Service = "management"
)

type ServiceList []Service

// +kubebuilder:validation:Enum=admin;xdcr;client;backup;external-cluster-connection
type ExposedFeature string

// Supported features
const (
	// Exposes the admin port/UI
	FeatureAdmin ExposedFeature = "admin"
	// Exposes ports necessary for XDCR
	FeatureXDCR ExposedFeature = "xdcr"
	// Exposes all client ports for services
	FeatureClient ExposedFeature = "client"
	// Exposes all ports necessary for backup
	FeatureBackup ExposedFeature = "backup"
	// Exposes all ports necessary for connections to external clusters
	FeatureExternalConnection ExposedFeature = "external-cluster-connection"
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

// This defines the default upgrade process for couchbase pods. Defaults to SwapRebalance.
// +kubebuilder:validation:Enum=SwapRebalance;DeltaRecovery;InPlaceUpgrade
type UpgradeProcess string

const (
	// SwapRebalance will default to upgrading one node at a time. This is much
	// safer but will take a longer time than a InPlaceUpgrade.
	SwapRebalance UpgradeProcess = "SwapRebalance"

	// DEPRECATED - by InPlaceUpgrade
	// DeltaRecovery will perform an in-place upgrade of the pods in the cluster.
	// It will also update PVCs to use the new couchbase server version.
	DeltaRecovery UpgradeProcess = "DeltaRecovery"

	InPlaceUpgrade UpgradeProcess = "InPlaceUpgrade"
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

type UpgradeOrderType string

const (
	UpgradeOrderTypeNodes         UpgradeOrderType = "Nodes"
	UpgradeOrderTypeServerGroups  UpgradeOrderType = "ServerGroups"
	UpgradeOrderTypeServerClasses UpgradeOrderType = "ServerClasses"
	UpgradeOrderTypeServices      UpgradeOrderType = "Services"
)

// UpgradeSpec defines the upgrade configuration for a Couchbase cluster.
type UpgradeSpec struct {
	// UpgradeProcess defines the process that will be used when performing a couchbase cluster upgrade.
	// When SwapRebalance is requested (default), pods will be upgraded using either a RollingUpgrade or
	// ImmediateUpgrade (determined by UpgradeStrategy). When InPlaceUpgrade is requested, the operator will
	// perform an in-place upgrade on a best effort basis. InPlaceUpgrade cannot be used if the UpgradeStrategy
	// is set to ImmediateUpgrade.
	// +kubebuilder:default="SwapRebalance"
	UpgradeProcess UpgradeProcess `json:"upgradeProcess,omitempty"`

	// UpgradeStrategy controls how aggressive the Operator is when performing a cluster
	// upgrade.  When a rolling upgrade is requested, pods are upgraded one at a time.  This
	// strategy is slower, however less disruptive.  When an immediate upgrade strategy is
	// requested, all pods are upgraded at the same time.  This strategy is faster, but more
	// disruptive.  This field must be either "RollingUpgrade" or "ImmediateUpgrade", defaulting
	// to "RollingUpgrade".
	// +kubebuilder:default="RollingUpgrade"
	UpgradeStrategy UpgradeStrategy `json:"upgradeStrategy,omitempty"`

	// StabilizationPeriod is the time the operator will wait after an upgrade cycle before starting the next upgrade cycle.
	// If not specified the operator will start the next upgrade immediately.
	// +optional
	StabilizationPeriod *metav1.Duration `json:"stabilizationPeriod,omitempty"`

	// When `spec.upgradeStrategy` is set to `RollingUpgrade` it will, by default, upgrade one pod
	// at a time.  If this field is specified then that number can be increased.
	RollingUpgrade *RollingUpgradeConstraints `json:"rollingUpgrade,omitempty"`

	// PreviousVersionPodCount is the number of pods that will be left running at the existing version.
	// NOTE: The cluster will not be fully upgraded until all pods are at the new version.
	// The default is 0.
	// +optional
	// +kubebuilder:default=0
	// +kubebuilder:validation:Minimum=0
	PreviousVersionPodCount int `json:"previousVersionPodCount,omitempty"`

	// UpgradeOrderType defines the order in which spec.upgrade.upgradeOrderSequence will be interpreted.
	// +kubebuilder:validation:Enum=Nodes;ServerGroups;ServerClasses;Services
	// +kubebuilder:default="Nodes"
	UpgradeOrderType UpgradeOrderType `json:"upgradeOrderType,omitempty"`

	// UpgradeOrder defines the sequence in which nodes will be upgraded.
	// The sequence will be interpreted based on what `spec.upgrade.upgradeOrderBy` is set to.
	// If `spec.upgrade.upgradeOrderType` is set to "Nodes" then the sequence will be a list of node names.
	// If `spec.upgrade.upgradeOrderType` is set to "ServerGroups" then the sequence will be a list of server group names.
	// If `spec.upgrade.upgradeOrderType` is set to "ServerClasses" then the sequence will be a list of server class names.
	// If `spec.upgrade.upgradeOrderType` is set to "Services" then the sequence will be a list of service names.
	UpgradeOrder []string `json:"upgradeOrder,omitempty"`
}

// ClusterSpec is the specification for a CouchbaseCluster resources, and allows
// the cluster to be customized.
type ClusterSpec struct {
	// Image is the container image name that will be used to launch Couchbase
	// server instances.  Updating this field will cause an automatic upgrade of
	// the cluster. Explicitly specifying the image for a server class will override
	// this value for the server class.
	// +kubebuilder:validation:Pattern="^(.*?(:\\d+)?/)?.*?/.*?(:.*?\\d+\\.\\d+\\.\\d+.*|@sha256:[0-9a-f]{64})$"
	Image string `json:"image"`

	// EnvImagePrecedence gives precedence over the default container image name in
	// `spec.Image` to an image name provided through Operator environment variables.
	// For more info on using Operator environment variables:
	// https://docs.couchbase.com/operator/current/reference-operator-configuration.html
	EnvImagePrecedence bool `json:"envImagePrecedence,omitempty"`

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

	// Upgrade defines the upgrade configuration for a Couchbase cluster.
	Upgrade *UpgradeSpec `json:"upgrade,omitempty"`

	// DEPRECATED - By spec.upgrade.upgradeProcess.
	// UpgradeProcess defines the process that will be used when performing a couchbase cluster upgrade.
	// When SwapRebalance is requested (default), pods will be upgraded using either a RollingUpgrade or
	// ImmediateUpgrade (determined by UpgradeStrategy). When InPlaceUpgrade is requested, the operator will
	// perform an in-place upgrade on a best effort basis. InPlaceUpgrade cannot be used if the UpgradeStrategy
	// is set to ImmediateUpgrade.
	UpgradeProcess *UpgradeProcess `json:"upgradeProcess,omitempty"`

	// DEPRECATED - By spec.upgrade.upgradeStrategy.
	// UpgradeStrategy controls how aggressive the Operator is when performing a cluster
	// upgrade.  When a rolling upgrade is requested, pods are upgraded one at a time.  This
	// strategy is slower, however less disruptive.  When an immediate upgrade strategy is
	// requested, all pods are upgraded at the same time.  This strategy is faster, but more
	// disruptive.  This field must be either "RollingUpgrade" or "ImmediateUpgrade", defaulting
	// to "RollingUpgrade".
	UpgradeStrategy *UpgradeStrategy `json:"upgradeStrategy,omitempty"`

	// DEPRECATED - By spec.upgrade.rollingUpgrade.
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
	ClusterSettings ClusterConfig `json:"cluster" annotation:",inline"`

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

	// PerServiceClassPDB determines whether a pod disruption budget (PDB) should be created for each service class.
	// By default, a single PDB will be created for the cluster with a minAvailable value of one less than the total number of requested Couchbase nodes in the cluster,
	// meaning only a single Couchbase node can be voluntarily disrupted at a time. When this field is set to true, a PDB will be created for each
	// service class, with a minAvailable value of one less than the service class size. This allows for a more granular
	// control over the number of Couchbase nodes that can be voluntarily disrupted at a time, such as during a Kubernetes upgrade.
	// In order to enable this feature, the size of each service class must be at least 2 and the maximum number of Couchbase nodes
	// that the PDB's would allow to be disrupted at once cannot exceed 50% of the total number of Couchbase nodes requested in the cluster specification.
	// Furthermore, the requested number of replicas for both the index and data services must remain less than the minimum number
	// of Couchbase nodes that the server class PDB's will cumulatively allow for.
	// +kubebuilder:default=false
	PerServiceClassPDB bool `json:"perServiceClassPDB,omitempty"`

	// ShuffleServerGroups allows the Operator to shuffle server groups before
	// scheduling pods. This can be useful to randomly place pods across availability
	// zones and not biasing to the first availability zones.
	ShuffleServerGroups bool `json:"-" annotation:"shuffleServerGroups"`

	// RescheduleDifferentServerGroup allows the Operator to attempt to  reschedule pods
	// to a different server group a pod scheduling.
	// +kubebuilder:default=true
	RescheduleDifferentServerGroup bool `json:"-" annotation:"rescheduleDifferentServerGroup"`

	// DEPRECATED - by spec.security.securityContext
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
	Networking CouchbaseClusterNetworkingSpec `json:"networking,omitempty" annotation:"networking"`

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
	Buckets Buckets `json:"buckets,omitempty" annotation:"buckets"`

	// XDCR defines whether the Operator should manage XDCR, remote clusters and how
	// to lookup replication resources.
	XDCR XDCR `json:"xdcr,omitempty" annotation:"xdcr"`

	// DEPRECATED - By Couchbase Server metrics endpoint on version 7.0+
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

	// OnlineVolumeExpansionTimeoutInMins must be provided as a retry mechanism with a timeout in minutes
	// for expanding volumes. This must only be provided, if EnableOnlineVolumeExpansion is set to true.
	// Value must be between 0 and 30.
	// If no value is provided, then it defaults to 10 minutes.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=30
	OnlineVolumeExpansionTimeoutInMins *int `json:"onlineVolumeExpansionTimeoutInMins,omitempty"`

	// Migration defines the specification for a CouchbaseCluster assimilation of an unmanaged
	// cluster to a managed Kubernetes cluster
	Migration *ClusterAssimilationSpec `json:"migration,omitempty"`

	// EnableMirWatchdog can be used to disable the manual intervention required watchdog for the cluster.
	// This defaults to true and is not publicly documented.
	EnableMirWatchdog *bool `json:"-" annotation:"enableMirWatchdog"`
}

// ClusterAssimilationSpec defines the specification for a CouchbaseCluster assimilation of an unmanaged
// cluster to a managed Kubernetes cluster
type ClusterAssimilationSpec struct {
	// UnmanagedClusterHost is a host of the unmanaged Couchbase cluster to be migrated. This is the host
	// that the operator will connect to to start the migration process.
	// +kubebuilder:validation:Pattern=`^((([a-zA-Z0-9](-?[a-zA-Z0-9])*)\.)+[a-zA-Z]{2,})|((25[0-5]|2[0-4][0-9]|[0-1]?[0-9]{1,2})\.){3}(25[0-5]|2[0-4][0-9]|[0-1]?[0-9]{1,2})|(([0-9A-Fa-f]{1,4}:){1,7}[0-9A-Fa-f]{1,4})$`
	UnmanagedClusterHost string `json:"unmanagedClusterHost,omitempty"`

	// NumUnmanagedNodes is the number of nodes the operator will leave in the cluster unmigrated.
	// This is useful for controlling how much of the cluster to migrate over at a time. If not specified
	// the operator will migrate all nodes.
	// e.g. if the unmanaged cluster has 10 nodes and NumUnmanagedNodes is set to 2, then the operator will
	// migrate 8 nodes to Kubernetes and leave 2 nodes.
	NumUnmanagedNodes int `json:"numUnmanagedNodes,omitempty"`

	// StabilizationPeriod is the time the operator will wait after a migration before starting the next migration.
	// If not specified the operator will start the next migration immediately.
	StabilizationPeriod *metav1.Duration `json:"stabilizationPeriod,omitempty"`

	// MaxConcurrentMigrations is the maximum number of nodes migrations the operator will run concurrently.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	MaxConcurrentMigrations int `json:"maxConcurrentMigrations,omitempty"`

	// MigrationOrderOverride defines the strategy for migration order. If not set then the operator will choose nodes at random.
	// +optional
	MigrationOrderOverride *MigrationOrderOverrideSpec `json:"migrationOrderOverride,omitempty"`
}

// MigrationOrderOverrideStrategy defines the strategy for overriding the default migration order.
// +kubebuilder:validation:Enum=ByServerGroup;ByServerClass;ByNode
type MigrationOrderOverrideStrategy string

const (
	// ByServerGroup indicates that the migration order is based on server groups.
	ByServerGroup MigrationOrderOverrideStrategy = "ByServerGroup"

	// ByServerClass indicates that the migration order is based on server classes.
	ByServerClass MigrationOrderOverrideStrategy = "ByServerClass"

	// ByNode indicates that the migration order is based on individual nodes.
	ByNode MigrationOrderOverrideStrategy = "ByNode"
)

// MigrationOrderOverrideSpec defines the specification for overriding the default migration order.
// It allows specifying the order of server groups, server classes, or individual nodes for migration.
type MigrationOrderOverrideSpec struct {
	// MigrationOrderOverrideStrategy defines the strategy for migration order. When not set, the operator will choose nodes at random.
	// When ByServerGroup is set, the operator will migrate nodes in the order of the server groups defined in spec.migration.migrationOrderOverride.serverGroupOrder.
	// If spec.migration.migrationOrderOverride.serverGroupOrder is not set, the operator will migrate the server groups in alphabetical order.
	// When ByServerClass is set, the operator will migrate nodes in the order of the server classes defined in spec.migration.migrationOrderOverride.serverClassOrder.
	// If spec.migration.migrationOrderOverride.serverClassOrder is not set, the operator will migrate the server classes in the order of the server classes defined in spec.servers.
	// When ByNode is set, the operator will migrate nodes in the order of the nodes defined in spec.migration.migrationOrderOverride.nodeOrder.
	// If spec.migration.migrationOrderOverride.nodeOrder is not set, the operator will migrate the nodes in alphabetical order.
	// +optional
	MigrationOrderOverrideStrategy MigrationOrderOverrideStrategy `json:"migrationOrderOverrideStrategy,omitempty"`

	// ServerGroupOrder defines the order of server groups for migration.
	// +optional
	ServerGroupOrder []string `json:"serverGroupOrder,omitempty"`

	// ServerClassOrder defines the order of server classes for migration.
	// +optional
	ServerClassOrder []string `json:"serverClassOrder,omitempty"`

	// NodeOrder defines the order of nodes for migration.
	// +optional
	NodeOrder []string `json:"nodeOrder,omitempty"`
}

type PersistentVolumeClaimTemplate struct {
	ObjectMeta NamedObjectMeta              `json:"metadata"`
	Spec       v1.PersistentVolumeClaimSpec `json:"spec"`
}

type Backup struct {
	// Managed defines whether backups are managed by us or the clients.
	Managed bool `json:"managed,omitempty"`

	// The Backup Image to run on backup pods.
	// +kubebuilder:default="couchbase/operator-backup:1.4.1"
	Image string `json:"image"`

	// The Service Account to run backup (and restore) pods under.
	// Without this backup pods will not be able to update status.
	// +kubebuilder:default="couchbase-backup"
	ServiceAccount string `json:"serviceAccountName,omitempty"`

	// NodeSelector defines which nodes to constrain the pods that
	// run any backup and restore operations to.
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Labels defines additional labels to appear on the backup/restore pods.
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations defines additional annotations to appear on the backup/restore pods.
	Annotations map[string]string `json:"annotations,omitempty"`

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

	// Deprecated: by CouchbaseBackup.spec.objectStore.secret
	// S3Secret contains the key region and optionally access-key-id and secret-access-key for operating backups in S3.
	// This field must be popluated when the `spec.s3bucket` field is specified
	// for a backup or restore resource.
	S3Secret string `json:"s3Secret,omitempty"`

	// Deprecated: by CouchbaseBackup.spec.objectStore.Endpoint
	// ObjectEndpoint contains the configuration for connecting to a custom S3 compliant object store.
	ObjectEndpoint *ObjectEndpoint `json:"objectEndpoint,omitempty"`

	// Deprecated: by CouchbaseBackup.spec.objectStore.useIAM
	// UseIAMRole enables backup to fetch EC2 instance metadata.
	// This allows the AWS SDK to use the EC2's IAM Role for S3 access.
	// UseIAMRole will ignore credentials in s3Secret.
	UseIAMRole bool `json:"useIAMRole,omitempty"`
}

type ObjectEndpoint struct {
	// The name of the secret, in this namespace, that contains the CA certificate for verification of a TLS endpoint
	// The secret must have the key with the name "tls.crt"
	CertSecret string `json:"secret,omitempty"`

	// The host/address of the custom object endpoint.
	URL string `json:"url,omitempty"`

	// UseVirtualPath will force the AWS SDK to use the new virtual style paths
	// which are often required by S3 compatible object stores.
	UseVirtualPath bool `json:"useVirtualPath,omitempty"`
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
	// BindSecret is the name of a Kubernetes secret to use containing password for LDAP user binding.
	// The bindSecret must have a key with the name "password" and a value which corresponds to the
	// password of the binding LDAP user.
	BindSecret string `json:"bindSecret"`

	// TLSSecret is the name of a Kubernetes secret to use explcitly for LDAP ca cert.
	// If TLSSecret is not provided, certificates found in `couchbaseclusters.spec.networking.tls.rootCAs`
	// will be used instead.
	// If provided, the secret must contain the ca to be used under the name "ca.crt".
	TLSSecret string `json:"tlsSecret,omitempty"`

	// AuthenticationEnabled allows users who attempt to access Couchbase Server without having been
	// added as local users to be authenticated against the specified LDAP Host(s).
	// +optional
	// +kubebuilder:default=true
	AuthenticationEnabled bool `json:"authenticationEnabled"`

	// AuthorizationEnabled allows authenticated LDAP users to be authorized with RBAC roles granted to
	// any Couchbase Server group associated with the user.
	AuthorizationEnabled bool `json:"authorizationEnabled,omitempty"`

	// List of LDAP hosts to provide authentication-support for Couchbase Server.
	// Host name must be a valid IP address or DNS Name e.g openldap.default.svc, 10.0.92.147.
	// +kubebuilder:validation:MinItems=1
	Hosts []string `json:"hosts"`

	// LDAP port.
	// This is typically 389 for LDAP, and 636 for LDAPS.
	// +kubebuilder:default=389
	Port int `json:"port"`

	// Encryption determines how the connection with the LDAP server should be encrypted.
	// Encryption may set as either StartTLSExtension, TLS, or false.
	// When set to "false" then no verification of the LDAP hostname is performed.
	// When Encryption is StartTLSExtension, or TLS is set then the default behavior is to
	// use the certificate already loaded into the Couchbase Cluster for certificate validation,
	// otherwise `ldap.tlsSecret` may be set to override The Couchbase certificate.
	Encryption LDAPEncryption `json:"encryption,omitempty"`

	// Whether server certificate validation be enabled.
	EnableCertValidation bool `json:"serverCertValidation,omitempty"`

	// DEPRECATED - Field is ignored, use tlsSecret.
	// CA Certificate in PEM format to be used in LDAP server certificate validation.
	// This cert is the string form of the secret provided to `spec.tls.tlsSecret`.
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

	// Sets middlebox compatibility mode for LDAP. This option is only available on
	// Couchbase Server 7.6.0+.
	// +optional
	// +kubebuilder:default=true
	MiddleboxCompMode bool `json:"middleboxCompMode"`
}

type LDAPUserDNMapping struct {
	// This field specifies list of templates to use for providing username to DN mapping.
	// The template may contain a placeholder specified as `%u` to represent the Couchbase
	// user who is attempting to gain access.
	Template string `json:"template,omitempty"`

	// Query is the LDAP query to run to map from Couchbase user to LDAP distinguished name.
	Query string `json:"query,omitempty"`
}

const (
	// AdminSecretUsernameKey is the secret key to save an admin username under.
	AdminSecretUsernameKey = "username"

	// AdminSecretPasswordKey is the secret key to save an admin password under.
	AdminSecretPasswordKey = "password"
)

type CouchbaseClusterSecuritySpec struct {
	// AdminSecret is the name of a Kubernetes secret to use for administrator authentication.
	// The admin secret must contain the keys "username" and "password".  The password data
	// must be at least 6 characters in length, and not contain the any of the characters
	// `()<>,;:\"/[]?={}`.
	AdminSecret string `json:"adminSecret"`

	// RBAC is the options provided for enabling and selecting RBAC User resources to manage.
	RBAC RBAC `json:"rbac,omitempty"`

	// LDAP provides settings to authenticate and authorize LDAP users with Couchbase Server.
	// When specified, the Operator keeps these settings in sync with Cocuhbase Server's
	// LDAP configuration. Leave empty to manually manage LDAP configuration.
	LDAP *CouchbaseClusterLDAPSpec `json:"ldap,omitempty"`

	// UISessionTimeout sets how long, in minutes, before a user is declared inactive
	// and signed out from the Couchbase Server UI.
	// 0 represents no time out.
	// +kubebuilder:default=0
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=16666
	UISessionTimeoutMinutes uint `json:"uiSessionTimeout,omitempty"`

	// PodSecurityContext allows the configuration of the security context for all
	// Couchbase server pods.  When using persistent volumes you may need to set
	// the fsGroup field in order to write to the volume.  For non-root clusters
	// you must also set runAsUser to 1000, corresponding to the Couchbase user
	// in official container images.  More info:
	// https://kubernetes.io/docs/tasks/configure-pod-container/security-context/
	PodSecurityContext *v1.PodSecurityContext `json:"podSecurityContext,omitempty"`

	// SecurityContext defines the security options the container should be run with.
	// If set, the fields of SecurityContext override the equivalent fields of PodSecurityContext.
	// Use securityContext.allowPrivilegeEscalation field to grant more privileges than its parent process.
	// More info: https://kubernetes.io/docs/tasks/configure-pod-container/security-context/
	SecurityContext *v1.SecurityContext `json:"securityContext,omitempty"`

	// EncryptionAtRest configures encryption at rest for the cluster.
	// This field is only supported on Couchbase Server 8.0.0+.
	EncryptionAtRest *EncryptionAtRestSpec `json:"encryptionAtRest,omitempty"`

	// PasswordPolicy specifies a series of character-related requirements that
	// must be met by all passwords whose definition occurs subsequent to the
	// establishing of the policy. If this is updated, previously defined passwords continue to be
	// valid, even if they do not meet the requirements specified in the new policy.
	PasswordPolicy *PasswordPolicySpec `json:"passwordPolicy,omitempty"`
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
// +kubebuilder:validation:Enum=IPv4;IPv6;IPv4Priority;IPv6Priority;IPv6Only;IPv4Only
type AddressFamily string

const (
	// Deprecated - use IPv4Only instead
	IPv4 AddressFamily = "IPv4"

	// Deprecated - use IPv6Only instead
	IPv6 AddressFamily = "IPv6"

	IPv4Priority AddressFamily = "IPv4Priority"

	IPv6Priority AddressFamily = "IPv6Priority"

	IPv4Only AddressFamily = "IPv4Only"

	IPv6Only AddressFamily = "IPv6Only"
)

type CloudNativeGatewayTLS struct {
	// ServerSecretName specifies the secret name, in the same namespace as the cluster,
	// that contains Cloud Native Gateway gRPC server TLS data.
	// The secret is expected to contain "tls.crt" and
	// "tls.key" as per the kubernetes.io/tls secret type.
	ServerSecretName string `json:"serverSecretName,omitempty"`
}

// CloudNativeGatewayLogLevel controls the verbosity of indexer logs.
// +kubebuilder:validation:Enum=fatal;panic;dpanic;error;warn;info;debug
type CloudNativeGatewayLogLevel string

const (
	CloudNativeGatewayLogLevelFatal  CloudNativeGatewayLogLevel = "fatal"
	CloudNativeGatewayLogLevelPanic  CloudNativeGatewayLogLevel = "panic"
	CloudNativeGatewayLogLevelDPanic CloudNativeGatewayLogLevel = "dpanic"
	CloudNativeGatewayLogLevelError  CloudNativeGatewayLogLevel = "error"
	CloudNativeGatewayLogLevelWarn   CloudNativeGatewayLogLevel = "warn"
	CloudNativeGatewayLogLevelInfo   CloudNativeGatewayLogLevel = "info"
	CloudNativeGatewayLogLevelDebug  CloudNativeGatewayLogLevel = "debug"
)

type CloudNativeGatewayDataAPIProxyService string

type CloudNativeGatewayDataAPIProxyServiceList []CloudNativeGatewayDataAPIProxyService

const (
	CloudNativeGatewayDataAPIProxyServiceMgmt      CloudNativeGatewayDataAPIProxyService = "mgmt"
	CloudNativeGatewayDataAPIProxyServiceQuery     CloudNativeGatewayDataAPIProxyService = "query"
	CloudNativeGatewayDataAPIProxyServiceSearch    CloudNativeGatewayDataAPIProxyService = "search"
	CloudNativeGatewayDataAPIProxyServiceAnalytics CloudNativeGatewayDataAPIProxyService = "analytics"
)

type CloudNativeGatewayDataAPI struct {
	// DEVELOPER PREVIEW - This feature is in developer preview.
	// Enabled defines whether the data api will be available through cloud native gateway.
	// This defaults to false. If set to true, the data api will be available on port 18008 of the cloud native gateway service.
	Enabled bool `json:"-" annotation:"enabled"`

	// DEVELOPER PREVIEW - This feature is in developer preview.
	// ProxyServices is a list of services that the Cloud Native Gateway can proxy via the data api.
	// If this field is used, it must be one of "mgmt", "query", "search" or "analytics". By default, none of these services will
	// be available through the data api if it has been enabled.
	ProxyServices *CloudNativeGatewayDataAPIProxyServiceList `json:"-" annotation:"proxyServices"`
}

type CloudNativeGateway struct {
	// Image is the Cloud Native Gateway image to be used to run the sidecar container.
	// No validation is carried out as this can be any arbitrary repo and tag.
	Image string `json:"image"`

	// TLS defines the TLS configuration for the Cloud Native Gateway server including
	// server and client certificate configuration, and TLS security policies.
	// If no TLS config are explicitly provided, the operator generates/manages self-signed certs/keys
	// and creates a k8s secret named `couchbase-cloud-native-gateway-self-signed-secret-<cluster-name>`
	// unique to a Couchbase cluster, which is volume mounted to the cb k8s pod.
	// This action could be overidden at the outset or later, by using the below
	// TLS config or generating the secret of same name as
	// `couchbase-cloud-native-gateway-self-signed-secret-<cluster-name>` with certificates
	// conforming to the keys of well-known type "kubernetes.io/tls" with "tls.crt" and "tls.key".
	// N.B. The secret is on per cluster basis so it's advised to use the unique cluster name else
	// would be ignored.
	TLS *CloudNativeGatewayTLS `json:"tls,omitempty"`

	OTLP *CloudNativeGatewayOTLP `json:"-" annotation:"otlp"`

	// TerminationGracePeriodSeconds specifies the grace period for the container to
	// terminate. Defaults to 75 seconds.
	// +kubebuilder:default=75
	TerminationGracePeriodSeconds int64 `json:"terminationGracePeriodSeconds,omitempty"`

	// DEVELOPER PREVIEW - This feature is in developer preview.
	// LogLevel controls the verbosity of cloud native logs.  This field must be one of
	// "fatal", "panic", "dpanic", "error", "warn", "info", "debug" defaulting to "info".
	// +kubebuilder:default="info"
	LogLevel CloudNativeGatewayLogLevel `json:"logLevel"`

	// DEVELOPER PREVIEW - This feature is in developer preview.
	// The DataAPI settings control whether the Cloud Native Gateway can be used to access the data api. Adding or changing
	// this configuration on an existing cluster with cloud native gateway will add the rescheduling annotation to each of the pods, which
	// will trigger a restart depending on the configured UpgradeProcess.
	DataAPI *CloudNativeGatewayDataAPI `json:"-" annotation:"dataAPI"`

	// ServiceTemplate can be used to provice a template used by the Operator
	// when creating the CNG service. This allows services to be annotated, the
	// service type defined and any other options that Kubernetes provides. The Operator
	// reserves the right to modify or replace any field.  More info:
	// https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#service-v1-core
	ServiceTemplate *ServiceTemplateSpec `json:"serviceTemplate,omitempty"`
}

type CloudNativeGatewayOTLP struct {
	Endpoint string `json:"-" annotation:"endpoint"`
}

type CouchbaseClusterNetworkingSpec struct {
	// AddressFamily allows the manual selection of the address family to use.
	// Setting this field to either IPv4Only or IPv6Only will exclusively use that address family.
	// Setting this field to IPv4Priority or IPv6Priority will allow dual stack networking with
	// the given address family being prioritised.
	// When this field is not set, Couchbase server will default to using IPv4
	// for internal communication and also support IPv6 on dual stack systems.
	// This is only supported in Couchbase Server 7.0.2+.
	AddressFamily *AddressFamily `json:"addressFamily,omitempty"`

	// ExposeAdminConsole creates a service referencing the admin console.
	// The service is configured by the adminConsoleServiceTemplate field.
	ExposeAdminConsole bool `json:"exposeAdminConsole,omitempty"`

	// DEPRECATED - not required by Couchbase Server.
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
	// https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#service-v1-core
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
	// https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#service-v1-core
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
	// for external-DDNS controllers to propagate addresses. Pods will not be marked
	// as ready until external addresses are reachable which at the earliest will be
	// after this delay has elapsed.
	// +kubebuilder:default="2m"
	WaitForAddressReachableDelay *metav1.Duration `json:"waitForAddressReachableDelay,omitempty"`

	// WaitForAddressReachable is used to set the timeout between when polling of
	// external addresses is started, and when it is deemed a failure.  Polling of
	// DNS name availability inherently dangerous due to negative caching, so prefer
	// the use of an initial `waitForAddressReachableDelay` to allow propagation.
	// Once the timeout has elapsed, pods without a reachable alternate address that have not
	// been balanced into the cluster will be removed. This field will not effect
	// pods that have already been balanced into the cluster and those will continue
	// to have their alternate address validated during each reconciliation loop until
	// it can be reached.
	// +kubebuilder:default="10m"
	WaitForAddressReachable *metav1.Duration `json:"waitForAddressReachable,omitempty"`

	// AllowExternallyUnreachablePods is used to allow new pods to be rebalanced into the cluster
	// regardless of whether the external DNS is reachable or not. If this is set to true,
	// pods for which the DNS has not yet propagated will be balanced into the cluster and marked
	// as ready once the WaitForAddressReachableDelay has elapsed.
	// The external DNS will continue to be checked for reachability during each reconciliation loop
	// and the couchbase node will not have it's alternate addresses updated until it is reachable.
	// +kubebuilder:default=false
	AllowExternallyUnreachablePods *bool `json:"allowExternallyUnreachablePods,omitempty"`

	// CloudNativeGateway is used to provision a gRPC gateway proxying a Couchbase
	// cluster.
	CloudNativeGateway *CloudNativeGateway `json:"cloudNativeGateway,omitempty" annotation:"cloudNativeGateway"`

	// ImprovedHostNetwork is used to set the alternate address of the pod to the node name
	// +kubebulder:default=false
	ImprovedHostNetwork bool `json:"improvedHostNetwork,omitempty"`

	// InitPodsWithNodeHostname is used to set the hostname of the pod to the node name
	// +kubebuilder:default=false
	InitPodsWithNodeHostname bool `json:"initPodsWithNodeHostname,omitempty"`
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
// +kubebuilder:validation:XValidation:rule="!has(self.overheadPercent) || !has(self.overheadMemory)",message="at most one of spec.autoResourceAllocation.overheadPercent or spec.autoResourceAllocation.overheadMemory can be set"
type AutoResourceAllocation struct {
	// Enabled defines whether auto-resource allocation is enabled.
	Enabled bool `json:"enabled,omitempty"`

	// OverheadPercent defines the amount of memory above that required for individual
	// services on a pod.  For Couchbase Server this should be approximately 25%.
	// +kubebuilder:validation:Minimum=0
	OverheadPercent *int `json:"overheadPercent,omitempty"`

	// OverheadMemory defines a static amount of memory above that required for
	// individual services on a pod. This will override `overheadPercent` if both
	// are specified.
	// +kubebuilder:validation:Type=string
	OverheadMemory *resource.Quantity `json:"overheadMemory,omitempty"`

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

	// QueryServiceMemQuota is used when the spec.autoResourceAllocation feature is enabled,
	// and is used to define the amount of memory reserved by the query service for use with
	// Kubernetes resource scheduling. More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// In CB Server 7.6.0+ QueryServiceMemQuota also sets a soft memory limit for every Query node in the cluster.
	// The garbage collector tries to keep below this target. It is not a hard, absolute limit, and memory
	// usage may exceed this value.
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

	// Analytics allows the analytics service to be configured.
	Analytics *CouchbaseClusterAnalyticSettings `json:"analytics,omitempty"`

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
	// will allow before not allowing any more.  This field must be between 1-3 for server versions prior to 7.1.0
	// default is 1.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
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
	// This field is ignored in server versions 7.1+ as it has been removed from the Couchbase API
	AutoFailoverServerGroup bool `json:"autoFailoverServerGroup,omitempty"`

	// AutoCompaction allows the configuration of auto-compaction, including on what
	// conditions disk space is reclaimed and when it is allowed to run. Cluster level settings
	// will be used as the default when creating new buckets and any changes to the settings will be applied
	// to all existing buckets that have not had their auto-compaction settings individually modified.
	// +kubebuilder:default="x-couchbase-object"
	AutoCompaction *AutoCompaction `json:"autoCompaction,omitempty" annotation:"autoCompaction"`

	// AllowFailoverEphemeralNoReplicas allows failover of ephemeral buckets with no replicas.
	// This is only supported on Couchbase Server 8.0+.
	AllowFailoverEphemeralNoReplicas *bool `json:"allowFailoverEphemeralNoReplicas,omitempty"`

	// AppTelemetry allows the configuration of application telemetry.
	// This is only supported on Couchbase Server 8.0+.
	AppTelemetry *CouchbaseClusterAppTelemetrySettings `json:"appTelemetry,omitempty"`
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

	// NumberOfReplica specifies number of secondary index replicas to be created
	// by the Index Service whenever CREATE INDEX is invoked, which ensures
	// high availability and high performance.
	// Note, if nodes and num_replica are both specified in the WITH clause,
	// the specified number of nodes must be one greater than num_replica
	// This field must be between 0 and 16, defaulting to 0, which means no index replicas to be created by default.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=16
	// +kubebuilder:default=0
	NumberOfReplica int `json:"numReplica,omitempty"`

	// RedistributeIndexes when true, Couchbase Server redistributes indexes
	// when rebalance occurs, in order to optimize performance.
	// If false (the default), such redistribution does not occur.
	// +kubebuilder:default=false
	RedistributeIndexes bool `json:"redistributeIndexes,omitempty"`

	// EnableShardAffinity when false Index Servers rebuild any index that
	// are newly assigned to them during a rebalance. When set to true,
	// Couchbase Server moves a reassigned index’s files between Index Servers.
	// This field is only supported on CB versions 7.6.0+.
	// +kubebuilder:default=false
	EnableShardAffinity bool `json:"enableShardAffinity,omitempty"`

	// EnablePageBloomFilter gives Couchbase Server guidance whether
	// bloom filters should be used when item lookups occur. These help to
	// indicate during a lookup that an item is not on disk, and therefore
	// prevent unnecessary on-disk searches.
	// This field is only supported on CB versions 7.1.0+.
	// +kubebuilder:default=false
	EnablePageBloomFilter bool `json:"enablePageBloomFilter,omitempty"`

	// DeferBuild allows the indexer to defer building indexes.
	// This field is only supported on CB versions 8.0.0+.
	// +kubebuilder:default=false
	DeferBuild bool `json:"deferBuild,omitempty"`
}

// QueryLogLevel controls the verbosity of the query service logs.
// +kubebuilder:validation:Enum=debug;trace;info;warn;error;severe;none
type QueryLogLevel string

const (
	QueryLogLevelDebug  QueryLogLevel = "debug"
	QueryLogLevelTrace  QueryLogLevel = "trace"
	QueryLogLevelInfo   QueryLogLevel = "info"
	QueryLogLevelWarn   QueryLogLevel = "warn"
	QueryLogLevelError  QueryLogLevel = "error"
	QueryLogLevelSevere QueryLogLevel = "severe"
	QueryLogLevelNone   QueryLogLevel = "none"
)

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

	// PipelineBatch controls the number of items execution operators can batch for
	// Fetch from the KV. Defaults to 16.
	// +kubebuilder:default=16
	PipelineBatch int32 `json:"pipelineBatch"`

	// PipelineCap controls the maximum number of items each execution
	// operator can buffer between various operators. Defaults to 512.
	// +kubebuilder:default=512
	PipelineCap int32 `json:"pipelineCap"`

	// ScapCan sets the maximum buffered channel size between the indexer client
	// and the query service for index scans.
	// Defaults to 512.
	// +kubebuilder:default=512
	ScanCap int32 `json:"scanCap"`

	// Timeout is the maximum time to spend on the request before timing out.
	// If this field is not set then there will be no timeout.
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// PreparedLimit is the maximum number of prepared statements in the cache.
	// When this cache reaches the limit, the least recently used prepared
	// statements will be discarded as new prepared statements are created.
	// +kubebuilder:default=16384
	PreparedLimit int32 `json:"preparedLimit"`

	// CompletedLimit sets the number of requests to be logged in the completed
	// requests catalog. As new completed requests are added, old ones are removed.
	// +kubebuilder:default=4000
	CompletedLimit int32 `json:"completedLimit"`

	// DEPRECATED - by spec.cluster.query.completedThreshold. Set completedThreshold to "-1" to disable request tracking.
	// CompletedTrackingEnabled allows completed requests to be tracked in the requests
	// catalog.
	CompletedTrackingEnabled *bool `json:"completedTrackingEnabled,omitempty"`

	// DEPRECATED - by spec.cluster.query.completedThreshold. Set completedThreshold to "0" to log all requests.
	// CompletedTrackingAllRequests allows all requests to be tracked regardless of their
	// time. This field requires `completedTrackingEnabled` to be true.
	CompletedTrackingAllRequests *bool `json:"completedTrackingAllRequests,omitempty"`

	// DEPRECATED - by spec.cluster.query.completedThreshold.
	// CompletedTrackingThreshold is a trigger for queries to be logged in the completed
	// requests catalog. All completed queries lasting longer than this threshold
	// are logged in the completed requests catalog. This field requires `completedTrackingEnabled`
	// to be set to true and `completedTrackingAllRequests` to be false to have any effect.
	CompletedTrackingThreshold *metav1.Duration `json:"completedTrackingThreshold,omitempty"`

	// CompletedThreshold sets the minimum request duration after which requests are added to the completed
	// requests catalog. This field must either be a duration up to 2147483648s, "0", or "-1".
	// Setting this field to "0" will log all requests. Setting this field to "-1" will disable request logging.
	// This field defaults to 1s.
	// +kubebuilder:default="1s"
	CompletedThreshold *metav1.Duration `json:"completedThreshold,omitempty"`

	// LogLevel controls the verbosity of query logs. This field must be one of
	// "debug", "trace", "info", "warn", "error", "severe", or "none", defaulting to "info".
	// +kubebuilder:default="info"
	LogLevel QueryLogLevel `json:"logLevel,omitempty"`

	// MaxParallelism specifies the maximum parallelism for queries on all Query nodes in the cluster.
	// If the value is zero, negative, or larger than the number of allowed cored the maximum parallelism
	// is restricted to the number of allowed cores.
	// Defaults to 1.
	// +kubebuilder:default=1
	MaxParallelism int32 `json:"maxParallelism"`

	// TxTimeout is the maximum time to spend on a transaction before timing out. This setting
	// only applies to requests containing the BEGIN TRANSACTION statement, or to requests where
	// the tximplicit parameter is set. For all other requests, it is ignored.
	// Defaults to 0ms (no timeout).
	// +kubebuilder:default="0ms"
	TxTimeout *metav1.Duration `json:"txTimeout,omitempty"`

	// MemoryQuota specifies the maximum amount of memory a request may use on any Query node in the cluster.
	// This parameter enforces a ceiling on the memory used for the tracked documents required for processing
	// a request. It does not take into account any other memory that might be used to process a request,
	// such as the stack, the operators, or some intermediate values.
	// Defaults to 0.
	// +kubebuilder:default="0"
	// +kubebuilder:validation:Type=string
	MemoryQuota *resource.Quantity `json:"memoryQuota,omitempty"`

	// CBOEnabled specifies whether the cost-based optimizer is enabled.
	// Defaults to true.
	// +kubebuilder:default=true
	CBOEnabled bool `json:"cboEnabled"`

	// CleanupClientAttemptsEnabled specifies whether the Query service preferentially aims to clean up just
	// transactions that it has created, leaving transactions for the distributed cleanup process only
	// when it is forced to.
	// Defaults to true.
	// +kubebuilder:default=true
	CleanupClientAttemptsEnabled bool `json:"cleanupClientAttemptsEnabled"`

	// CleanupLostAttemptsEnabled specifies the Query service takes part in the distributed cleanup
	// process, and cleans up expired transactions created by any client.
	// Defaults to true.
	// +kubebuilder:default=true
	CleanupLostAttemptsEnabled bool `json:"cleanupLostAttemptsEnabled"`

	// CleanupWindow specifies how frequently the Query service checks its subset of active
	// transaction records for cleanup.
	// Defaults to 60s
	// +kubebuilder:default="60s"
	CleanupWindow *metav1.Duration `json:"cleanupWindow"`

	// NumActiveTransactionRecords specifies the total number of active transaction records for
	// all Query nodes in the cluster.
	// Default to 1024 and has a minimum of 1.
	// +kubebuilder:default=1024
	// +kubebuilder:validation:Minimum=1
	NumActiveTransactionRecords int32 `json:"numActiveTransactionRecords"`

	// UseReplica specifies whether a query can fetch data from a replica vBucket if active vBuckets
	// are inaccessible. If set to true then read from replica is enabled for all queries, but can
	// be disabled at request level. If set to false read from replica is disabled for all queries
	// and cannot be overridden at request level. If this field is unset then it is enabled/disabled
	// at the request level.
	// This field is only supported on CB versions 7.6.0+.
	UseReplica *bool `json:"useReplica,omitempty"`

	// NodeQuotaValPercent sets the  percentage of the `useReplica` that is dedicated to tracked
	// value content memory across all active requests for every Query node in the cluster.
	// This field is only supported on CB versions 7.6.0+.
	// Defaults to 67.
	// +kubebuilder:default=67
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	NodeQuotaValPercent int32 `json:"nodeQuotaValPercent"`

	// NumCpus is the number of CPUs the Query service can use on any Query node in the cluster.
	// When set to 0 (the default), the Query service can use all available CPUs, up to the limits described below.
	// The number of CPUs can never be greater than the number of logical CPUs.
	// In Community Edition, the number of allowed CPUs cannot be greater than 4.
	// In Enterprise Edition, there is no limit to the number of allowed CPUs.
	// This field is only supported on CB versions 7.6.0+.
	// NOTE: This change requires a restart of the Query service to take effect which can be done by rescheduling
	// nodes that are running the query service.
	// Defaults to 0
	// +kubebuilder:default=0
	// +kubebuilder:validation:Minimum=0
	NumCpus int32 `json:"numCpus"`

	// CompletedMaxPlanSize limits the size of query execution plans that can be logged in the
	// completed requests catalog. Queries with plans larger than this are not logged.
	// This field is only supported on CB versions 7.6.0+.
	// Defaults to 262144, maximum value is 20840448, and minimum value is 0.
	// +kubebuilder:default="262144"
	// +kubebuilder:validation:Type=string
	CompletedMaxPlanSize *resource.Quantity `json:"completedMaxPlanSize"`

	// CompletedStreamSize controls how much data about completed N1QL queries is saved to disk
	// for analysis. When set to a value greater than 0 (measured in MiB), Couchbase saves
	// information about completed queries to GZIP-compressed files with prefix local_request_log.
	// This field is only supported on CB versions 8.0.0+.
	// Defaults to 0 (disabled), minimum value is 0.
	// +kubebuilder:validation:Minimum=0
	CompletedStreamSize *int32 `json:"completedStreamSize,omitempty"`
}

// CouchbaseClusterAppTelemetrySettings allows application telemetry service tweaks.
type CouchbaseClusterAppTelemetrySettings struct {
	// Enabled controls whether application telemetry is enabled.
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`

	// MaxScrapeClientsPerNode sets the maximum number of scrape clients per node.
	// Must be between 1 and 1024.
	// +kubebuilder:default=1024
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1024
	MaxScrapeClientsPerNode int `json:"maxScrapeClientsPerNode,omitempty"`

	// ScrapeIntervalSeconds sets the scrape interval in seconds.
	// Must be between 60 and 600.
	// +kubebuilder:default=60
	// +kubebuilder:validation:Minimum=60
	// +kubebuilder:validation:Maximum=600
	ScrapeIntervalSeconds int `json:"scrapeIntervalSeconds,omitempty"`
}

func (q *CouchbaseClusterQuerySettings) UnmarshalJSON(data []byte) error {
	type Alias CouchbaseClusterQuerySettings

	aux := &struct {
		*Alias
		CompletedThreshold string `json:"completedThreshold,omitempty"`
	}{
		Alias: (*Alias)(q),
	}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	duration, err := unmarshalDurationWithNegativeOverride(aux.CompletedThreshold, -1*time.Millisecond, time.Second)
	if err != nil {
		return err
	}

	q.CompletedThreshold = duration

	return nil
}

func (q *CouchbaseClusterQuerySettings) MarshalJSON() ([]byte, error) {
	type Alias CouchbaseClusterQuerySettings

	aux := &struct {
		*Alias
		CompletedThreshold string `json:"completedThreshold,omitempty"`
	}{
		Alias: (*Alias)(q),
	}

	aux.CompletedThreshold = marshalDurationWithNegativeOverride(q.CompletedThreshold, -1*time.Millisecond)

	json, err := json.Marshal(aux)
	if err != nil {
		return nil, err
	}

	return json, nil
}

// CouchbaseClusterAnalyticSettings allow analytic service tweaks.
type CouchbaseClusterAnalyticSettings struct {
	// NumReplicas specifies the number of replicas for Analytics.
	NumReplicas *int `json:"numReplicas,omitempty"`
}

// CouchbaseClusterDataSettings allows data service tweaks.
type CouchbaseClusterDataSettings struct {
	// ReaderThreads allows the number of threads used by the data service,
	// per pod, to be altered. This can either be fixed to a number between 1 and 64, or to
	// one of default(pre 8.0.0) / balanced(post 8.0.0) or disk_io_optimized.
	// For server versions below 7.1.0, the minimum fixed value is 4.
	// Increasing the fixed value should only be done where there are sufficient CPU resources.
	// When using the default/balanced and disk_io_optimized options, CB server will automatically determine the number of threads to use.
	// If not specified, this defaults to default/balanced.
	// +kubebuilder:validation:XIntOrString
	ReaderThreads *intstr.IntOrString `json:"readerThreads,omitempty"`

	// WriterThreads allows the number of threads used by the data service,
	// per pod, to be altered. This can either be fixed to a number between 1 and 64, or to
	// one of "default" (pre 8.0.0) / "balanced" (post 8.0.0) or "disk_io_optimized".
	// For server versions below 7.1.0, the minimum fixed value is 4.
	// Increasing the fixed value should only be done where there are sufficient CPU resources.
	// When using the default/balanced and disk_io_optimized options, CB server will automatically determine the number of threads to use.
	// If not specified, this defaults to default/balanced.
	// +kubebuilder:validation:XIntOrString
	WriterThreads *intstr.IntOrString `json:"writerThreads,omitempty"`

	// NonIOThreads allows the number of threads used by the data service,
	// per pod, to be altered.  This indicates the number of threads that are
	// to be used in the NonIO thread pool to run in memory tasks.
	// This value must be between 1 and 64 threads and is only supported on CB versions 7.1.0+.
	// and should only be increased where there are sufficient CPU resources
	// allocated for their use. If not specified, this defaults to the
	// default value set by Couchbase Server.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=64
	NonIOThreads *int `json:"nonIOThreads,omitempty"`

	// AuxIOThreads allows the number of threads used by the data service,
	// per pod, to be altered.  This indicates the number of threads that are
	// to be used in the AuxIO thread pool to run auxiliary I/O tasks.
	// This value must be between 1 and 64 threads and is only supported on CB versions 7.1.0+.
	// and should only be increased where there are sufficient CPU resources
	// allocated for their use. If not specified, this defaults to the
	// default value set by Couchbase Server.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=64
	AuxIOThreads *int `json:"auxIOThreads,omitempty"`

	// MinReplicasCount allows the minimum number of replicas required for
	// buckets to be set. New buckets cannot be created with less than this minimum.
	// This field must be between 0 and 3, defaulting to 0.
	// +kubebuilder:default=0
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=3
	MinReplicasCount int `json:"minReplicasCount,omitempty"`

	// DiskUsageLimit allows a threshold to be set to limit the amount of disk space that can be used by buckets.
	// If the disk usage limit is reached, Couchbase server will prevent data writes to buckets.
	// Setting this value reserves disk space for recovery operations like performing rebalances to add a new node.
	// This field is only supported on Couchbase server versions 8.0 and later.
	DiskUsageLimit *DiskUsageLimit `json:"diskUsageLimit,omitempty"`

	// TCPKeepAliveIdle is the number of seconds before the first TCP probe is sent.
	// This field is only supported on Couchbase server versions 8.0.0 and later.
	TCPKeepAliveIdle *int `json:"tcpKeepAliveIdle,omitempty"`

	// TCPKeepAliveInterval is the number of seconds between TCP probes.
	// This field is only supported on Couchbase server versions 8.0.0 and later.
	TCPKeepAliveInterval *int `json:"tcpKeepAliveInterval,omitempty"`

	// TCPKeepAliveProbes is the number of TCP probes missing before the connection is considered dead.
	// This field is only supported on Couchbase server versions 8.0.0 and later.
	TCPKeepAliveProbes *int `json:"tcpKeepAliveProbes,omitempty"`

	// TCPUserTimeout is the number of seconds data is stuck in the send buffer before the connection gets torn down.
	// This field is only supported on Couchbase server versions 8.0.0 and later.
	TCPUserTimeout *int `json:"tcpUserTimeout,omitempty"`
}

type DiskUsageLimit struct {
	// Enabled specifies whether the disk usage limit is enabled, defaulting to false.
	// +kubebuilder:default=false
	Enabled *bool `json:"enabled,omitempty"`

	// Percent is the percentage of disk space that can be used before bucket writes are prevented. This field must be in the range 1-100, defaulting to 85.
	// +kubebuilder:default=85
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	Percent *int `json:"percent,omitempty"`
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

// DatabaseFragmentationThresholdBucket lists triggers for when database compaction at the bucket level. This has different defaults and documentation than cluster level.
type DatabaseFragmentationThresholdBucket struct {
	// Percent specifies the level of view fragmentation that must be reached for View compaction to be automatically triggered.
	// This field must be in the range 2-100, defaulting to the cluster level value.
	// +kubebuilder:validation:Minimum=2
	// +kubebuilder:validation:Maximum=100
	Percent *int `json:"percent,omitempty"`

	// Size the level of database fragmentation that must be reached for data compaction to be automatically triggered on the bucket.
	// More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:validation:Type=string
	Size *resource.Quantity `json:"size,omitempty"`
}

// ViewFragmentationThresholdBucket lists triggers for when view compaction should start at the bucket level. This has different defaults and documentation than cluster level.
type ViewFragmentationThresholdBucket struct {
	// Percent specifies the percentage level of View fragmentation that must be reached for View compaction to be automatically triggered on the bucket
	// This field must be in the range 2-100, defaulting to the cluster level value.
	// +kubebuilder:validation:Minimum=2
	// +kubebuilder:validation:Maximum=100
	Percent *int `json:"percent,omitempty"`

	// Size is the level of View fragmentation that must be reached for view compaction to be automatically triggered on the bucket.
	// More info:
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes
	// +kubebuilder:validation:Type=string
	Size *resource.Quantity `json:"size,omitempty"`
}

// TimeWindow allows the user to restrict when compaction can occur.
type TimeWindow struct {
	// Start is a wallclock time, in the form HH:MM, when a compaction is permitted to start.
	// +kubebuilder:validation:Pattern="^(2[0-3]|[01]?[0-9]):([0-5]?[0-9])$"
	Start *string `json:"start,omitempty"`

	// End is a wallclock time, in the form HH:MM, when a compaction should stop.
	// +kubebuilder:validation:Pattern="^(2[0-3]|[01]?[0-9]):([0-5]?[0-9])$"
	End *string `json:"end,omitempty"`

	// AbortCompactionOutsideWindow stops compaction processes when the
	// process moves outside the window, defaulting to false.
	AbortCompactionOutsideWindow bool `json:"abortCompactionOutsideWindow,omitempty"`
}

// AutoCompaction defines auto-compaction settings.
type AutoCompaction struct {
	// DatabaseFragmentationThreshold defines the default database fragmentation level to determine the point when compaction is triggered for buckets with a couchstore storage backend.
	// +optional
	// +kubebuilder:default="x-couchbase-object"
	DatabaseFragmentationThreshold DatabaseFragmentationThreshold `json:"databaseFragmentationThreshold,omitempty"`

	// ViewFragmentationThreshold defines triggers for when view compaction should start.
	// +optional
	// +kubebuilder:default="x-couchbase-object"
	ViewFragmentationThreshold ViewFragmentationThreshold `json:"viewFragmentationThreshold,omitempty"`

	// MagmaFragmentationThresholdPercentage defines the default database fragmentation level to determine point when database compaction is triggered for buckets with a magma storage backend.
	// This field must be in the range 10-100.
	// This field is ignored for Couchstore buckets.
	// +optional
	// +kubebuilder:validation:Minimum=10
	// +kubebuilder:validation:Maximum=100
	MagmaFragmentationThresholdPercentage *int `json:"magmaFragmentationPercentage,omitempty" annotation:"magmaFragmentationPercentage"`

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

// AutoCompactionBucket defines auto-compaction settings at the bucket level.
type AutoCompactionSpecBucket struct {
	// DatabaseFragmentationThreshold defines triggers for when database compaction should start on buckets with a couchstore storage backend. This field will be ignored if the bucket has a magma storage backend.
	DatabaseFragmentationThreshold *DatabaseFragmentationThresholdBucket `json:"databaseFragmentationThreshold,omitempty"`

	// ViewFragmentationThreshold defines triggers for when view compaction should start. This field will be ignored if the bucket has a magma storage backend.
	ViewFragmentationThreshold *ViewFragmentationThresholdBucket `json:"viewFragmentationThreshold,omitempty"`

	// MagmaFragmentationThresholdPercentage defines the percentage of magma fragmentation level to determine the point when compaction is triggered for buckets with a magma storage backend. This field will be ignored if the bucket has a couchstore storage backend.
	// +optional
	// +kubebuilder:validation:Minimum=10
	// +kubebuilder:validation:Maximum=100
	MagmaFragmentationThresholdPercentage *int `json:"magmaFragmentationPercentage,omitempty" annotation:"magmaFragmentationPercentage"`

	// TombstonePurgeInterval controls how long to wait before purging tombstones.
	// This field must be in the range 1h-1440h, defaulting to the cluster level value.
	// More info:  https://golang.org/pkg/time/#ParseDuration
	TombstonePurgeInterval *metav1.Duration `json:"tombstonePurgeInterval,omitempty"`

	// TimeWindow allows restriction of when compaction can occur. This field will be ignored if the bucket has a magma storage backend.
	TimeWindow *TimeWindow `json:"timeWindow,omitempty"`
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
	// Note that, -operator-managed is added as suffix by operator automatically
	// to the name in order to diffrentiate from non operator managed remote clusters.
	Name string `json:"name"`

	// UUID of the remote cluster.  The UUID of a CouchbaseCluster resource
	// is advertised in the status.clusterId field of the resource.
	// +kubebuilder:validation:Pattern="^[0-9a-f]{32}$"
	// +optional
	UUID string `json:"uuid,omitempty"`

	// Hostname is the connection string to use to connect the remote cluster.  To use IPv6, place brackets (`[`, `]`) around the IPv6 value.
	// +kubebuilder:validation:Pattern=`^((couchbase|http)(s)?(://))?((\b((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)(\.|$)){4}\b)|((([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\-]*[a-zA-Z0-9])\.)*([A-Za-z0-9]|[A-Za-z0-9][A-Za-z0-9\-]*[A-Za-z0-9]))|\[(\s*((([0-9A-Fa-f]{1,4}:){7}([0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){6}(:[0-9A-Fa-f]{1,4}|((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3})|:))|(([0-9A-Fa-f]{1,4}:){5}(((:[0-9A-Fa-f]{1,4}){1,2})|:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3})|:))|(([0-9A-Fa-f]{1,4}:){4}(((:[0-9A-Fa-f]{1,4}){1,3})|((:[0-9A-Fa-f]{1,4})?:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:))|(([0-9A-Fa-f]{1,4}:){3}(((:[0-9A-Fa-f]{1,4}){1,4})|((:[0-9A-Fa-f]{1,4}){0,2}:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:))|(([0-9A-Fa-f]{1,4}:){2}(((:[0-9A-Fa-f]{1,4}){1,5})|((:[0-9A-Fa-f]{1,4}){0,3}:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:))|(([0-9A-Fa-f]{1,4}:){1}(((:[0-9A-Fa-f]{1,4}){1,6})|((:[0-9A-Fa-f]{1,4}){0,4}:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:))|(:(((:[0-9A-Fa-f]{1,4}){1,7})|((:[0-9A-Fa-f]{1,4}){0,5}:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:)))(%.+)?\s*\]))(:[0-9]{0,5})?(\\{0,1}\?network=[^&]+)?$`
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

	// EnablePrechecks sets if the Operator will perform XDCR prechecks.
	// Defaults to false
	// +kubebuilder:default=false
	DisablePrechecks bool `json:"-" annotation:"disablePrechecks"`

	// GlobalSettings configures cluster-wide XDCR advanced settings.
	// These settings provide defaults for new replications and do not affect existing
	// replications retroactively. Only specified fields are applied; unspecified fields
	// are left unchanged on the server.
	GlobalSettings *XDCRGlobalSettings `json:"globalSettings,omitempty"`
}

// XDCRGlobalSettings defines the subset of XDCR advanced settings that are cluster-wide
// (global) or are allowed to be set globally by Couchbase Server. Fields are pointers so that
// omission does not change the corresponding server-side value.
type XDCRGlobalSettings struct {
	// Global / Per-replication settings (can be set globally as defaults)

	// CheckpointInterval is the interval in seconds between checkpoints.
	// This field defaults to 600 and must be between 60 and 14400.
	// +kubebuilder:validation:Minimum=60
	// +kubebuilder:validation:Maximum=14400
	CheckpointInterval *int32 `json:"checkpointInterval,omitempty"`

	// CollectionsOSOMode optimizes for out-of-order mutations streaming (performance toggle).
	// This field defaults to true.
	CollectionsOSOMode *bool `json:"collectionsOSOMode,omitempty"`

	// CompressionType is the compression used for XDCR traffic.
	// This field must be one of "Auto" or "None", defaulting to "Auto".
	// +kubebuilder:validation:Enum=Auto;None
	CompressionType *string `json:"compressionType,omitempty"`

	// DesiredLatency is the target latency (ms) for high-priority replications;
	// lower values result in faster replication but greater load.
	// This field defaults to 50.
	DesiredLatency *int32 `json:"desiredLatency,omitempty"`

	// DocBatchSizeKb is the size (KB) of document batches sent.
	// This field defaults to 2048 and must be between 10 and 10000.
	// +kubebuilder:validation:Minimum=10
	// +kubebuilder:validation:Maximum=10000
	DocBatchSizeKb *int32 `json:"docBatchSizeKb,omitempty"`

	// FailureRestartInterval is the seconds to wait before restarting after a failure.
	// This field defaults to 10 and must be between 1 and 300.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=300
	FailureRestartInterval *int32 `json:"failureRestartInterval,omitempty"`

	// FilterBypassExpiry when true, TTL is removed before replication.
	// This field defaults to false.
	FilterBypassExpiry *bool `json:"filterBypassExpiry,omitempty"`

	// FilterBypassUncommittedTxn when true, documents with uncommitted txn xattrs are not replicated.
	// This field defaults to false.
	FilterBypassUncommittedTxn *bool `json:"filterBypassUncommittedTxn,omitempty"`

	// FilterDeletion when true, delete mutations are filtered out (not replicated).
	// This field defaults to false.
	FilterDeletion *bool `json:"filterDeletion,omitempty"`

	// FilterExpiration when true, expiry mutations are filtered out.
	// This field defaults to false.
	FilterExpiration *bool `json:"filterExpiration,omitempty"`

	// HlvPruningWindowSec is the HLV pruning window (sec) for hybrid logical vector conflict resolution.
	// +kubebuilder:validation:Minimum=1
	HlvPruningWindowSec *int32 `json:"hlvPruningWindowSec,omitempty"`

	// JSFunctionTimeoutMs is the timeout for JS custom conflict-resolution functions (ms).
	// +kubebuilder:validation:Minimum=1
	JSFunctionTimeoutMs *int32 `json:"jsFunctionTimeoutMs,omitempty"`

	// LogLevel is the logging verbosity for XDCR.
	// This field must be one of "Error", "Info", "Debug", or "Trace", defaulting to "Info".
	// +kubebuilder:validation:Enum=Error;Info;Debug;Trace
	LogLevel *string `json:"logLevel,omitempty"`

	// Mobile enables mobile (Sync Gateway) active-active mode.
	// This field must be one of "Active" or "Off", defaulting to "Off".
	// +kubebuilder:validation:Enum=Off;Active
	Mobile *string `json:"mobile,omitempty"`

	// NetworkUsageLimit is the upper limit for replication network usage (MB/s).
	// This field defaults to 0 (no limit).
	// +kubebuilder:validation:Minimum=0
	NetworkUsageLimit *int32 `json:"networkUsageLimit,omitempty"`

	// OptimisticReplicationThreshold is the size threshold below which documents replicate optimistically.
	// This field defaults to 256 and must be between 0 and 20971520.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=20971520
	OptimisticReplicationThreshold *int32 `json:"optimisticReplicationThreshold,omitempty"`

	// Priority is the resource priority for replication streams.
	// This field must be one of "High", "Medium", or "Low", defaulting to "High".
	// +kubebuilder:validation:Enum=High;Medium;Low
	Priority *string `json:"priority,omitempty"`

	// RetryOnRemoteAuthErr defines whether to retry connections when remote auth fails.
	// This field defaults to true.
	RetryOnRemoteAuthErr *bool `json:"retryOnRemoteAuthErr,omitempty"`

	// RetryOnRemoteAuthErrMaxWaitSec is the max wait seconds for retrying remote auth failures.
	// Only effective if retryOnRemoteAuthErr is true. This field defaults to 360.
	// +kubebuilder:validation:Minimum=1
	RetryOnRemoteAuthErrMaxWaitSec *int32 `json:"retryOnRemoteAuthErrMaxWaitSec,omitempty"`

	// SourceNozzlePerNode is the number of source nozzles (parallelism) per source node.
	// This field defaults to 2 and must be between 1 and 100.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	SourceNozzlePerNode *int32 `json:"sourceNozzlePerNode,omitempty"`

	// StatsInterval is the interval for statistics updates (ms).
	// This field defaults to 1000 and must be between 200 and 600000.
	// +kubebuilder:validation:Minimum=200
	// +kubebuilder:validation:Maximum=600000
	StatsInterval *int32 `json:"statsInterval,omitempty"`

	// TargetNozzlePerNode is the number of target nozzles per target node (parallelism).
	// This field defaults to 2 and must be between 1 and 100.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	TargetNozzlePerNode *int32 `json:"targetNozzlePerNode,omitempty"`

	// WorkerBatchSize is the number of mutations per worker batch.
	// This field defaults to 500 and must be between 500 and 10000.
	// +kubebuilder:validation:Minimum=500
	// +kubebuilder:validation:Maximum=10000
	WorkerBatchSize *int32 `json:"workerBatchSize,omitempty"`

	// Global-only settings (cannot be set per-replication)

	// GoGC is the Go GC target percentage for XDCR processes.
	// This field can be an integer (0-100) as a string or "off", defaulting to "100".
	GoGC *string `json:"goGC,omitempty"`

	// GoMaxProcs is the max threads per node for XDCR.
	// This field defaults to 4.
	GoMaxProcs *int32 `json:"goMaxProcs,omitempty"`
}

type Buckets struct {
	// Managed defines whether buckets are managed by the Operator (true), or user managed (false).
	// When Operator managed, all buckets must be defined with either CouchbaseBucket or
	// CouchbaseEphemeralBucket resources.  Manual addition
	// of buckets will be reverted by the Operator.  When user managed, the Operator
	// will not interrogate buckets at all.  This field defaults to false.
	Managed bool `json:"managed,omitempty"`

	// Synchronize allows unmanaged buckets, scopes, and collections to be synchronized as
	// Kubernetes resources by the Operator.  This feature is intended for development only
	// and should not be used for production workloads.  The synchronization workflow starts
	// with `spec.buckets.managed` being set to false, the user can manually create buckets,
	// scopes, and collections using the Couchbase UI, or other tooling.  When you wish to
	// commit to Kubernetes resources, you must specify a unique label selector in the
	// `spec.buckets.selector` field, and this field is set to true.  The Operator will
	// create Kubernetes resources for you, and upon completion set the cluster's `Synchronized`
	// status condition. Synchronizing will not create a Kubernetes resource for the Couchbase
	// Server maintained _system scope. You may then safely set `spec.buckets.managed` to
	// true and the Operator will manage these resources as per usual.  To update an already
	// managed data topology, you must first set it to unmanaged, make any changes, and delete
	// any old resources, then follow the standard synchronization workflow.  The Operator
	// can not, and will not, ever delete, or make modifications to resource specifications
	// that are intended to be user managed, or managed by a life cycle management tool. These
	// actions must be instigated by an end user.  For a more complete experience, refer to
	// the documentation for the `cao save` and `cao restore` CLI commands.
	Synchronize bool `json:"synchronize,omitempty"`

	// Selector is a label selector used to list buckets in the namespace
	// that are managed by the Operator.
	Selector *metav1.LabelSelector `json:"selector,omitempty"`

	// Defined the default storage backend to use if a backend is not specified for a bucket,
	// if this isn't specified then the default will be Couchstore.
	// +kubebuilder:default=couchstore
	DefaultStorageBackend CouchbaseStorageBackend `json:"-" annotation:"defaultStorageBackend"`

	// Allows the operator to reconcile the storage backend for unmanaged buckets to match
	// this value.
	TargetUnmanagedBucketStorageBackend *CouchbaseStorageBackend `json:"-" annotation:"targetUnmanagedBucketStorageBackend"`

	// Used to define whether managed bucket storage backend migration routines should be enabled.
	// This value defaults to false.
	EnableBucketMigrationRoutines bool `json:"enableBucketMigrationRoutines,omitempty" annotation:"enableBucketMigrationRoutines"`

	// MaxConcurrentPodSwaps allows the number of pods affected by a bucket migration at any
	// one time to be increased.
	// By default a migration will affect one pod at a time.
	// This field must be greater than zero.
	MaxConcurrentPodSwaps uint64 `json:"-" annotation:"maxConcurrentPodSwaps"`
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

// MarshalJSON overrides the default JSON Marshalling so we can add the omitempty
// annotation to PodSpec.Containers so that we don't get unknown field warnings
func (t *PodTemplate) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		ObjectMeta `json:"metadata,omitempty"`
		Spec       couchbasePodSpec `json:"spec,omitempty"`
	}{
		ObjectMeta: t.ObjectMeta,
		Spec:       couchbasePodSpec(t.Spec),
	})
}

// Use a new type so we can override MarshalJSON
type couchbasePodSpec v1.PodSpec

// Embedding inherits everything and then we override the Containers field
func (t *couchbasePodSpec) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		*v1.PodSpec
		Containers []v1.Container `json:"containers,omitempty"`
	}{
		PodSpec:    (*v1.PodSpec)(t),
		Containers: t.Containers,
	})
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
	// Each service may only be specified once. An empty list can also be specified
	// for a serviceless class ("[]") if Couchbase version is 7.6.0 or greater.
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
	// https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#pod-v1-core
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

	// DEPRECATED - use spec.image and spec.upgrade instead
	// Image is the container image name that will be used to launch Couchbase
	// server instances in this server class. You cannot downgrade the Couchbase
	// version. Across spec.image and all server classes there can only be two
	// different Couchbase images. Updating this field to a value different than
	// spec.image will cause an automatic upgrade of the server class. If it isn't
	// specified then the cluster image will be used.
	// +kubebuilder:validation:Pattern="^(.*?(:\\d+)?/)?.*?/.*?(:.*?\\d+\\.\\d+\\.\\d+.*|@sha256:[0-9a-f]{64})$"
	Image string `json:"image,omitempty"`
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
	// DEPRECATED - by couchbaseclusters.spec.networking.tls.secretSource.
	// Static enables user to generate static x509 certificates and keys,
	// put them into Kubernetes secrets, and specify them here.  Static secrets
	// are Couchbase specific, and follow no well-known standards.
	Static *StaticTLS `json:"static,omitempty"`

	// SecretSource enables the user to specify a secret conforming to the Kubernetes TLS
	// secret specification that is used for the Couchbase server certificate, and optionally
	// the Operator's client certificate, providing cert-manager compatibility without having
	// to specify a separate root CA.  A server CA certificate must be supplied by one of the
	// provided methods. Certificates referred to must conform to the keys of well-known type
	// "kubernetes.io/tls" with "tls.crt" and "tls.key". If the "tls.key" is an encrypted
	// private key then the secret type can be the generic Opaque type since "kubernetes.io/tls"
	// type secrets cannot verify encrypted keys.
	SecretSource *TLSSecretSource `json:"secretSource,omitempty"`

	// RootCAs defines a set of secrets that reside in this namespace that contain
	// additional CA certificates that should be installed in Couchbase.  The CA
	// certificates that are defined here are in addition to those defined for the
	// cluster, optionally by couchbaseclusters.spec.networking.tls.secretSource, and
	// thus should not be duplicated.  Each Secret referred to must be of well-known type
	// "kubernetes.io/tls" and must contain one or more CA certificates under the key "tls.crt".
	// Multiple root CA certificates are only supported on Couchbase Server 7.1 and greater,
	// and not with legacy couchbaseclusters.spec.networking.tls.static configuration.
	RootCAs []string `json:"rootCAs,omitempty"`

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
	// Node to node encryption can only be used when TLS certificates are managed by the
	// Operator.  This field must be either "ControlPlaneOnly", "All", or "Strict".
	NodeToNodeEncryption *NodeToNodeEncryptionType `json:"nodeToNodeEncryption,omitempty"`

	// TLSMinimumVersion specifies the minimum TLS version the Couchbase server can
	// negotiate with a client.  Must be one of TLS1.0, TLS1.1 TLS1.2 or TLS1.3,
	// defaulting to TLS1.2.  TLS1.3 is only valid for Couchbase Server 7.1.0 onward.
	// TLS1.0 and TLS1.1 are not valid for Couchbase Server 7.6.0 onward.
	// +kubebuilder:default=TLS1.2
	TLSMinimumVersion TLSVersion `json:"tlsMinimumVersion,omitempty"`

	// CipherSuites specifies a list of cipher suites for Couchbase server to select
	// from when negotiating TLS handshakes with a client.  Suites are not validated
	// by the Operator.  Run "openssl ciphers -v" in a Couchbase server pod to
	// interrogate supported values.
	// +listType=set
	CipherSuites []string `json:"cipherSuites,omitempty"`

	// PassphraseConfig configures the passphrase key to use with encrypted certificates.
	// The passphrase may be registered with Couchbase Server using a local script or a
	// rest endpoint. Private key encryption is only available on Couchbase Server
	// versions 7.1 and greater.
	PassphraseConfig PassphraseConfig `json:"passphrase,omitempty"`

	// AllowPlainTextCertReload allows the reload of TLS certificates in plain text.
	// This option should only be enabled as a means to recover connectivity with
	// server in the event that any of the server certificates expire. When enabled
	// the Operator only attempts plain text cert reloading when expired certificates
	// are detected.
	// +kubebuilder:default=false
	AllowPlainTextCertReload bool `json:"allowPlainTextCertReload,omitempty"`

	// ValidateBareHostnames controls whether the operator expects bare hostname
	// entries (like "<cluster-name>-srv") in server certificates. When false,
	// the operator will not require bare hostname SAN entries for its internal
	// TLS verification. Defaults to true for backward compatibility.
	// +kubebuilder:default=true
	ValidateBareHostnames bool `json:"validateBareHostnames"`
}

type PassphraseType string

const (
	PassphraseTypeScript PassphraseType = "script"
	PassphraseTypeRest   PassphraseType = "rest"
)

type PassphraseConfig struct {
	// PassphraseScriptConfig is the configuration to register a private key passphrase with a script.
	// The Operator auto-provisions the underlying script so this config simply provides a mechanism
	// to perform the decryption of the Couchbase Private Key using a local script.
	Script *PassphraseScriptConfig `json:"script,omitempty"`

	// PassphraseRestConfig is the configuration to register a private key passphrase with a rest endpoint.
	// When the private key is accessed, Couchbase Server attempts to extract the password by means of the
	// specified endpoint. The response status must be 200 and the response text must be the exact passphrase
	// excluding newlines and extraneous spaces.
	Rest *PassphraseRestConfig `json:"rest,omitempty"`
}

type PassphraseScriptConfig struct {
	// Secret is the secret containing the passphrase string. The secret is expected
	// to contain "passphrase" key with the passphrase string as a value.
	Secret string `json:"secret"`
}

type PassphraseRestConfig struct {
	// URL is the endpoint to be called to retrieve the passphrase.
	// URL will be called using the GET method and may use http/https protocol.
	URL string `json:"url"`

	// VerifyPeer ensures peer verification is performed when Https is used.
	// +kubebuilder:default=true
	VerifyPeer bool `json:"verifyPeer,omitempty"`

	// Headers is a map of one or more key-value pairs to pass alongside the Get request.
	Headers map[string]string `json:"headers,omitempty"`

	// AddressFamily is the address family to use. By default inet (meaning IPV4) is used.
	// +kubebuilder:validation:Enum=inet;inet6
	// +kubebuilder:default=inet
	AddressFamily string `json:"addressFamily,omitempty"`

	// Timeout is  the number of milliseconds that must elapse before the call is timed out.
	// +kubebuilder:default=5000
	Timeout uint64 `json:"timeout,omitempty"`
}

type StaticTLS struct {
	// ServerSecret is a secret name containing TLS certs used by each Couchbase member pod
	// for the communication between Couchbase server and its clients.  The secret must
	// contain a certificate chain (data key "chain.pem") and a private
	// key (data key "pkey.key").  The private key must be in the PKCS#1 RSA
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
	// "tls.key" as per the kubernetes.io/tls secret type.  It may also contain "ca.crt".
	// Only a single PEM formated x509 certificate can be provided to "ca.crt".
	// The single certificate may also bundle together multiple root CA certificates.
	// Multiple root CA certificates are only supported on Couchbase Server 7.1 and greater.
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

// +kubebuilder:validation:Enum=Available;Balanced;ManageConfig;Scaling;ScalingUp;ScalingDown;Upgrading;Hibernating;Error;AutoscaleReady;Synchronized;WaitingBetweenMigrations;Migrating;Rebalancing;ExpandingVolume;BucketMigrating;Unreconcilable;WaitingBetweenUpgrades;MixedMode;ManualInterventionRequired;
type ClusterConditionType string

const (
	ClusterConditionAvailable                  ClusterConditionType = "Available"
	ClusterConditionBalanced                   ClusterConditionType = "Balanced"
	ClusterConditionManageConfig               ClusterConditionType = "ManageConfig"
	ClusterConditionScaling                    ClusterConditionType = "Scaling"
	ClusterConditionScalingUp                  ClusterConditionType = "ScalingUp"
	ClusterConditionScalingDown                ClusterConditionType = "ScalingDown"
	ClusterConditionUpgrading                  ClusterConditionType = "Upgrading"
	ClusterConditionHibernating                ClusterConditionType = "Hibernating"
	ClusterConditionError                      ClusterConditionType = "Error"
	ClusterConditionAutoscaleReady             ClusterConditionType = "AutoscaleReady"
	ClusterConditionSynchronized               ClusterConditionType = "Synchronized"
	ClusterConditionWaitingBetweenMigrations   ClusterConditionType = "WaitingBetweenMigrations"
	ClusterConditionWaitingBetweenUpgrades     ClusterConditionType = "WaitingBetweenUpgrades"
	ClusterConditionMigrating                  ClusterConditionType = "Migrating"
	ClusterConditionRebalancing                ClusterConditionType = "Rebalancing"
	ClusterConditionExpandingVolume            ClusterConditionType = "ExpandingVolume"
	ClusterLastUpdateTime                      ClusterConditionType = "LastUpdateTime"
	ClusterConditionBucketMigration            ClusterConditionType = "BucketMigrating"
	ClusterUnreconcilable                      ClusterConditionType = "Unreconcilable"
	ClusterConditionMixedMode                  ClusterConditionType = "MixedMode"
	ClusterConditionManualInterventionRequired ClusterConditionType = "ManualInterventionRequired"
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

	// LastUpdateTime is the time that the cluster object was last updated.
	LastUpdateTime string `json:"lastUpdateTime,omitempty"`

	// RebalanceAttempts is the number of consecutive reconciliation loops that the operator has failed to rebalance after exhausting all retries.
	RebalanceAttempts int `json:"rebalanceAttempts,omitempty"`
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

	// BucketStorageBackend is the storage backend of the bucket.
	BucketStorageBackend string `json:"storageBackend,omitempty"`

	// NumVBuckets is the number of vbuckets in the bucket.
	NumVBuckets int `json:"numVBuckets,omitempty"`

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
	// DEPRECATED - By Couchbase Server metrics endpoint on version 7.0+
	// Prometheus provides integration with Prometheus monitoring.
	Prometheus *CouchbaseClusterMonitoringPrometheusSpec `json:"prometheus,omitempty"`
}

type CouchbaseClusterMonitoringPrometheusSpec struct {
	// Enabled is a boolean that enables/disables the metrics sidecar container.
	// This must be set to true, when image is provided.
	Enabled bool `json:"enabled,omitempty"`

	// Image is the metrics image to be used to collect metrics.
	// No validation is carried out as this can be any arbitrary repo and tag.
	// enabled must be set to true, when image is provided.
	Image string `json:"image"`

	// Resources is the resource requirements for the metrics container.
	// Will be populated by Kubernetes defaults if not specified.
	Resources *v1.ResourceRequirements `json:"resources,omitempty"`

	// AuthorizationSecret is the name of a Kubernetes secret that contains a
	// bearer token to authorize GET requests to the metrics endpoint
	AuthorizationSecret *string `json:"authorizationSecret,omitempty"`

	// RefreshRate is the frequency in which cached statistics are updated in seconds.
	// Shorter intervals will add additional resource overhead to clusters running Couchbase Server 7.0+
	// Default is 60 seconds, Maximum value is 600 seconds, and minimum value is 1 second.
	// +kubebuilder:default=60
	// +kubebuilder:validation:Maximum=600
	// +kubebuilder:validation:Minimum=1
	RefreshRate uint64 `json:"refreshRate,omitempty"`
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
	// +kubebuilder:default="couchbase/fluent-bit:1.2.9"
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

	// TLS configures mounting kubernetes TLS secrets into the logging sidecar.
	// The operator will (in a later release) mount each secret under
	// <mountPath>/<secretName>/ and the files within the secret will retain
	// their keys as filenames. In 2.9.0 this field is accepted by the CRD but
	// currently not implemented - usage will be rejected by the admission
	// controller. Implementation (mounting) is planned for 2.9.1.
	TLS *LogShipperSidecarTLSSpec `json:"tls,omitempty"`
}

// LogShipperSidecarTLSSpec configures TLS secret mounts for the logging sidecar.
type LogShipperSidecarTLSSpec struct {
	// MountPath is the parent directory into which each secret will be mounted
	// as a sub-directory named after the secret. For example, a secret named
	// `fluent-bit-ca` mounted with MountPath `/fluent-bit/certs/` will expose
	// files under `/fluent-bit/certs/fluent-bit-ca/`.
	// +kubebuilder:default="/fluent-bit/certs/"
	MountPath string `json:"mountPath,omitempty"`

	// SecretNames is the list of Kubernetes Secret names (typically of type
	// kubernetes.io/tls) to mount into the sidecar. Filenames inside each
	// mounted directory will match the keys in the Secret's data map.
	SecretNames []string `json:"secretNames,omitempty"`
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

	// How long Couchbase Server keeps rotated audit logs.
	// If set to 0 (the default) then audit logs won't be pruned.
	// Has a maximum of 35791394 seconds.
	// +kubebuilder:default="0"
	PruneAge *metav1.Duration `json:"pruneAge,omitempty"`
}

type CouchbaseClusterAuditGarbageCollectionSpec struct {
	// DEPRECATED - by spec.logging.audit.rotation for Couchbase Server 7.2.4+
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

// EncryptionAtRestSpec is the specification for configuring encryption at rest.
type EncryptionAtRestSpec struct {
	// Managed defines whether the operator should manage encryption at rest for the cluster.
	// This includes encryption keys and encryption at rest settings.
	Managed bool `json:"managed,omitempty"`

	// Selector is a label selector used to select the encryption keys to use.
	Selector *metav1.LabelSelector `json:"selector,omitempty"`

	// Configuration defines how the configurations on the cluster should be encrypted at rest.
	// +kubebuilder:default={enabled: true}
	Configuration *EncryptionAtRestUsageConfiguration `json:"configuration"`

	// Audit is the configuration for encryption at rest for the cluster.
	Audit *EncryptionAtRestUsageConfiguration `json:"audit,omitempty"`

	// Log is the configuration for encryption at rest for log files.
	// NOTE: Enabled encryption at rest of logs will break fluent-bit log streaming.
	Log *EncryptionAtRestUsageConfiguration `json:"log,omitempty"`
}

// EncryptionAtRestUsageConfiguration configures encryption for a particular usage.
type EncryptionAtRestUsageConfiguration struct {
	// Enabled enables encryption at rest for the cluster.
	Enabled bool `json:"enabled"`

	// Key is the name of the encryption key to use for encryption at rest.
	// If not provided, the operator will use the master password.
	KeyName string `json:"keyName,omitempty"`

	// RotationInterval is the interval at which the encryption key will be rotated.
	// Must be greater or equal to 7 days. Default is 30 days.
	// +kubebuilder:default="720h"
	RotationInterval *metav1.Duration `json:"rotationInterval,omitempty"`

	// KeyLifetime is the lifetime of the encryption key.
	// Must be greater or equal to 30 days. Default is 365 days.
	// +kubebuilder:default="8760h"
	KeyLifetime *metav1.Duration `json:"keyLifetime,omitempty"`
}

// CouchbaseEncryptionKeyType defines the type of encryption key to use.
type CouchbaseEncryptionKeyType string

const (
	// CouchbaseEncryptionKeyTypeAutoGenerated indicates an automatically generated key.
	CouchbaseEncryptionKeyTypeAutoGenerated CouchbaseEncryptionKeyType = "AutoGenerated"

	// CouchbaseEncryptionKeyTypeAWS indicates an AWS KMS managed key.
	CouchbaseEncryptionKeyTypeAWS CouchbaseEncryptionKeyType = "AWS"

	// CouchbaseEncryptionKeyTypeKMIP indicates a KMIP managed key.
	CouchbaseEncryptionKeyTypeKMIP CouchbaseEncryptionKeyType = "KMIP"
)

// CouchbaseEncryptionApproach defines the encryption approach for KMIP keys.
type CouchbaseEncryptionApproach string

const (
	// CouchbaseEncryptionApproachNativeEncryptDecrypt uses native encrypt/decrypt approach.
	CouchbaseEncryptionApproachNativeEncryptDecrypt CouchbaseEncryptionApproach = "NativeEncryptDecrypt"

	// CouchbaseEncryptionApproachLocalEncrypt uses local encryption approach.
	CouchbaseEncryptionApproachLocalEncrypt CouchbaseEncryptionApproach = "LocalEncrypt"
)

// CouchbaseEncryptionKeyUsage defines what the encryption key should be used for.
type CouchbaseEncryptionKeyUsage struct {
	// Configuration defines whether the key should be used for configurations.
	// +kubebuilder:default=true
	Configuration bool `json:"configuration"`

	// Key defines whether the key should be used for keys.
	// +kubebuilder:default=true
	Key bool `json:"key"`

	// Log defines whether the key should be used for logs.
	// +kubebuilder:default=true
	Log bool `json:"log"`

	// Audit defines whether the key should be used for audit.
	// +kubebuilder:default=true
	Audit bool `json:"audit"`

	// AllBuckets defines whether the key should be used for all buckets.
	// +kubebuilder:default=true
	AllBuckets bool `json:"allBuckets"`

	// ManagedBucketSelection defines whether the key should be used for managed bucket selection.
	// +optional
	ManagedBucketSelection bool `json:"managedBucketSelection"`
}

// CouchbaseEncryptionKeyRotation defines rotation settings for auto-generated keys.
type CouchbaseEncryptionKeyRotation struct {
	// IntervalDays defines the rotation interval in days.
	// +kubebuilder:validation:Minimum=1
	IntervalDays int `json:"intervalDays"`

	// StartTime defines when rotation should start (timestamp).
	StartTime *metav1.Time `json:"startTime,omitempty"`
}

// CouchbaseEncryptionKeyAutoGenerated defines settings for auto-generated encryption keys.
type CouchbaseEncryptionKeyAutoGenerated struct {
	// Rotation defines the rotation settings for the auto-generated key.
	// If not provided, the key will not be rotated.
	Rotation *CouchbaseEncryptionKeyRotation `json:"rotation,omitempty"`

	// CanBeCached defines whether the key can be cached.
	// +kubebuilder:default=true
	CanBeCached bool `json:"canBeCached"`

	// EncryptWithKey is the name of another encryption key to use to encrypt the auto-generated key. If not provided,
	// the key will be encrypted with the master password.
	EncryptWithKey string `json:"encryptWithKey,omitempty"`
}

// CouchbaseEncryptionKeyAWS defines settings for AWS KMS encryption keys.
type CouchbaseEncryptionKeyAWS struct {
	// KeyArn is the ARN of the AWS KMS key.
	KeyARN string `json:"keyARN"`

	// KeyRegion is the AWS region where the key is located.
	KeyRegion string `json:"keyRegion,omitempty"`

	// UseImds defines whether to use IMDS for authentication.
	UseIMDS bool `json:"useIMDS,omitempty"`

	// CredentialSecret is the name of the secret containing AWS credentials. The secret must contain a key with the name "credentials"
	// with the data value of the AWS credentials file.
	CredentialsSecret string `json:"credentialsSecret,omitempty"`

	// ProfileName is the name of the profile to use from the credentials secret.
	ProfileName string `json:"profileName,omitempty"`
}

// CouchbaseEncryptionKeyKMIP defines settings for KMIP encryption keys.
type CouchbaseEncryptionKeyKMIP struct {
	// Host is the KMIP server host.
	Host string `json:"host"`

	// Port is the KMIP server port.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65536
	Port int `json:"port"`

	// TimeoutInMs is the timeout in milliseconds.
	// +kubebuilder:validation:Minimum=1000
	// +kubebuilder:validation:Maximum=300000
	TimeoutInMs int `json:"timeoutInMs"`

	// VerifyWithSystemCA defines whether to verify with system CA.
	// +kubebuilder:default=true
	VerifyWithSystemCA bool `json:"verifyWithSystemCA"`

	// VerifyWithCouchbaseCA defines whether to verify with Couchbase CA.
	// +kubebuilder:default=true
	VerifyWithCouchbaseCA bool `json:"verifyWithCouchbaseCA"`

	// ClientSecret is the name of the secret containing the client private key, cert and passphrase. The secret must contain the keys
	// "tls.crt", "tls.key", and "passphrase" with the data value of the client cert, key in encrypted pkcs8 format, and the passphrase
	// for the key respectively.
	ClientSecret string `json:"clientSecret"`

	// KeyID is the KMIP key identifier.
	KeyID string `json:"keyID,omitempty"`

	// EncryptionApproach defines the encryption approach to use.
	// +kubebuilder:validation:Enum=NativeEncryptDecrypt;LocalEncrypt
	EncryptionApproach CouchbaseEncryptionApproach `json:"encryptionApproach,omitempty"`
}

// CouchbaseEncryptionKeySpec defines the desired state of CouchbaseEncryptionKey.
type CouchbaseEncryptionKeySpec struct {
	// KeyType defines the type of encryption key. This field is immutable after creation.
	// +kubebuilder:validation:Enum=AutoGenerated;AWS;KMIP
	KeyType CouchbaseEncryptionKeyType `json:"keyType"`

	// Usage defines what the encryption key should be used for.
	// +kubebuilder:default={configuration: true, key: true, log: true, audit: true, allBuckets: true, managedBucketSelection: false}
	Usage *CouchbaseEncryptionKeyUsage `json:"usage"`

	// AutoGenerated defines settings for auto-generated keys.
	// This field is only valid when KeyType is "AutoGenerated".
	AutoGenerated *CouchbaseEncryptionKeyAutoGenerated `json:"autoGenerated,omitempty"`

	// AwsKey defines settings for AWS KMS keys.
	// This field is only valid when KeyType is "AWS".
	AWSKey *CouchbaseEncryptionKeyAWS `json:"awsKey,omitempty"`

	// KmipKey defines settings for KMIP keys.
	// This field is only valid when KeyType is "KMIP".
	KMIPKey *CouchbaseEncryptionKeyKMIP `json:"kmipKey,omitempty"`
}

// The CouchbaseEncryptionKey resource is used to manage encryption keys for a Couchbase cluster.
// CouchbaseEncryptionKey is the Schema for the couchbaseencryptionkeys API.
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:categories=all;couchbase
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="type",type="string",JSONPath=".spec.keyType"
type CouchbaseEncryptionKey struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec CouchbaseEncryptionKeySpec `json:"spec,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// CouchbaseEncryptionKeyList contains a list of CouchbaseEncryptionKey.
type CouchbaseEncryptionKeyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseEncryptionKey `json:"items"`
}

type PasswordPolicySpec struct {
	// RequirePasswordResetOnPolicyChange defines whether users will be required to change
	// their password when the password policy is updated.
	// This field is only available for Couchbase Server 8.0.0+.
	RequirePasswordResetOnPolicyChange *bool `json:"requirePasswordResetOnPolicyChange,omitempty"`

	// PolicyChangePasswordResetExemptUsers defines a list of users who will not be required to change
	// their password if requirePasswordResetOnPolicyChange is set to true and the password policy is updated.
	// This field is only available for Couchbase Server 8.0.0+.
	PasswordResetOnPolicyChangeExemptUsers []*string `json:"passwordResetOnPolicyChangeExemptUsers,omitempty"`

	// MinLength sets the minimum length a password must be,
	// This field must be between 0 and 100.
	// If this field is set to 0, Couchbase Server will permit the definition of highly insecure
	// zero-length passwords which is not recommended.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	MinLength *int `json:"minLength,omitempty"`

	// EnforceUppercase sets whether passwords must contain at least one uppercase letter.
	EnforceUppercase *bool `json:"enforceUppercase,omitempty"`

	// EnforceLowercase sets whether passwords must contain at least one lowercase letter.
	EnforceLowercase *bool `json:"enforceLowercase,omitempty"`

	// EnforceDigits sets whether passwords must contain at least one digit.
	EnforceDigits *bool `json:"enforceDigits,omitempty"`

	// EnforceSpecialChars sets whether passwords must contain at least one special character.
	// If this is set to true, the allowed special chars are limited to: @, %, +, /, ', \, ", !, #, $, ^, ?, :, ,, (, ), {, }, [, ], ~, `, -, and _.
	EnforceSpecialChars *bool `json:"enforceSpecialChars,omitempty"`
}
