/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2eutil

import (
	"context"
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ScheduleIn creates a cron schedule that will schedule a single run in a certain duration
// from now.  Note that the schedule is relative to the local timezone, so running in the
// Kubernetes cluster under test is a good idea.
func ScheduleIn(in time.Duration) string {
	when := time.Now().UTC().Add(in)
	return fmt.Sprintf("%d * * * *", when.Minute())
}

// DefaultSchedule schedules a cron run that will execute.  If the cron implementation is
// driven on transition from one minute to the next, then it's possible the schedule may
// be created for the next minute, the clock ticks over, and then the resource gets
// committed, thus missing the transition.  For this reason we use 2m as a default to be
// nearly 100% sure it'll happen.
func DefaultSchedule() string {
	return ScheduleIn(2 * time.Minute)
}

// Backup is an abstract type used to build backups.
type Backup struct {
	// kind is the type of backup to perform.
	kind couchbasev2.Strategy

	// fullSchedule is when to run the full backup.
	fullSchedule string

	// incrementalSchedule is when to run the incremental backups.
	incrementalSchedule string

	// include is what to include in the backup.
	include []couchbasev2.BucketScopeOrCollectionNameWithDefaults

	// exclude is what to exclude from the backup.
	exclude []couchbasev2.BucketScopeOrCollectionNameWithDefaults

	// s3Bucket indicates the backup should be to s3.
	s3Bucket string

	// objStoreSecret is the secret to be used for authentication to
	// the objStore
	objStoreSecret *v1.Secret
	// objStore indicates the remote destination for backup.
	objStore string
	// retention is the length of time to retain backups for.
	retention time.Duration

	// size is the backup volume size.
	size *resource.Quantity

	// autoscaling defines any autoscaling parameters.
	autoscaling *couchbasev2.CouchbaseBackupAutoScaling

	// storageClass allows the storage class to be explcitly defined.
	storageClass string

	// withoutFTAlias prevents FT alias from being backed up.
	withoutFTAlias bool

	// configures backup to use an ephemeral volume
	ephemeral bool

	// configures how long before a finished backup job deletes.
	ttlSeconds *int32

	// whether backup SDK can use IAM for credentials.
	useIAM bool

	// custom object endpoint to be used with backup.
	objStoreEndpoint string

	// cert to be used with the custom object endpoint.
	objStoreEndpointCert *v1.Secret
}

// NewFullBackup creates a full-only backup with all required parameters.
func NewFullBackup(fullSchedule string) *Backup {
	return &Backup{
		kind:         couchbasev2.FullOnly,
		fullSchedule: fullSchedule,
	}
}

func NewIncrementalBackup(fullSchedule, incrementalSchedule string) *Backup {
	return &Backup{
		kind:                couchbasev2.FullIncremental,
		fullSchedule:        fullSchedule,
		incrementalSchedule: incrementalSchedule,
	}
}

// Include includes only the selected data in a backup.  This may be a string representing
// buckets, scope or collections explcitly, and also allows implicit inclusion from non
// ambiguous datatypes.
func (b *Backup) Include(things ...interface{}) *Backup {
	for _, thing := range things {
		switch t := thing.(type) {
		case couchbasev2.BucketScopeOrCollectionNameWithDefaults:
			b.include = append(b.include, t)
		case string:
			b.include = append(b.include, couchbasev2.BucketScopeOrCollectionNameWithDefaults(t))
		case *couchbasev2.CouchbaseBucket:
			b.include = append(b.include, couchbasev2.BucketScopeOrCollectionNameWithDefaults(t.GetName()))
		case *couchbasev2.CouchbaseEphemeralBucket:
			b.include = append(b.include, couchbasev2.BucketScopeOrCollectionNameWithDefaults(t.GetName()))
		case *couchbasev2.CouchbaseMemcachedBucket:
			b.include = append(b.include, couchbasev2.BucketScopeOrCollectionNameWithDefaults(t.GetName()))
		}
	}

	return b
}

// Exclude excludes the selected data from a backup.  This may be a string representing
// buckets, scope or collections explcitly, and also allows implicit exclusion from non
// ambiguous datatypes.
func (b *Backup) Exclude(things ...interface{}) *Backup {
	for _, thing := range things {
		switch t := thing.(type) {
		case couchbasev2.BucketScopeOrCollectionNameWithDefaults:
			b.exclude = append(b.exclude, t)
		case string:
			b.exclude = append(b.exclude, couchbasev2.BucketScopeOrCollectionNameWithDefaults(t))
		case *couchbasev2.CouchbaseBucket:
			b.exclude = append(b.exclude, couchbasev2.BucketScopeOrCollectionNameWithDefaults(t.GetName()))
		case *couchbasev2.CouchbaseEphemeralBucket:
			b.exclude = append(b.exclude, couchbasev2.BucketScopeOrCollectionNameWithDefaults(t.GetName()))
		case *couchbasev2.CouchbaseMemcachedBucket:
			b.exclude = append(b.exclude, couchbasev2.BucketScopeOrCollectionNameWithDefaults(t.GetName()))
		}
	}

	return b
}

// ToS3, if not empty, sends the backup to S3.
func (b *Backup) ToS3(bucket string) *Backup {
	b.s3Bucket = bucket
	return b
}

// ToObjStore, if not empty, sends the backup to the remote object store.
func (b *Backup) ToObjStore(objStore string) *Backup {
	b.objStore = objStore

	return b
}

// WithObjStoreSecret the secret to use to authenticate against the object store.
func (b *Backup) WithObjStoreSecret(secret *v1.Secret) *Backup {
	if secret != nil {
		b.objStoreSecret = secret
	}

	return b
}

// WithRetention allows backup retention to be set.
func (b *Backup) WithRetention(duration time.Duration) *Backup {
	b.retention = duration
	return b
}

// WithSize allows the backup volume size to be set.
func (b *Backup) WithSize(size *resource.Quantity) *Backup {
	b.size = size
	return b
}

// WithAutoscaling allows backup volume autoscaling.
func (b *Backup) WithAutoscaling(limit *resource.Quantity, threshold, increment int) *Backup {
	b.autoscaling = &couchbasev2.CouchbaseBackupAutoScaling{
		Limit:            limit,
		ThresholdPercent: threshold,
		IncrementPercent: increment,
	}

	return b
}

// WithEphemeralVolume sets backup to use an ephemeral volume.
func (b *Backup) WithEphemeralVolume() *Backup {
	b.ephemeral = true
	return b
}

// WithTTL sets when backup job should be deleted.
func (b *Backup) WithJobTTL(time int32) *Backup {
	b.ttlSeconds = &time
	return b
}

// WithStorageClass allows the use of a non-default storage class.
func (b *Backup) WithStorageClass(storageClass string) *Backup {
	b.storageClass = storageClass

	return b
}

// WithServices allows specifying specific services to be backedup.
func (b *Backup) WithoutFTAlias() *Backup {
	b.withoutFTAlias = true
	return b
}

// WithUseIAM set the cluster to use IAM Role.
func (b *Backup) WithUseIAM(useIAM bool) *Backup {
	b.useIAM = useIAM

	return b
}

// WithCustomStoreURL sets the custom url to use
// with backup.spec.objectStore.Endpoint.
func (b *Backup) WithCustomStoreURL(url string) *Backup {
	b.objStoreEndpoint = url

	return b
}

// WithCustomStoreCert sets the custom url to use
// with backup.spec.objectStore.CertSecret.
func (b *Backup) WithCustomStoreCert(secret *v1.Secret) *Backup {
	b.objStoreEndpointCert = secret

	return b
}

// MustCreate generates the concrete backup resource and creates it in Kubernetes.
func (b *Backup) MustCreate(t *testing.T, kubernetes *types.Cluster) *couchbasev2.CouchbaseBackup {
	generateName := "backup-"

	backup := &couchbasev2.CouchbaseBackup{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: generateName,
		},
		Spec: couchbasev2.CouchbaseBackupSpec{
			Strategy: b.kind,
			Full: &couchbasev2.CouchbaseBackupSchedule{
				Schedule: b.fullSchedule,
			},
			DefaultRecoveryMethod: couchbasev2.DefaultRecoveryTypeNone,
		},
	}

	if b.s3Bucket != "" && b.objStore != "" {
		Die(t, fmt.Errorf("object store can not be used with s3 bucket"))
	}

	if b.incrementalSchedule != "" {
		backup.Spec.Incremental = &couchbasev2.CouchbaseBackupSchedule{
			Schedule: b.incrementalSchedule,
		}
	}

	if backup.Spec.ObjectStore == nil {
		backup.Spec.ObjectStore = &couchbasev2.ObjectStoreSpec{}
	}

	if b.objStoreSecret != nil {
		backup.Spec.ObjectStore.Secret = b.objStoreSecret.Name
	}

	if b.objStore != "" {
		backup.Spec.ObjectStore.URI = couchbasev2.ObjectStoreURI(b.objStore)
	}

	if b.s3Bucket != "" {
		backup.Spec.S3Bucket = couchbasev2.S3BucketURI(fmt.Sprintf("s3://%s", b.s3Bucket))
	}

	if b.useIAM {
		backup.Spec.ObjectStore.UseIAM = &b.useIAM
	}

	if b.objStoreEndpoint != "" {
		if backup.Spec.ObjectStore.Endpoint == nil {
			backup.Spec.ObjectStore.Endpoint = &couchbasev2.ObjectEndpoint{URL: b.objStoreEndpoint}
		}

		if b.objStoreEndpointCert != nil {
			backup.Spec.ObjectStore.Endpoint.CertSecret = b.objStoreEndpointCert.Name
		}
	}

	if b.retention != 0 {
		backup.Spec.BackupRetention = &metav1.Duration{
			Duration: b.retention,
		}
	}

	if b.size != nil {
		backup.Spec.Size = b.size
	}

	if b.autoscaling != nil {
		backup.Spec.AutoScaling = b.autoscaling
	}

	if b.ephemeral {
		backup.Spec.EphemeralVolume = true
	}

	if len(b.include) > 0 || len(b.exclude) > 0 {
		backup.Spec.Data = &couchbasev2.CouchbaseBackupDataFilter{
			Include: b.include,
			Exclude: b.exclude,
		}
	}

	if b.storageClass != "" {
		backup.Spec.StorageClassName = &b.storageClass
	}

	if b.withoutFTAlias {
		falseRef := false
		backup.Spec.Services.FTSAliases = &falseRef
	}

	backup.Spec.TTLSecondsAfterFinished = b.ttlSeconds

	newBackup, err := kubernetes.CRClient.CouchbaseV2().CouchbaseBackups(kubernetes.Namespace).Create(context.Background(), backup, metav1.CreateOptions{})
	if err != nil {
		Die(t, err)
	}

	return newBackup
}

// Restore is an abstract type used to build restores.
type Restore struct {
	// backup is a reference to the backup resource to restore from.
	backup *couchbasev2.CouchbaseBackup

	// s3Bucket indicates the restore should be to s3.
	s3Bucket string

	// objStore indicates the restore should be from a cloud store.
	objStore string

	// objStoreSecret containst the credentials for use when restoring from a cloud store.
	objStoreSecret *v1.Secret

	// withBucketConfig enables bucket configuration restoration.
	withBucketConfig bool

	// withoutData omits data restoration.
	withoutData bool

	// withoutGSI omits GSI restoration.
	withoutGSI bool

	// withoutEventing omits eventing restoration.
	withoutEventing bool

	// withoutAnalytics omits analytics restoration.
	withoutAnalytics bool

	// withoutFTAlias omits ft alias restoration.
	withoutFTAlias bool

	// include are data items to include in the restore.
	include []couchbasev2.BucketScopeOrCollectionNameWithDefaults

	// exclude are data items to exclude from restoration.
	exclude []couchbasev2.BucketScopeOrCollectionNameWithDefaults

	// mapping are any data mappings to perform.
	mapping []couchbasev2.RestoreMapping

	// forceUpdates overwrites data in the cluster with restore data
	forceUpdates bool

	// useIAM
	useIAM bool

	// custom object endpoint to be used with backup.
	objStoreEndpoint string

	// cert to be used with the custom object endpoint.
	objStoreEndpointCert *v1.Secret

	// use blank backup name
	useBlankBackupName bool
}

// NewRestore create a new restore with all the required parameters.
func NewRestore(backup *couchbasev2.CouchbaseBackup) *Restore {
	return &Restore{
		backup: backup,
	}
}

// WithForcedUpdates, overwrites existing data in cluster regardless of what's "newer".
func (r *Restore) WithForcedUpdates() *Restore {
	r.forceUpdates = true
	return r
}

// FromS3, if not empty, retrieves the backup to S3.
func (r *Restore) FromS3(bucket string) *Restore {
	r.s3Bucket = bucket
	return r
}

// FromObjStore, if not empty, retrieves the backup from a cloud store.
func (r *Restore) FromObjStore(objStore string) *Restore {
	r.objStore = objStore
	return r
}

func (r *Restore) WithObjStoreSecret(secret *v1.Secret) *Restore {
	r.objStoreSecret = secret
	return r
}

// WithBucketConfig enables bucket configuration restoration.
func (r *Restore) WithBucketConfig() *Restore {
	r.withBucketConfig = true
	return r
}

// WithoutData omits data restoration.
func (r *Restore) WithoutData() *Restore {
	r.withoutData = true
	return r
}

// WithoutGSI omits GSI restoration.
func (r *Restore) WithoutGSI() *Restore {
	r.withoutGSI = true
	return r
}

// WithoutEventing omits eventing restoration.
func (r *Restore) WithoutEventing() *Restore {
	r.withoutEventing = true
	return r
}

// WithoutAnalytics omits analytics restoration.
func (r *Restore) WithoutAnalytics() *Restore {
	r.withoutAnalytics = true
	return r
}

// WithoutFTAlias omits ft alias restoration.
func (r *Restore) WithoutFTAlias() *Restore {
	r.withoutFTAlias = true
	return r
}

// WithIncludes includes data in the restore.
func (r *Restore) WithIncludes(includes ...string) *Restore {
	for _, include := range includes {
		r.include = append(r.include, couchbasev2.BucketScopeOrCollectionNameWithDefaults(include))
	}

	return r
}

// WithExcludes excludes data from the restore.
func (r *Restore) WithExcludes(excludes ...string) *Restore {
	for _, exclude := range excludes {
		r.exclude = append(r.exclude, couchbasev2.BucketScopeOrCollectionNameWithDefaults(exclude))
	}

	return r
}

// WithMapping adds a mapping from a data source to a new name.
func (r *Restore) WithMapping(source, target string) *Restore {
	r.mapping = append(r.mapping, couchbasev2.RestoreMapping{
		Source: couchbasev2.BucketScopeOrCollectionNameWithDefaults(source),
		Target: couchbasev2.BucketScopeOrCollectionNameWithDefaults(target),
	})

	return r
}

// UseIAM allows restore to use IAM role for backup restoration.
func (r *Restore) UseIAM(useIAM bool) *Restore {
	r.useIAM = useIAM
	return r
}

// WithCustomStoreURL sets the custom url to use
// with backup.spec.objectStore.Endpoint.
func (r *Restore) WithCustomStoreURL(url string) *Restore {
	r.objStoreEndpoint = url

	return r
}

// WithCustomStoreCert sets the custom url to use
// with backup.spec.objectStore.CertSecret.
func (r *Restore) WithCustomStoreCert(secret *v1.Secret) *Restore {
	r.objStoreEndpointCert = secret

	return r
}

// UseBlankBackupName doesn't set spec.backup in the created restore.
func (r *Restore) UseBlankBackupName(useBlankBackupName bool) *Restore {
	r.useBlankBackupName = useBlankBackupName

	return r
}

// MustCreate generates the requested restore and creates it in Kubernetes.
func (r *Restore) MustCreate(t *testing.T, kubernetes *types.Cluster) *couchbasev2.CouchbaseBackupRestore {
	generateName := "restore-"
	falseRef := false

	restore := &couchbasev2.CouchbaseBackupRestore{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: generateName,
		},
		Spec: couchbasev2.CouchbaseBackupRestoreSpec{},
	}

	if !r.useBlankBackupName {
		restore.Spec.Backup = r.backup.Name
	}

	if r.s3Bucket != "" && r.objStore != "" {
		Die(t, fmt.Errorf("object store can not be used with s3 bucket"))
	}

	if r.s3Bucket != "" {
		restore.Spec.S3Bucket = couchbasev2.S3BucketURI(fmt.Sprintf("s3://%s", r.s3Bucket))
	}

	if restore.Spec.ObjectStore == nil {
		restore.Spec.ObjectStore = &couchbasev2.ObjectStoreSpec{}
	}

	if r.objStore != "" {
		restore.Spec.ObjectStore.URI = couchbasev2.ObjectStoreURI(r.objStore)
	}

	if r.objStoreSecret != nil {
		restore.Spec.ObjectStore.Secret = r.objStoreSecret.Name
	}

	if r.objStoreEndpoint != "" {
		restore.Spec.ObjectStore.Endpoint = &couchbasev2.ObjectEndpoint{URL: r.objStoreEndpoint}
		if r.objStoreEndpointCert != nil {
			restore.Spec.ObjectStore.Endpoint.CertSecret = r.objStoreEndpointCert.Name
		}
	}

	if r.useIAM {
		restore.Spec.ObjectStore.UseIAM = &r.useIAM
	}

	if r.withBucketConfig {
		restore.Spec.Services.BucketConfig = true
	}

	if r.withoutData {
		restore.Spec.Services.Data = &falseRef
	}

	if r.withoutGSI {
		restore.Spec.Services.GSIIndex = &falseRef
	}

	if r.withoutEventing {
		restore.Spec.Services.Eventing = &falseRef
	}

	if r.withoutFTAlias {
		restore.Spec.Services.FTAlias = &falseRef
	}

	if r.withoutAnalytics {
		restore.Spec.Services.Analytics = &falseRef
	}

	if r.forceUpdates {
		restore.Spec.ForceUpdates = true
	}

	if r.include != nil || r.exclude != nil || r.mapping != nil {
		restore.Spec.Data = &couchbasev2.CouchbaseBackupRestoreDataFilter{}

		if r.include != nil {
			restore.Spec.Data.Include = r.include
		}

		if r.exclude != nil {
			restore.Spec.Data.Exclude = r.exclude
		}

		if r.mapping != nil {
			restore.Spec.Data.Map = r.mapping
		}
	}

	newRestore, err := kubernetes.CRClient.CouchbaseV2().CouchbaseBackupRestores(kubernetes.Namespace).Create(context.Background(), restore, metav1.CreateOptions{})
	if err != nil {
		Die(t, err)
	}

	return newRestore
}
