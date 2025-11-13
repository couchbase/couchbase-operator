package e2e

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	v2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil/cloud"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"k8s.io/apimachinery/pkg/api/errors"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	fullBackupResourceName = "full"
)

func testFullIncremental(t *testing.T, providerType cloud.ProviderType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// create provider
	provider := MustNewProvider(t, kubernetes, providerType)
	// setup environment

	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	// Create a normal cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewIncrementalBackup(e2eutil.DefaultSchedule(), e2eutil.ScheduleIn(5*time.Minute)).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)
	e2eutil.MustWaitForBackup(t, kubernetes, backup, time.Minute)

	// Expect the full backup to complete, followed by the the incremental.
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupStartedEvent(cluster, backup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupStartedEvent(cluster, backup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupFullIncremental(t *testing.T) {
	testFullIncremental(t, cloud.NoCloudProvider)
}

func TestBackupFullIncrementalS3(t *testing.T) {
	testFullIncremental(t, cloud.CloudProviderAWS)
}

func TestBackupFullIncrementalAzure(t *testing.T) {
	testFullIncremental(t, cloud.CloudProviderAzure)
}

func TestBackupFullIncrementalGCP(t *testing.T) {
	testFullIncremental(t, cloud.CloudProviderGCP)
}

func testFullOnly(t *testing.T, providerType cloud.ProviderType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, providerType)

	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	// Create a normal cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)
	e2eutil.MustWaitForBackup(t, kubernetes, backup, time.Minute)

	// Expect the full backup to complete.
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupStartedEvent(cluster, backup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupFullOnly(t *testing.T) {
	testFullOnly(t, cloud.NoCloudProvider)
}

func TestBackupFullOnlyS3(t *testing.T) {
	testFullOnly(t, cloud.CloudProviderAWS)
}
func TestBackupFullOnlyAzure(t *testing.T) {
	testFullOnly(t, cloud.CloudProviderAzure)
}

func TestBackupFullOnlyGCP(t *testing.T) {
	testFullOnly(t, cloud.CloudProviderGCP)
}

// Cluster goes down during a backup (all pods go down)
// Tests --purge behaviour is working as expected - this should allow us to ignore the previous backup and start anew.
func testFailedBackupBehaviour(t *testing.T, providerType cloud.ProviderType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, providerType)
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2

	// Create cluster.
	cluster := clusterOptions().WithMixedTopology(mdsGroupSize).Generate(kubernetes)

	cluster.Spec.Backup.Image = f.CouchbaseBackupImage

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup, make a lot, we need this backup to be slow to the point
	// we can vaguely reliably kill pods while it's happening.
	e2eutil.MustPopulateWithDataSize(t, kubernetes, cluster, bucket.GetName(), f.CouchbaseServerImage, 1<<30, time.Minute)

	// create this backup to run every 2 minutes so we can test the backup still runs successfully after a failure.
	backup := e2eutil.NewFullBackup("*/2 * * * *").ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup, 2*time.Minute)

	// Wait for the failure now, the backup has a tendency to fail and raise the event
	// while we are still deleting the pods.
	backupFailWait := e2eutil.WaitForPendingClusterEvent(kubernetes, backup, e2eutil.BackupFailedEvent(cluster, backup.Name), 15*time.Minute)

	// wait for the backup to start
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupStartedEvent(cluster, backup.Name), 10*time.Minute)

	// kill the cluster
	for i := 0; i < mdsGroupSize; i++ {
		e2eutil.MustKillPodForMember(t, kubernetes, cluster, i, false)
	}

	// backup should fail
	e2eutil.MustReceiveErrorValue(t, backupFailWait)

	// check backup and cronjob are still up
	e2eutil.MustWaitForBackup(t, kubernetes, backup, 2*time.Minute)
	e2eutil.MustWaitForCronjob(t, kubernetes, cluster, fullBackupResourceName, time.Minute)

	// Expect the full backup to now complete.
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupStartedEvent(cluster, backup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 15*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Members go down
	// * Members recover
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
		e2eutil.PodDownWithPVCRecoverySequence(clusterSize, mdsGroupSize),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestFailedBackupBehaviour(t *testing.T) {
	testFailedBackupBehaviour(t, cloud.NoCloudProvider)
}

func TestFailedBackupBehaviourS3(t *testing.T) {
	testFailedBackupBehaviour(t, cloud.CloudProviderAWS)
}

func TestFailedBackupBehaviourAzure(t *testing.T) {
	testFailedBackupBehaviour(t, cloud.CloudProviderAzure)
}

func TestFailedBackupBehaviourGCP(t *testing.T) {
	testFailedBackupBehaviour(t, cloud.CloudProviderGCP)
}

// Make sure a new Backup PVC comes up if the Backup PVC is deleted (stupidly)
// N.B. Obviously all old data on the old PVC is gone forever and cannot be recovered.
func testBackupPVCReconcile(t *testing.T, providerType cloud.ProviderType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, providerType)
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3

	// Create cluster.
	numOfDocs := f.DocsCount

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.ScheduleIn(7*time.Minute)).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup, time.Minute)

	// check pvc exists
	pvc, err := kubernetes.KubeClient.CoreV1().PersistentVolumeClaims(kubernetes.Namespace).Get(context.Background(), backup.Name, v1.GetOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	// check pvc has same name as backup
	if pvc.Name != backup.Name {
		e2eutil.Die(t, fmt.Errorf("pvc name %s is a mismatch with backup name %s", pvc.Name, backup.Name))
	}

	// Expect backup to complete
	e2eutil.MustDeletePVC(t, kubernetes, pvc, 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 10*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Backup deleted
	// * Backup created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBackupUpdated, FuzzyMessage: backup.Name},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupPVCReconcile(t *testing.T) {
	testBackupPVCReconcile(t, cloud.NoCloudProvider)
}

func TestBackupPVCReconcileS3(t *testing.T) {
	testBackupPVCReconcile(t, cloud.CloudProviderAWS)
}

func TestBackupPVCReconcileAzure(t *testing.T) {
	testBackupPVCReconcile(t, cloud.CloudProviderAzure)
}

func TestBackupPVCReconcileGCP(t *testing.T) {
	testBackupPVCReconcile(t, cloud.CloudProviderGCP)
}

// check that replacing a CouchbaseBackup works as expected
// delete CouchbaseBackup which should then delete Cronjobs and Jobs
// create new full-only CouchbaseBackup
// wait for backup to perform
// delete backup
// create new full/incremental CouchbaseBackup.
func testReplaceFullOnlyBackup(t *testing.T, providerType cloud.ProviderType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, providerType)
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3

	// Create a normal cluster.
	numOfDocs := f.DocsCount

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// create initial backup
	backup1 := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup1, 2*time.Minute)

	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup1, e2eutil.BackupStartedEvent(cluster, backup1.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup1, e2eutil.BackupCompletedEvent(cluster, backup1.Name), 5*time.Minute)

	// wait for backup to be deleted
	e2eutil.MustDeleteBackup(t, kubernetes, backup1)
	e2eutil.MustWaitForBackupDeletion(t, kubernetes, backup1, 2*time.Minute)

	// create new backup
	backup2 := e2eutil.NewIncrementalBackup(e2eutil.DefaultSchedule(), e2eutil.ScheduleIn(5*time.Minute)).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)
	e2eutil.MustWaitForBackup(t, kubernetes, backup2, 2*time.Minute)

	// wait for backups to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup2, e2eutil.BackupStartedEvent(cluster, backup2.Name), 10*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup2, e2eutil.BackupCompletedEvent(cluster, backup2.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup2, e2eutil.BackupStartedEvent(cluster, backup2.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup2, e2eutil.BackupCompletedEvent(cluster, backup2.Name), 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Backup deleted
	// * Backup created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated},
		eventschema.Set{Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonBackupDeleted},
			eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup2.Name},
		}},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestReplaceFullOnlyBackup(t *testing.T) {
	testReplaceFullOnlyBackup(t, cloud.NoCloudProvider)
}

func TestReplaceFullOnlyBackupS3(t *testing.T) {
	testReplaceFullOnlyBackup(t, cloud.CloudProviderAWS)
}

func TestReplaceFullOnlyBackupAzure(t *testing.T) {
	testReplaceFullOnlyBackup(t, cloud.CloudProviderAzure)
}

func TestReplaceFullOnlyBackupGCP(t *testing.T) {
	testReplaceFullOnlyBackup(t, cloud.CloudProviderGCP)
}

// check that replacing a CouchbaseBackup works as expected
// delete CouchbaseBackup which should then delete Cronjobs and Jobs
// create new full/incremental CouchbaseBackup
// wait for backup to perform
// delete backup
// create new full-only CouchbaseBackup.
func testReplaceFullIncrementalBackup(t *testing.T, providerType cloud.ProviderType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, providerType)
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3

	// Create a normal cluster.
	numOfDocs := f.DocsCount

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// create initial backup
	backup1 := e2eutil.NewIncrementalBackup(e2eutil.DefaultSchedule(), e2eutil.ScheduleIn(5*time.Minute)).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup1, 2*time.Minute)

	// wait for backups completion and confirm they ran without error
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup1, e2eutil.BackupCompletedEvent(cluster, backup1.Name), 7*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup1, e2eutil.BackupCompletedEvent(cluster, backup1.Name), 7*time.Minute)

	// wait for backup to be deleted
	e2eutil.MustDeleteBackup(t, kubernetes, backup1)
	e2eutil.MustWaitForBackupDeletion(t, kubernetes, backup1, 2*time.Minute)

	// create new backup
	backup2 := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)
	e2eutil.MustWaitForBackup(t, kubernetes, backup2, 2*time.Minute)

	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup2, e2eutil.BackupCompletedEvent(cluster, backup2.Name), 10*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Backup deleted
	// * Backup created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup1.Name},
		eventschema.Set{Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonBackupDeleted, FuzzyMessage: backup1.Name},
			eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup2.Name},
		}},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestReplaceFullIncrementalBackup(t *testing.T) {
	testReplaceFullIncrementalBackup(t, cloud.NoCloudProvider)
}

func TestReplaceFullIncrementalBackupS3(t *testing.T) {
	testReplaceFullIncrementalBackup(t, cloud.CloudProviderAWS)
}

func TestReplaceFullIncrementalBackupAzure(t *testing.T) {
	testReplaceFullIncrementalBackup(t, cloud.CloudProviderAzure)
}

func TestReplaceFullIncrementalBackupGCP(t *testing.T) {
	testReplaceFullIncrementalBackup(t, cloud.CloudProviderGCP)
}

func testBackupAndRestore(t *testing.T, providerType cloud.ProviderType, useBlankBackupName bool) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, providerType)

	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Create a normal cluster.
	clusterSize := constants.Size3

	numOfDocs := f.DocsCount
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup, 2*time.Minute)

	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 15*time.Minute)

	// delete bucket
	e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	bucket = e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 5*time.Minute)

	// create new restore
	e2eutil.NewRestore(backup).FromObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).UseBlankBackupName(useBlankBackupName).MustCreate(t, kubernetes)

	// restore job is too fast, just validate bucket item count
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Remove Bucket
	// * Bucket created
	// * Restore created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupAndRestore(t *testing.T) {
	testBackupAndRestore(t, cloud.NoCloudProvider, false)
}

func TestBackupAndRestoreS3(t *testing.T) {
	testBackupAndRestore(t, cloud.CloudProviderAWS, true)
}

func TestBackupAndRestoreAzure(t *testing.T) {
	testBackupAndRestore(t, cloud.CloudProviderAzure, true)
}

func TestBackupAndRestoreGCP(t *testing.T) {
	testBackupAndRestore(t, cloud.CloudProviderGCP, true)
}

// Test that CouchbaseBackup Status fields update when the initial backup job is created
// Archive, Repo, RepoList, LastRun, Running will be updated once the job is started
// LastSuccess, RepoList and Duration fields will be updated once the job is finished.
func testUpdateBackupStatus(t *testing.T, providerType cloud.ProviderType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, providerType)
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Create a normal cluster.
	clusterSize := constants.Size3

	numOfDocs := f.DocsCount

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup, 2*time.Minute)

	// wait for backup to start
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupStartedEvent(cluster, backup.Name), 5*time.Minute)
	// wait for backup to finish
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 3*time.Minute)

	// check for backup status updates
	e2eutil.MustWaitStatusUpdate(t, kubernetes, backup.Name, "LastRun", time.Minute)
	e2eutil.MustWaitStatusUpdate(t, kubernetes, backup.Name, "Archive", time.Minute)
	e2eutil.MustWaitStatusUpdate(t, kubernetes, backup.Name, "Repo", time.Minute)
	e2eutil.MustWaitStatusUpdate(t, kubernetes, backup.Name, "Backups", time.Minute)
	e2eutil.MustWaitStatusUpdate(t, kubernetes, backup.Name, "LastSuccess", time.Minute)
	e2eutil.MustWaitStatusUpdate(t, kubernetes, backup.Name, "Duration", time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestUpdateBackupStatus(t *testing.T) {
	testUpdateBackupStatus(t, cloud.NoCloudProvider)
}

func TestUpdateBackupStatusS3(t *testing.T) {
	testUpdateBackupStatus(t, cloud.CloudProviderAWS)
}

func TestUpdateBackupStatusAzure(t *testing.T) {
	testUpdateBackupStatus(t, cloud.CloudProviderAzure)
}

func TestUpdateBackupStatusGCP(t *testing.T) {
	testUpdateBackupStatus(t, cloud.CloudProviderGCP)
}

func testMultipleBackups(t *testing.T, providerType cloud.ProviderType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, providerType)
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	clusterSize := constants.Size3
	// Create a normal cluster.
	numOfDocs := f.DocsCount

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create Backup object 1.
	backup1 := e2eutil.NewIncrementalBackup(e2eutil.DefaultSchedule(), e2eutil.ScheduleIn(5*time.Minute)).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)

	// Create Backup object 2.
	backup2 := e2eutil.NewFullBackup(e2eutil.ScheduleIn(7*time.Minute)).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)

	// wait for backups
	e2eutil.MustWaitForBackup(t, kubernetes, backup1, 2*time.Minute)
	e2eutil.MustWaitForBackup(t, kubernetes, backup2, 2*time.Minute)

	// wait for backups to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup1, e2eutil.BackupStartedEvent(cluster, backup1.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup1, e2eutil.BackupCompletedEvent(cluster, backup1.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup1, e2eutil.BackupStartedEvent(cluster, backup1.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup1, e2eutil.BackupCompletedEvent(cluster, backup1.Name), 5*time.Minute)

	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup2, e2eutil.BackupStartedEvent(cluster, backup2.Name), 8*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup2, e2eutil.BackupCompletedEvent(cluster, backup2.Name), 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backups created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Set{Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup1.Name},
			eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup2.Name},
		}},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestMultipleBackups(t *testing.T) {
	testMultipleBackups(t, cloud.NoCloudProvider)
}

func TestMultipleBackupsS3(t *testing.T) {
	testMultipleBackups(t, cloud.CloudProviderAWS)
}

func TestMultipleBackupsAzure(t *testing.T) {
	testMultipleBackups(t, cloud.CloudProviderAzure)
}

func TestMultipleBackupsGCP(t *testing.T) {
	testMultipleBackups(t, cloud.CloudProviderGCP)
}

func testFullIncrementalOverTLS(t *testing.T, providerType cloud.ProviderType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, providerType)
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	clusterSize := constants.Size3
	// Create the cluster.
	numOfDocs := f.DocsCount

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewIncrementalBackup(e2eutil.DefaultSchedule(), e2eutil.ScheduleIn(5*time.Minute)).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup, 2*time.Minute)

	// wait for backups to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupStartedEvent(cluster, backup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupStartedEvent(cluster, backup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupFullIncrementalOverTLS(t *testing.T) {
	testFullIncrementalOverTLS(t, cloud.NoCloudProvider)
}

func TestBackupFullIncrementalOverTLSS3(t *testing.T) {
	testFullIncrementalOverTLS(t, cloud.CloudProviderAWS)
}

func TestBackupFullIncrementalOverTLSAzure(t *testing.T) {
	testFullIncrementalOverTLS(t, cloud.CloudProviderAzure)
}

func TestBackupFullIncrementalOverTLSGCP(t *testing.T) {
	testFullIncrementalOverTLS(t, cloud.CloudProviderGCP)
}

func TestBackupFullOnlyOverTLSKubernetes(t *testing.T) {
	testFullOnlyOverTLS(t, cloud.NoCloudProvider, &e2eutil.TLSOpts{Source: e2eutil.TLSSourceKubernetesSecret, MultipleCAs: true}, nil)
}

func testFullOnlyOverTLS(t *testing.T, providerType cloud.ProviderType, tls *e2eutil.TLSOpts, policy *v2.ClientCertificatePolicy) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, providerType)
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Create the cluster.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, tls)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithMutualTLS(ctx, policy).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup, 2*time.Minute)

	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupStartedEvent(cluster, backup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Optional{
			Validator: eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupFullOnlyOverTLS(t *testing.T) {
	testFullOnlyOverTLS(t, cloud.NoCloudProvider, &e2eutil.TLSOpts{}, nil)
}

func TestBackupFullOnlyOverTLSStandard(t *testing.T) {
	keyEncoding := e2eutil.KeyEncodingPKCS8
	opts := &e2eutil.TLSOpts{Source: e2eutil.TLSSourceCertManagerSecret, KeyEncoding: &keyEncoding}

	testFullOnlyOverTLS(t, cloud.NoCloudProvider, opts, nil)
}

func TestBackupFullOnlyOverMutualTLS(t *testing.T) {
	policy := v2.ClientCertificatePolicyEnable

	testFullOnlyOverTLS(t, cloud.NoCloudProvider, &e2eutil.TLSOpts{}, &policy)
}

func TestBackupFullOnlyOverMandatoryMutualTLS(t *testing.T) {
	policy := v2.ClientCertificatePolicyMandatory

	testFullOnlyOverTLS(t, cloud.NoCloudProvider, &e2eutil.TLSOpts{}, &policy)
}

func TestBackupFullOnlyOverTLSS3(t *testing.T) {
	testFullOnlyOverTLS(t, cloud.CloudProviderAWS, &e2eutil.TLSOpts{}, nil)
}
func TestBackupFullOnlyOverTLSAzure(t *testing.T) {
	testFullOnlyOverTLS(t, cloud.CloudProviderAzure, &e2eutil.TLSOpts{}, nil)
}

func TestBackupFullOnlyOverTLSGCP(t *testing.T) {
	testFullOnlyOverTLS(t, cloud.CloudProviderGCP, &e2eutil.TLSOpts{}, nil)
}

func TestBackupFullOnlyOverTLSS3Standard(t *testing.T) {
	keyEncoding := e2eutil.KeyEncodingPKCS8
	opts := &e2eutil.TLSOpts{Source: e2eutil.TLSSourceCertManagerSecret, KeyEncoding: &keyEncoding}

	testFullOnlyOverTLS(t, cloud.CloudProviderAWS, opts, nil)
}

func TestBackupFullOnlyOverTLSAzureStandard(t *testing.T) {
	keyEncoding := e2eutil.KeyEncodingPKCS8
	opts := &e2eutil.TLSOpts{Source: e2eutil.TLSSourceCertManagerSecret, KeyEncoding: &keyEncoding}
	testFullOnlyOverTLS(t, cloud.CloudProviderAzure, opts, nil)
}

func TestBackupFullOnlyOverTLSGCPStandard(t *testing.T) {
	keyEncoding := e2eutil.KeyEncodingPKCS8
	opts := &e2eutil.TLSOpts{Source: e2eutil.TLSSourceCertManagerSecret, KeyEncoding: &keyEncoding}

	testFullOnlyOverTLS(t, cloud.CloudProviderGCP, opts, nil)
}

func testBackupRetention(t *testing.T, providerType cloud.ProviderType) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, providerType)
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// Trigger a full backup.
	backup := e2eutil.NewFullBackup(e2eutil.ScheduleIn(5*time.Minute)).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).WithRetention(time.Minute).MustCreate(t, kubernetes)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 10*time.Minute)

	// Trigger another full backup, the old one should be discarded because it is
	// too old.
	e2eutil.MustPatchBackup(t, kubernetes, backup, jsonpatch.NewPatchSet().Replace("/spec/full/schedule", e2eutil.ScheduleIn(2*time.Minute)), time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 10*time.Minute)

	// And one more time to make sure the discard didn't do anything stupid...
	e2eutil.MustPatchBackup(t, kubernetes, backup, jsonpatch.NewPatchSet().Replace("/spec/full/schedule", e2eutil.ScheduleIn(2*time.Minute)), time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 10*time.Minute)
}

func TestBackupRetention(t *testing.T) {
	testBackupRetention(t, cloud.NoCloudProvider)
}

func TestBackupRetentionS3(t *testing.T) {
	testBackupRetention(t, cloud.CloudProviderAWS)
}

func TestBackupRetentionAzure(t *testing.T) {
	testBackupRetention(t, cloud.CloudProviderAzure)
}

func TestBackupRetentionGCP(t *testing.T) {
	testBackupRetention(t, cloud.CloudProviderGCP)
}

// Manually editing the size of the PVC in a CouchbaseBackup should be reflected in the PVC and PV
// N.B. Requires a SC with allowVolumeExpansion: true.
func testBackupPVCResize(t *testing.T, providerType cloud.ProviderType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, providerType)
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster().ExpandableStorage()

	// Static configuration.
	clusterSize := 3

	// Create cluster.
	numOfDocs := 100

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup, time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 10*time.Minute)

	// edit CouchbaseBackup to trigger another backup which then triggers the PVC resize
	newBackupSize := backup.Spec.Size.DeepCopy()
	newBackupSize.Add(newBackupSize)

	patchset := jsonpatch.NewPatchSet().
		Replace("/spec/full/schedule", e2eutil.ScheduleIn(4*time.Minute)).
		Replace("/spec/size", newBackupSize)
	e2eutil.MustPatchBackup(t, kubernetes, backup, patchset, time.Minute)

	// Expect backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 10*time.Minute)

	// check pvc has been resized
	e2eutil.MustWaitForPVCSize(t, kubernetes, backup.Name, &newBackupSize, 3*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Backup updated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBackupUpdated, FuzzyMessage: backup.Name},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupPVCResize(t *testing.T) {
	testBackupPVCResize(t, cloud.NoCloudProvider)
}

func TestBackupPVCResizeS3(t *testing.T) {
	testBackupPVCResize(t, cloud.CloudProviderAWS)
}

func TestBackupPVCResizeAzure(t *testing.T) {
	testBackupPVCResize(t, cloud.CloudProviderAzure)
}

func TestBackupPVCResizeGCP(t *testing.T) {
	testBackupPVCResize(t, cloud.CloudProviderGCP)
}

// TestBackupAutoscaling populates the database with a load of data, then backs it up.
// We check the capacity and then ensure that by setting the right thresholds, it
// resizes the volume as we need it.
func TestBackupAutoscaling(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).ExpandableStorage()

	clusterSize := 3
	initialVolumeSize := e2espec.NewResourceQuantityMi(1024)
	volumeSizeLimit := e2espec.NewResourceQuantityMi(1024 + 512)

	// Create a 2 node cluster, with 1Gi of memory per node...
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ClusterSettings.DataServiceMemQuota = e2espec.NewResourceQuantityMi(1024)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Create a bucket with 1Gi per node, and no replicas...
	bucket := e2espec.DefaultBucket()
	bucket.Spec.MemoryQuota = initialVolumeSize
	bucket.Spec.Replicas = 0

	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// The stage is set, we have 3Gi of document storage to play around with.
	// First up, populate the cluster with a sizable amount of documentation.
	// When backed up, it will shrink by something like 5x.  So 1Gi of data
	// will be 200Mi of backup, on 1Gi of volume, which is a good 20% of the
	// space...
	e2eutil.MustPopulateWithDataSize(t, kubernetes, cluster, bucket.Name, f.CouchbaseServerImage, 1<<30, time.Minute)

	// Schedule the backup to happen in 2 minutes time, and expect it to complete...
	// at some point, this may take a while given VMs may need to be created to schedule
	// the thing.  Importantly, we set the threshold at 99% (e.g. we need 99% of the
	// volume to be free), we increment by 200% (triple the existing 1Gi), and put a limit
	// in of 1.5Gi. We wait for the backup to be updated
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).WithSize(e2espec.NewResourceQuantityMi(1024)).WithAutoscaling(volumeSizeLimit, 99, 200).MustCreate(t, kubernetes)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, k8sutil.BackupUpdateEvent(backup.Name, cluster), 20*time.Minute)

	// Trigger another backup, this will cause volume expansion when it's remounted.
	// Also reduce the threshold down so we don't trigger another update unnecessarily,
	// and also test that another update doesn't happen erroneously.  Beware of rounding,
	// My request for 1.5Gi turned into 2Gi in reality!!
	patchset := jsonpatch.NewPatchSet().
		Replace("/spec/full/schedule", e2eutil.ScheduleIn(2*time.Minute)).
		Replace("/spec/autoScaling/thresholdPercent", 1)

	e2eutil.MustPatchBackup(t, kubernetes, backup, patchset, time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 20*time.Minute)
	e2eutil.MustWaitForPVCNotSize(t, kubernetes, backup.Name, initialVolumeSize, time.Minute)

	// Check the events match what we expect:
	// * Cluster created.
	// * Bucket created.
	// * Backup created.
	// * PVC resized after the backup reported low space (auto update).
	// * Backup rescheduled and occurred (update).
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated},
		eventschema.Repeat{
			Times:     2,
			Validator: eventschema.Event{Reason: k8sutil.EventReasonBackupUpdated},
		},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func testBackupAndRestoreDisableEventing(t *testing.T, providerType cloud.ProviderType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, providerType)
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3
	dataQuota := e2espec.NewResourceQuantityMi(int64(256 * 3))
	buckets := []v1.Object{}
	numOfDocs := f.DocsCount

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)

	cluster.Spec.Servers[0].Services = append(cluster.Spec.Servers[0].Services, v2.EventingService)
	cluster.Spec.ClusterSettings.DataServiceMemQuota = dataQuota
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	for i := 0; i < 3; i++ {
		bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
		bucket.SetName(fmt.Sprintf("%s-%d", bucket.GetName(), i))
		buckets = append(buckets, bucket)
		e2eutil.MustNewBucket(t, kubernetes, bucket)
		e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)
	}

	// create eventing function and verify
	e2eutil.MustDeployEventingFunction(t, cluster, "test", buckets[0].GetName(), buckets[1].GetName(), buckets[2].GetName(), function, time.Minute)
	e2eutil.NewDocumentSet(buckets[0].GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, buckets[2].GetName(), f.DocsCount, 5*time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup, 2*time.Minute)

	// wait for backup to start
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupStartedEvent(cluster, backup.Name), 5*time.Minute)
	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// delete bucket
	for _, bucket := range buckets {
		e2eutil.MustDeleteBucket(t, kubernetes, bucket)
		e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)
	}

	// delete the eventing function
	e2eutil.MustDeleteEventingFunction(t, cluster, time.Minute)
	time.Sleep(30 * time.Second)

	for _, bucket := range buckets {
		e2eutil.MustNewBucket(t, kubernetes, bucket)
		e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)
	}

	// create new restore
	e2eutil.NewRestore(backup).FromObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).WithoutEventing().MustCreate(t, kubernetes)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// if eventing function is restored raise error.
	if err := e2eutil.MustGetEventingFunction(t, cluster, time.Minute); err == nil {
		e2eutil.Die(t, err)
	}

	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, buckets[0].GetName(), f.DocsCount, 5*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, buckets[2].GetName(), f.DocsCount, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Remove Bucket
	// * Bucket created
	// * Restore created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: len(buckets), Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketCreated}},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
		eventschema.Repeat{Times: len(buckets), Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted}},
		eventschema.Repeat{Times: len(buckets), Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketCreated}},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupAndRestoreDisableEventing(t *testing.T) {
	testBackupAndRestoreDisableEventing(t, cloud.NoCloudProvider)
}

func TestBackupAndRestoreDisableEventingS3(t *testing.T) {
	testBackupAndRestoreDisableEventing(t, cloud.CloudProviderAWS)
}

func TestBackupAndRestoreDisableEventingAzure(t *testing.T) {
	testBackupAndRestoreDisableEventing(t, cloud.CloudProviderAzure)
}

func TestBackupAndRestoreDisableEventingGCP(t *testing.T) {
	testBackupAndRestoreDisableEventing(t, cloud.CloudProviderGCP)
}

func testBackupAndRestoreDisableGSI(t *testing.T, providerType cloud.ProviderType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, providerType)
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// populate the bucket
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// create eventing function and verify
	query := "CREATE PRIMARY INDEX `#primary` ON `default` USING GSI"
	e2eutil.MustExecuteIndexQuery(t, kubernetes, cluster, query, time.Minute)

	// verify index creation
	time.Sleep(20 * time.Second)
	indexCount := e2eutil.MustGetIndexCount(t, kubernetes, cluster, 2*time.Minute)

	if indexCount == 0 {
		e2eutil.Die(t, fmt.Errorf("Index `#primary` not created"))
	}

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup, 2*time.Minute)

	// wait for backup to start
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupStartedEvent(cluster, backup.Name), 5*time.Minute)
	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// delete bucket
	e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// create new restore
	e2eutil.NewRestore(backup).FromObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).WithoutGSI().MustCreate(t, kubernetes)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check Index is not restored.
	indexCount = e2eutil.MustGetIndexCount(t, kubernetes, cluster, 2*time.Minute)
	if indexCount != 0 {
		e2eutil.Die(t, fmt.Errorf("Index `#primary` restored"))
	}

	expectedDocs := numOfDocs

	// 7.6 adds docs in the system scope
	if atleast76, _ := cluster.IsAtLeastVersion("7.6.0"); atleast76 {
		expectedDocs += 2
	}

	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), expectedDocs, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Remove Bucket
	// * Bucket created
	// * Restore created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupAndRestoreDisableGSI(t *testing.T) {
	testBackupAndRestoreDisableGSI(t, cloud.NoCloudProvider)
}

func TestBackupAndRestoreDisableGSIS3(t *testing.T) {
	testBackupAndRestoreDisableGSI(t, cloud.CloudProviderAWS)
}

func TestBackupAndRestoreDisableGSIAzure(t *testing.T) {
	testBackupAndRestoreDisableGSI(t, cloud.CloudProviderAzure)
}

func TestBackupAndRestoreDisableGSIGCP(t *testing.T) {
	testBackupAndRestoreDisableGSI(t, cloud.CloudProviderGCP)
}

func testBackupAndRestoreDisableAnalytics(t *testing.T, providerType cloud.ProviderType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, providerType)
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	numOfDocs := f.DocsCount
	clusterSize := constants.Size3

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)

	cluster.Spec.Servers[0].Services = append(cluster.Spec.Servers[0].Services, v2.AnalyticsService)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// populate the bucket
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// create eventing function and verify
	analyticsDataset := "testDataset1"
	queries := []string{
		"CREATE DATASET " + analyticsDataset + " ON `default`",
		"CONNECT LINK Local",
	}

	for _, query := range queries {
		e2eutil.MustExecuteAnalyticsQuery(t, kubernetes, cluster, query, time.Minute)
	}

	time.Sleep(time.Minute) // let analytics catch up
	datasetItemCount := e2eutil.MustGetDatasetItemCount(t, kubernetes, cluster, analyticsDataset, time.Minute)

	if datasetItemCount != int64(numOfDocs) {
		e2eutil.Die(t, fmt.Errorf("dataset item mismatch %v/%v", datasetItemCount, numOfDocs))
	}

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup, 2*time.Minute)

	// wait for backup to start
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupStartedEvent(cluster, backup.Name), 5*time.Minute)
	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// delete bucket
	e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

	// drop dataset
	query := "DROP DATASET " + analyticsDataset
	e2eutil.MustExecuteAnalyticsQuery(t, kubernetes, cluster, query, time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// create new restore
	e2eutil.NewRestore(backup).FromObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).WithoutAnalytics().MustCreate(t, kubernetes)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	datasetCount := e2eutil.MustGetDatasetCount(t, kubernetes, cluster, time.Minute)
	if datasetCount != 0 {
		e2eutil.Die(t, fmt.Errorf("dataset restored"))
	}

	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Remove Bucket
	// * Bucket created
	// * Restore created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupAndRestoreDisableAnalytics(t *testing.T) {
	testBackupAndRestoreDisableAnalytics(t, cloud.NoCloudProvider)
}

func TestBackupAndRestoreDisableAnalyticsS3(t *testing.T) {
	testBackupAndRestoreDisableAnalytics(t, cloud.CloudProviderAWS)
}

func TestBackupAndRestoreDisableAnalyticsAzure(t *testing.T) {
	testBackupAndRestoreDisableAnalytics(t, cloud.CloudProviderAzure)
}

func TestBackupAndRestoreDisableAnalyticsGCP(t *testing.T) {
	testBackupAndRestoreDisableAnalytics(t, cloud.CloudProviderGCP)
}

func testBackupAndRestoreDisableData(t *testing.T, providerType cloud.ProviderType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, providerType)
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup, 2*time.Minute)

	// wait for backup to start
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupStartedEvent(cluster, backup.Name), 5*time.Minute)
	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// delete bucket
	e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 5*time.Minute)

	// create new restore
	e2eutil.NewRestore(backup).FromObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).WithoutData().MustCreate(t, kubernetes)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// restore job is too fast, just validate that no items were restored.
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), 0, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Remove Bucket
	// * Bucket created
	// * Restore created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupAndRestoreDisableData(t *testing.T) {
	testBackupAndRestoreDisableData(t, cloud.NoCloudProvider)
}

func TestBackupAndRestoreDisableDataS3(t *testing.T) {
	testBackupAndRestoreDisableData(t, cloud.CloudProviderAWS)
}

func TestBackupAndRestoreDisableDataAzure(t *testing.T) {
	testBackupAndRestoreDisableData(t, cloud.CloudProviderAzure)
}

func TestBackupAndRestoreDisableDataGCP(t *testing.T) {
	testBackupAndRestoreDisableData(t, cloud.CloudProviderGCP)
}

func testBackupAndRestoreEnableBucketConfig(t *testing.T, providerType cloud.ProviderType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, providerType)
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	numOfDocs := f.DocsCount
	clusterSize := constants.Size3

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup, 2*time.Minute)

	// wait for backup to start
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupStartedEvent(cluster, backup.Name), 5*time.Minute)
	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	newBucket := e2espec.DefaultBucketTwoReplicas()
	e2eutil.MustNewBucket(t, kubernetes, newBucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, newBucket, 2*time.Minute)

	// create new restore
	e2eutil.NewRestore(backup).FromObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).WithBucketConfig().MustCreate(t, kubernetes)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// verify replica count was restored.
	e2eutil.MustVerifyReplicaCount(t, kubernetes, cluster, newBucket.GetName(), 5*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Remove Bucket
	// * Bucket created
	// * Restore created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
		eventschema.Optional{
			Validator: eventschema.Sequence{
				Validators: []eventschema.Validatable{
					eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
					eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
				},
			},
		},
		eventschema.Event{Reason: k8sutil.EventReasonBucketEdited},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupAndRestoreEnableBucketConfig(t *testing.T) {
	testBackupAndRestoreEnableBucketConfig(t, cloud.NoCloudProvider)
}

func TestBackupAndRestoreEnableBucketConfigS3(t *testing.T) {
	testBackupAndRestoreEnableBucketConfig(t, cloud.CloudProviderAWS)
}

func TestBackupAndRestoreEnableBucketConfigAzure(t *testing.T) {
	testBackupAndRestoreEnableBucketConfig(t, cloud.CloudProviderAzure)
}

func TestBackupAndRestoreEnableBucketConfigGCP(t *testing.T) {
	testBackupAndRestoreEnableBucketConfig(t, cloud.CloudProviderGCP)
}

func testBackupAndRestoreMapBuckets(t *testing.T, providerType cloud.ProviderType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, providerType)
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	targetBucketName := "bucketty-mcbuccketface"
	// Create a normal cluster.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup, 2*time.Minute)

	// wait for backup to start
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupStartedEvent(cluster, backup.Name), 5*time.Minute)
	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// delete bucket
	e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

	newBucket := e2espec.DefaultBucket()
	newBucket.SetName(targetBucketName)
	e2eutil.MustNewBucket(t, kubernetes, newBucket)

	// create new restore
	e2eutil.NewRestore(backup).FromObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).WithMapping(bucket.GetName(), targetBucketName).MustCreate(t, kubernetes)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, newBucket.GetName(), f.DocsCount, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Remove Bucket
	// * Bucket created
	// * Restore created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupAndRestoreMapBuckets(t *testing.T) {
	testBackupAndRestoreMapBuckets(t, cloud.NoCloudProvider)
}

func TestBackupAndRestoreMapBucketsS3(t *testing.T) {
	testBackupAndRestoreMapBuckets(t, cloud.CloudProviderAWS)
}

func TestBackupAndRestoreMapBucketsAzure(t *testing.T) {
	testBackupAndRestoreMapBuckets(t, cloud.CloudProviderAzure)
}

func TestBackupAndRestoreMapBucketsGCP(t *testing.T) {
	testBackupAndRestoreMapBuckets(t, cloud.CloudProviderGCP)
}

func testBackupAndRestoreIncludeBuckets(t *testing.T, providerType cloud.ProviderType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, providerType)
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Create a normal cluster.
	clusterSize := constants.Size3
	buckets := []v1.Object{}
	numOfDocs := f.DocsCount

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ClusterSettings.DataServiceMemQuota = e2espec.NewResourceQuantityMi(int64(2 * 256))
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	for i := 0; i < 2; i++ {
		bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
		bucket.SetName(fmt.Sprintf("%s-%d", bucket.GetName(), i))
		buckets = append(buckets, bucket)
		e2eutil.MustNewBucket(t, kubernetes, bucket)
		e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)
		e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
		e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), f.DocsCount, time.Minute)
	}

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup, 2*time.Minute)
	// wait for backup to start
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupStartedEvent(cluster, backup.Name), 5*time.Minute)
	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// delete bucket and then create
	for _, bucket := range buckets {
		e2eutil.MustDeleteBucket(t, kubernetes, bucket)
		e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

		e2eutil.MustNewBucket(t, kubernetes, bucket)
		e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)
	}

	// create new restore
	e2eutil.NewRestore(backup).FromObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).WithIncludes(buckets[0].GetName()).MustCreate(t, kubernetes)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, buckets[0].GetName(), f.DocsCount, time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, buckets[1].GetName(), 0, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Remove Bucket
	// * Bucket created
	// * Restore created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: len(buckets), Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketCreated}},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupAndRestoreIncludeBuckets(t *testing.T) {
	testBackupAndRestoreIncludeBuckets(t, cloud.NoCloudProvider)
}

func TestBackupAndRestoreIncludeBucketsS3(t *testing.T) {
	testBackupAndRestoreIncludeBuckets(t, cloud.CloudProviderAWS)
}

func TestBackupAndRestoreIncludeBucketsAzure(t *testing.T) {
	testBackupAndRestoreIncludeBuckets(t, cloud.CloudProviderAzure)
}

func TestBackupAndRestoreIncludeBucketsGCP(t *testing.T) {
	testBackupAndRestoreIncludeBuckets(t, cloud.CloudProviderGCP)
}

func testBackupAndRestoreExcludeBuckets(t *testing.T, providerType cloud.ProviderType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, providerType)
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Create a normal cluster.
	clusterSize := constants.Size3
	buckets := []v1.Object{}
	numOfDocs := f.DocsCount

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ClusterSettings.DataServiceMemQuota = e2espec.NewResourceQuantityMi(int64(2 * 256))
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	for i := 0; i < 2; i++ {
		bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
		bucket.SetName(fmt.Sprintf("%s-%d", bucket.GetName(), i))
		buckets = append(buckets, bucket)

		e2eutil.MustNewBucket(t, kubernetes, bucket)
		e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

		// Populate the bucket
		e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
		e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), f.DocsCount, time.Minute)
	}

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup, 2*time.Minute)
	// wait for backup to start
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupStartedEvent(cluster, backup.Name), 5*time.Minute)
	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// delete buckets and then create it.
	for _, bucket := range buckets {
		e2eutil.MustDeleteBucket(t, kubernetes, bucket)
		e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

		e2eutil.MustNewBucket(t, kubernetes, bucket)
		e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)
	}

	// create new restore
	e2eutil.NewRestore(backup).FromObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).WithExcludes(buckets[0].GetName()).MustCreate(t, kubernetes)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, buckets[0].GetName(), 0, time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, buckets[1].GetName(), f.DocsCount, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Remove Bucket
	// * Bucket created
	// * Restore created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: len(buckets), Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketCreated}},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupAndRestoreExcludeBuckets(t *testing.T) {
	testBackupAndRestoreExcludeBuckets(t, cloud.NoCloudProvider)
}

func TestBackupAndRestoreExcludeBucketsS3(t *testing.T) {
	testBackupAndRestoreExcludeBuckets(t, cloud.CloudProviderAWS)
}

func TestBackupAndRestoreExcludeBucketsAzure(t *testing.T) {
	testBackupAndRestoreExcludeBuckets(t, cloud.CloudProviderAzure)
}

func TestBackupAndRestoreExcludeBucketsGCP(t *testing.T) {
	testBackupAndRestoreExcludeBuckets(t, cloud.CloudProviderGCP)
}

func testBackupAndRestoreNodeSelector(t *testing.T, providerType cloud.ProviderType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	nodes, err := kubernetes.KubeClient.CoreV1().Nodes().List(context.Background(), v1.ListOptions{})
	if err != nil {
		e2eutil.Die(t, fmt.Errorf("failed to get node list: %w", err))
	}

	provider := MustNewProvider(t, kubernetes, providerType)
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)

	zone, ok := nodes.Items[0].Labels[constants.FailureDomainZoneLabel]

	if !ok {
		e2eutil.Die(nil, fmt.Errorf("node %s missing label %s", nodes.Items[0].Name, constants.FailureDomainZoneLabel))
	}

	nodeSelector := map[string]string{
		constants.FailureDomainZoneLabel: zone,
	}

	cluster.Spec.Backup.NodeSelector = nodeSelector
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).WithStorageClass(f.StorageClassName).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup, 2*time.Minute)

	// wait for backup to start
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupStartedEvent(cluster, backup.Name), 5*time.Minute)
	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// delete bucket
	e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 5*time.Minute)

	// create new restore
	e2eutil.NewRestore(backup).FromObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// restore job is too fast, just validate that no items were restored.
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), 0, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Remove Bucket
	// * Bucket created
	// * Restore created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupAndRestoreNodeSelector(t *testing.T) {
	testBackupAndRestoreNodeSelector(t, cloud.NoCloudProvider)
}

func TestBackupAndRestoreNodeSelectorS3(t *testing.T) {
	testBackupAndRestoreNodeSelector(t, cloud.CloudProviderAWS)
}

func TestBackupAndRestoreNodeSelectorAzure(t *testing.T) {
	testBackupAndRestoreNodeSelector(t, cloud.CloudProviderAzure)
}

func TestBackupAndRestoreNodeSelectorGCP(t *testing.T) {
	testBackupAndRestoreNodeSelector(t, cloud.CloudProviderGCP)
}

// TestBackupBucketInclusion tests we can selectively include a bucket in the backup.
func TestBackupBucketInclusion(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	clusterSize := 1

	// Create two buckets.
	// TODO: The typing in types.Cluster need to change.
	bucket1 := e2eutil.NewBucket(f.BucketType).WithCompressionMode(f.CompressionMode).WithFlush().MustCreate(t, kubernetes)
	bucket2 := e2eutil.NewBucket(f.BucketType).WithCompressionMode(f.CompressionMode).WithFlush().MustCreate(t, kubernetes)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket1, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket2, time.Minute)

	// Add some data to both buckets.
	e2eutil.NewDocumentSet(bucket1.GetName(), f.DocsCount).MustCreate(t, kubernetes, cluster)
	e2eutil.NewDocumentSet(bucket2.GetName(), f.DocsCount).MustCreate(t, kubernetes, cluster)

	// Create and wait for a backup
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).Include(bucket1).MustCreate(t, kubernetes)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// Flush the buckets.
	e2eutil.MustFlushBucket(t, kubernetes, cluster, bucket1, time.Minute)
	e2eutil.MustFlushBucket(t, kubernetes, cluster, bucket2, time.Minute)

	// Create and wait for restore.
	restore := e2eutil.NewRestore(backup).MustCreate(t, kubernetes)
	e2eutil.MustWaitForResourceDeletion(t, kubernetes, restore, time.Minute)

	// Expect bucket1 to be included, and bucket2 to be ignored.
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket1.GetName(), f.DocsCount, time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket2.GetName(), 0, time.Minute)
}

// TestBackupBucketExclusion tests we can selectively exclude a bucket from the backup.
func TestBackupBucketExclusion(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	clusterSize := 1

	// Create two buckets.
	bucket1 := e2eutil.NewBucket(f.BucketType).WithCompressionMode(f.CompressionMode).WithFlush().MustCreate(t, kubernetes)
	bucket2 := e2eutil.NewBucket(f.BucketType).WithCompressionMode(f.CompressionMode).WithFlush().MustCreate(t, kubernetes)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket1, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket2, time.Minute)

	// Add some data to both buckets.
	e2eutil.NewDocumentSet(bucket1.GetName(), f.DocsCount).MustCreate(t, kubernetes, cluster)
	e2eutil.NewDocumentSet(bucket2.GetName(), f.DocsCount).MustCreate(t, kubernetes, cluster)

	// Create and wait for a backup
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).Exclude(bucket2).MustCreate(t, kubernetes)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// Flush the buckets.
	e2eutil.MustFlushBucket(t, kubernetes, cluster, bucket1, time.Minute)
	e2eutil.MustFlushBucket(t, kubernetes, cluster, bucket2, time.Minute)

	// Create and wait for restore.
	restore := e2eutil.NewRestore(backup).MustCreate(t, kubernetes)
	e2eutil.MustWaitForResourceDeletion(t, kubernetes, restore, time.Minute)

	// Expect bucket1 to be implicitly included, and bucket2 to be explicitly excluded.
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket1.GetName(), f.DocsCount, time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket2.GetName(), 0, time.Minute)
}

// testBackupAndRestoreScopesAndCollection tests data backup taken of a bucket
// with managed scopes and collections,
// is restored successfully in a managed bucket with unmanaged scopes and collections.
func testBackupAndRestoreScopesAndCollections(t *testing.T, providerType cloud.ProviderType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, providerType)
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	// Static configuration.
	clusterSize := constants.Size1
	scopeName := "pinky"
	collectionName := "brain"
	numOfDocs := f.DocsCount

	// Create a collection and collection group.
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithCollections(collection).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithIgnoreSystemScope().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollection(collectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// insert docs in the created scope.collection.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, collectionName).MustCreate(t, kubernetes, cluster)

	// Create and wait for a backup
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// delete bucket
	e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)
	bucket = e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	// create new restore
	e2eutil.NewRestore(backup).FromObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// restore job is too fast, just validate that all items were restored in collection.
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, cluster, bucket.GetName(), scopeName, collectionName, numOfDocs, 10*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created with Scopes and Collections.
	// * Backup created
	// * Remove Bucket
	// * Bucket created
	// * Restore created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated, FuzzyMessage: bucket.GetName()},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupAndRestoreScopesAndCollections(t *testing.T) {
	testBackupAndRestoreScopesAndCollections(t, cloud.NoCloudProvider)
}

func TestBackupAndRestoreScopesAndCollectionsS3(t *testing.T) {
	testBackupAndRestoreScopesAndCollections(t, cloud.CloudProviderAWS)
}

func TestBackupAndRestoreScopesAndCollectionsAzure(t *testing.T) {
	testBackupAndRestoreScopesAndCollections(t, cloud.CloudProviderAzure)
}

func TestBackupAndRestoreScopesAndCollectionsGCP(t *testing.T) {
	testBackupAndRestoreScopesAndCollections(t, cloud.CloudProviderGCP)
}

// testBackupAndRestoreCollection tests data backup taken of a bucket
// with managed scopes and collections,
// is restored successfully in a managed bucket with managed scopes
// and unmanaged collection.
func testBackupAndRestoreCollections(t *testing.T, providerType cloud.ProviderType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, providerType)
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	// Static configuration.
	clusterSize := constants.Size1
	scopeName := "pinky"
	collectionName := "brain"
	numOfDocs := f.DocsCount

	// Create a collection and collection group.
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithCollections(collection).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithIgnoreSystemScope().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollection(collectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// insert docs in the created scope.collection.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, collectionName).MustCreate(t, kubernetes, cluster)

	// Create and wait for a backup
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// delete bucket
	e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

	// deleting bucket will not delete the underlying k8s couchbasescope resource
	// delete it explicitly.
	e2eutil.MustDeleteScope(t, kubernetes, scopeName)

	// Create the scope with no collection.
	scopeNew := e2eutil.NewScope(scopeName).MustCreate(t, kubernetes)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	bucket = e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scopeNew)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	// create new restore
	e2eutil.NewRestore(backup).FromObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// restore job is too fast, just validate that all items were restored in collection.
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, cluster, bucket.GetName(), scopeName, collectionName, numOfDocs, 10*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created with Scopes and Collections.
	// * Backup created
	// * Remove Bucket
	// * Bucket created
	// * Restore created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated, FuzzyMessage: bucket.GetName()},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Optional{
			Validator: eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated, FuzzyMessage: bucket.GetName()},
		},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupAndRestoreCollections(t *testing.T) {
	testBackupAndRestoreCollections(t, cloud.NoCloudProvider)
}

func TestBackupAndRestoreCollectionsS3(t *testing.T) {
	testBackupAndRestoreCollections(t, cloud.CloudProviderAWS)
}

func TestBackupAndRestoreCollectionsAzure(t *testing.T) {
	testBackupAndRestoreCollections(t, cloud.CloudProviderAzure)
}

func TestBackupAndRestoreCollectionsGCP(t *testing.T) {
	testBackupAndRestoreCollections(t, cloud.CloudProviderGCP)
}

// testBackupAndRestoreScope tests data backup taken of a managed scope
// is restored successfully in an unmanaged scope with the same name.
func testBackupAndRestoreScope(t *testing.T, providerType cloud.ProviderType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, providerType)
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket().PlatformIs(v2.PlatformTypeAWS)

	// Static configuration.
	clusterSize := constants.Size1
	scopeName := "pinky"
	collectionName := "brain"
	numOfDocs := f.DocsCount

	// Create a collection and collection group.
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithCollections(collection).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithIgnoreSystemScope().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollection(collectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// insert docs.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, collectionName).MustCreate(t, kubernetes, cluster)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)

	// take backup of a single scope.
	backupInclude := fmt.Sprintf("%s.%s", bucket.GetName(), scopeName)

	// Create and wait for a backup
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).Include(backupInclude).MustCreate(t, kubernetes)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// delete bucket
	e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)
	bucket = e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	// create new restore
	e2eutil.NewRestore(backup).FromObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// restore job is too fast, just validate that all items were restored in collection.
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, cluster, bucket.GetName(), scopeName, collectionName, numOfDocs, 10*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created with Scopes and Collections.
	// * Backup created
	// * Remove Bucket
	// * Bucket created
	// * Restore created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated, FuzzyMessage: bucket.GetName()},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupAndRestoreScope(t *testing.T) {
	testBackupAndRestoreScope(t, cloud.NoCloudProvider)
}

func TestBackupAndRestoreScopeS3(t *testing.T) {
	testBackupAndRestoreScope(t, cloud.CloudProviderAWS)
}

func TestBackupAndRestoreScopeAzure(t *testing.T) {
	testBackupAndRestoreScope(t, cloud.CloudProviderAzure)
}

func TestBackupAndRestoreScopeGCP(t *testing.T) {
	testBackupAndRestoreScope(t, cloud.CloudProviderGCP)
}

// testBackupAndRestoreCollection tests data backup taken of a managed collection
// is restored successfully in an unmanaged collection with the same name.
func testBackupAndRestoreCollection(t *testing.T, providerType cloud.ProviderType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, providerType)
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	// Static configuration.
	clusterSize := constants.Size1
	scopeName := "pinky"
	collectionName1 := "brain"
	collectionName2 := "heart"

	numOfDocs := f.DocsCount

	// Create a collection.
	collection1 := e2eutil.NewCollection(collectionName1).MustCreate(t, kubernetes)
	collection2 := e2eutil.NewCollection(collectionName2).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithCollections(collection1, collection2).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithIgnoreSystemScope().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName1, collectionName2)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// insert docs in the created scope.collection.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, collectionName1).MustCreate(t, kubernetes, cluster)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, collectionName2).MustCreate(t, kubernetes, cluster)

	// take backup of a single scope.
	backupInclude := fmt.Sprintf("%s.%s.%s", bucket.GetName(), scopeName, collectionName1)

	// Create and wait for a backup
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).Include(backupInclude).MustCreate(t, kubernetes)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// delete bucket
	e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)
	bucket = e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	// create new restore
	e2eutil.NewRestore(backup).FromObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// restore job is too fast, just validate that all items were restored in included collection only.
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, cluster, bucket.GetName(), scopeName, collectionName1, numOfDocs, 10*time.Minute)
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, cluster, bucket.GetName(), scopeName, collectionName2, 0, 10*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created with Scopes and Collections.
	// * Backup created
	// * Remove Bucket
	// * Bucket created
	// * Restore created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated, FuzzyMessage: bucket.GetName()},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupAndRestoreCollection(t *testing.T) {
	testBackupAndRestoreCollection(t, cloud.NoCloudProvider)
}

func TestBackupAndRestoreCollectionS3(t *testing.T) {
	testBackupAndRestoreCollection(t, cloud.CloudProviderAWS)
}

func TestBackupAndRestoreCollectionAzure(t *testing.T) {
	testBackupAndRestoreCollection(t, cloud.CloudProviderAzure)
}

func TestBackupAndRestoreCollectionGCP(t *testing.T) {
	testBackupAndRestoreCollection(t, cloud.CloudProviderGCP)
}

// TestBackupThenDelete tests that a backup resource can be created
// and prematurely deleted before the cronjob runs.
func TestBackupThenDelete(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	// Create a normal cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewIncrementalBackup(e2eutil.DefaultSchedule(), e2eutil.ScheduleIn(5*time.Minute)).MustCreate(t, kubernetes)
	e2eutil.MustWaitForBackup(t, kubernetes, backup, time.Minute)

	// Delete the backup object.
	err := e2eutil.DeleteBackup(kubernetes, backup)
	if err != nil {
		e2eutil.Die(t, err)
	}

	// // Check the events match what we expect:
	// // * Cluster created
	// // * Bucket created
	// // * Backup created
	// // * Backup deleted
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupDeleted},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupCustomObjEndpoint(t *testing.T) {
	testBackupCustomObjectEndpoint(t, false, false)
}

func TestBackupCustomObjEndpointWithCert(t *testing.T) {
	testBackupCustomObjectEndpoint(t, true, false)
}

func TestBackupLegacyCustomObjEndpointWithCert(t *testing.T) {
	testBackupCustomObjectEndpoint(t, true, true)
}

func TestBackupLegacyCustomObjEndpoint(t *testing.T) {
	testBackupCustomObjectEndpoint(t, false, true)
}

// testbackupCustomObjectEndpoint tests backup compatibility with
// AWS compliant object stores and with an optional custom CA
// cert to verify the endpoint with.
func testBackupCustomObjectEndpoint(t *testing.T, withCA, legacy bool) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	numOfDocs := f.DocsCount

	framework.Requires(t, kubernetes).StaticCluster()

	minio, err := e2eutil.MinioOptions(kubernetes).WithName("minio").WithTLS(withCA).WithCredentials(f.MinioAccessKey, f.MinioSecretID, f.MinioRegion).Create()
	defer minio.CleanUp()

	if err != nil {
		e2eutil.Die(t, err)
	}

	minio.MustWaitUntilReady(t, 5*time.Minute)

	var cert []byte
	if withCA {
		cert = minio.CASecret.Data["tls.crt"]
	}

	s3secret, s3BucketName, s3cleanup := createObjEndpointS3Secret(t, kubernetes, minio.Endpoint, cert)
	defer s3cleanup()

	clusterOptions := clusterOptions().WithEphemeralTopology(clusterSize)
	if legacy {
		clusterOptions.WithS3(s3secret).WithObjEndpoint(minio.Endpoint).WithObjEndpointCert(minio.CASecret)
	}

	cluster := clusterOptions.MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)

	// Create bucket.
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// Insert docs to backup.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backupOptions := e2eutil.NewFullBackup(e2eutil.DefaultSchedule())
	if legacy {
		backupOptions.ToS3(s3BucketName)
	} else {
		backupOptions.WithObjStoreSecret(s3secret).ToObjStore("s3://" + s3BucketName).WithCustomStoreURL(minio.Endpoint).WithCustomStoreCert(minio.CASecret)
	}

	backup := backupOptions.MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup, 2*time.Minute)

	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 12*time.Minute)

	// delete bucket
	e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 5*time.Minute)

	// create new restore
	if legacy {
		e2eutil.NewRestore(backup).FromS3(s3BucketName).MustCreate(t, kubernetes)
	} else {
		e2eutil.NewRestore(backup).FromObjStore("s3://"+s3BucketName).WithObjStoreSecret(s3secret).WithCustomStoreURL(minio.Endpoint).WithCustomStoreCert(minio.CASecret).MustCreate(t, kubernetes)
	}

	// restore job is too fast, just validate bucket item count
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Remove Bucket
	// * Bucket created
	// * Restore created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestBackupAndRestoreS3WithIAMRole tests backup functionality
// when not using explicit credentials and instead applying an
// IAMRole to backup. Only runs in AWS.
func TestBackupAndRestoreS3WithIAMRole(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3RegionSecret(t, kubernetes)

	defer s3cleanup()

	framework.Requires(t, kubernetes).StaticCluster().HasIAMParameters()

	// Create a normal cluster.
	clusterSize := constants.Size1

	numOfDocs := f.DocsCount
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	aws := e2eutil.AwsHelper(f.IAMAccessKey, f.IAMSecretID, f.S3Region).Create()
	e2eutil.MustSetupBackupIAM(t, kubernetes, aws, f.AWSAccountID, f.AWSOIDCProvider, s3BucketName)

	defer aws.Cleanup()

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).WithUseIAM(true).WithObjStoreSecret(s3secret).ToObjStore("s3://"+s3BucketName).MustCreate(t, kubernetes)

	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 12*time.Minute)

	// delete bucket
	e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	bucket = e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 5*time.Minute)

	// create new restore
	e2eutil.NewRestore(backup).FromS3(s3BucketName).UseIAM(true).MustCreate(t, kubernetes)

	// restore job is too fast, just validate bucket item count
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Remove Bucket
	// * Bucket created
	// * Restore created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestBackupAndForcedRestore tests forced restore functionality.
// That when selected, a restore will overwrite the "newer" document.
func TestBackupAndForcedRestore(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().AtLeastBackupVersion("1.3.0")

	// Create a normal cluster.
	clusterSize := constants.Size3

	numOfDocs := f.DocsCount
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	docSet := e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs)
	docSet.WithValue("key1", "dummyVal1").MustCreate(t, kubernetes, cluster)

	contents := map[string]interface{}{
		"key1": "dummyVal1",
	}

	err := e2eutil.VerifyContents(600*time.Second, docSet, contents)
	if err != nil {
		e2eutil.Die(t, err)
	}

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup, 2*time.Minute)

	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 15*time.Minute)

	// update the data in the bucket
	docSet.WithValue("key1", "thatsnew").MustCreate(t, kubernetes, cluster)

	// create new restore
	e2eutil.NewRestore(backup).WithForcedUpdates().MustCreate(t, kubernetes)

	err = e2eutil.VerifyContents(600*time.Second, docSet, contents)
	if err != nil {
		e2eutil.Die(t, err)
	}

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Restore created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupAndRestoreServices(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	// Create a normal cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).WithoutFTAlias().MustCreate(t, kubernetes)
	e2eutil.MustWaitForBackup(t, kubernetes, backup, time.Minute)

	// Expect the full backup to complete.
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupStartedEvent(cluster, backup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	e2eutil.NewRestore(backup).WithoutFTAlias().MustCreate(t, kubernetes)
	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupAndRestoreToSubPath(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, cloud.CloudProviderAWS)
	s3secret, s3BucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	s3BucketName += "/subpath"

	framework.Requires(t, kubernetes).StaticCluster()

	// Create a normal cluster.
	clusterSize := constants.Size3

	numOfDocs := f.DocsCount
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToS3(s3BucketName).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup, 2*time.Minute)

	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 15*time.Minute)

	// delete bucket
	e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	bucket = e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 5*time.Minute)

	// create new restore
	e2eutil.NewRestore(backup).FromS3(s3BucketName).MustCreate(t, kubernetes)

	// restore job is too fast, just validate bucket item count
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Remove Bucket
	// * Bucket created
	// * Restore created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupAndRestoreEphemeralVolume(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, cloud.CloudProviderAWS)

	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Create a normal cluster.
	clusterSize := constants.Size1

	numOfDocs := f.DocsCount
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithEphemeralVolume().WithJobTTL(int32(0)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup, 2*time.Minute)

	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 15*time.Minute)

	// // check if the ephemeral volume exists
	_, err := e2eutil.FindBackupEphemeralVolume(kubernetes, backup.Name)
	if err != nil {
		e2eutil.Die(t, err)
	}

	// it should die because of TTL
	callback := func() error {
		if _, err := e2eutil.FindBackupEphemeralVolume(kubernetes, backup.Name); err != nil {
			return nil
		}

		return fmt.Errorf("found ephemeral backup volume, but did not expect one for job %s", backup.Name)
	}
	// check if the pvc has gone
	if err := retryutil.RetryFor(15*time.Second, callback); err != nil {
		e2eutil.Die(t, err)
	}

	// delete bucket
	e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	bucket = e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 5*time.Minute)

	// create new restore
	e2eutil.NewRestore(backup).FromObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)

	// restore job is too fast, just validate bucket item count
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Remove Bucket
	// * Bucket created
	// * Restore created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupAndRestoreUsers(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().AtLeastVersion("7.6.0").AtLeastBackupVersion("1.4.0")

	// Static configuration.
	clusterSize := constants.Size3

	// Create a normal cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Security.RBAC.Managed = false
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	users := []*couchbaseutil.User{
		{
			Name:     "user1",
			ID:       "user1",
			Domain:   "local",
			Password: "password1",
		},
		{
			Name:     "user2",
			ID:       "user2",
			Domain:   "local",
			Password: "password2",
		},
	}

	// Create the users
	e2eutil.MustCreateUsers(t, kubernetes, cluster, users...)

	e2eutil.MustWaitUntilUsersExist(t, kubernetes, cluster, users, 2*time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).WithoutFTAlias().WithUseIAM(true).WithUsers(true).MustCreate(t, kubernetes)
	e2eutil.MustWaitForBackup(t, kubernetes, backup, time.Minute)

	// Expect the full backup to complete.
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupStartedEvent(cluster, backup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// Delete after the backup has been taken.
	e2eutil.MustDeleteUsers(t, kubernetes, cluster, users...)

	e2eutil.NewRestore(backup).WithoutFTAlias().WithUsers(true).MustCreate(t, kubernetes)

	e2eutil.MustWaitUntilUsersExist(t, kubernetes, cluster, users, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Restore created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestPeriodicMergeBackup(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().AtLeastBackupVersion("1.4.3")

	// Create a normal cluster.
	clusterSize := constants.Size3

	numOfDocs := f.DocsCount
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewPeriodicMergeBackup(e2eutil.ScheduleXWithYIntervalInZ(2, 1, 3*time.Minute), e2eutil.ScheduleIn(6*time.Minute)).MustCreate(t, kubernetes)

	// wait for the immediate full backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// wait for the incremental backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// wait for the merge backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupMergeCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestAutoCreateBucketOnRestore(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().AtLeastBackupVersion("1.4.3")

	// Create a normal cluster.
	clusterSize := constants.Size1

	numOfDocs := f.DocsCount
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup, 2*time.Minute)

	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 15*time.Minute)

	// delete bucket
	e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

	// Go unmanaged
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/buckets/managed", false), time.Minute)
	time.Sleep(30 * time.Second)

	// create new restore
	e2eutil.NewRestore(backup).UseBlankBackupName(false).MustCreate(t, kubernetes)

	// wait for the bucket to be recreated
	e2eutil.MustWaitUntilUnmanagedBucketExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

	// restore job is too fast, just validate bucket item count
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Remove Bucket
	// * Bucket created
	// * Restore created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupRemoveLockfileAnnotation(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, cloud.NoCloudProvider)

	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster().AtLeastBackupVersion("1.4.3")

	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	// Create a normal cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup("*/1 * * * *").ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).WithAnnotations(map[string]string{"cao.couchbase.com/forceDeleteLockfile": "true"}).MustCreate(t, kubernetes)
	e2eutil.MustWaitForBackup(t, kubernetes, backup, time.Minute)

	cronJob, err := kubernetes.KubeClient.BatchV1().CronJobs(backup.Namespace).Get(context.Background(), backup.Name+"-full", v1.GetOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	// Check the force delete lockfile argument is present
	if !slices.Contains(cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Args, "--force-delete-lockfile") {
		e2eutil.Die(t, fmt.Errorf("expected --force-delete-lockfile in cron job args"))
	}

	// Wait for a backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// Wait for the backup to be updated
	e2eutil.MustObserveClusterEvent(t, kubernetes, cluster, e2eutil.BackupUpdatedEvent(cluster, backup.Name), 5*time.Minute)

	cronJob, err = kubernetes.KubeClient.BatchV1().CronJobs(backup.Namespace).Get(context.Background(), backup.Name+"-full", v1.GetOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	// Check the force delete lockfile argument is no longer present
	if slices.Contains(cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Args, "--force-delete-lockfile") {
		e2eutil.Die(t, fmt.Errorf("Unexpected --force-delete-lockfile in cron job args"))
	}
}

func TestBackupAndKeepRestore(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, cloud.NoCloudProvider)

	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster().AtLeastBackupVersion("1.4.3")

	// Create a normal cluster.
	clusterSize := constants.Size3

	numOfDocs := f.DocsCount
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup, 2*time.Minute)

	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 15*time.Minute)

	// delete bucket
	e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	bucket = e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 5*time.Minute)

	// create new restore
	restore := e2eutil.NewRestore(backup).FromObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).UseBlankBackupName(false).WithPreserveRestoreRecord(true).MustCreate(t, kubernetes)

	// restore job is too fast, just validate bucket item count
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	time.Sleep(20 * time.Second)

	r, err := kubernetes.CRClient.CouchbaseV2().CouchbaseBackupRestores(backup.Namespace).Get(context.Background(), restore.Name, v1.GetOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	if r == nil {
		e2eutil.Die(t, fmt.Errorf("Restore was not preserved"))
	}

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Remove Bucket
	// * Bucket created
	// * Restore created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: backup.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupAndRestorePreserveRecord(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	provider := MustNewProvider(t, kubernetes, cloud.NoCloudProvider)

	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Create a normal cluster.
	clusterSize := constants.Size3

	numOfDocs := f.DocsCount
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup, 2*time.Minute)

	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 15*time.Minute)

	// delete bucket
	e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	bucket = e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 5*time.Minute)

	t.Run("PreserveRestoreRecord_True", func(t *testing.T) {
		cleanup := f.SetupSubTest(t)
		defer cleanup()

		// create new restore with preserveRestoreRecord: true
		restore := e2eutil.NewRestore(backup).FromObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).UseBlankBackupName(false).WithPreserveRestoreRecord(true).MustCreate(t, kubernetes)

		// restore job is too fast, just validate bucket item count
		e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

		// Wait for restore job to complete
		time.Sleep(30 * time.Second)

		// Verify that the restore record was preserved
		r, err := kubernetes.CRClient.CouchbaseV2().CouchbaseBackupRestores(backup.Namespace).Get(context.Background(), restore.Name, v1.GetOptions{})
		if err != nil {
			e2eutil.Die(t, fmt.Errorf("Failed to get restore record: %w", err))
		}

		if r == nil {
			e2eutil.Die(t, fmt.Errorf("Restore record should have been preserved but was not found"))
		}

		// Cleanup the restore record manually since it was preserved
		err = kubernetes.CRClient.CouchbaseV2().CouchbaseBackupRestores(backup.Namespace).Delete(context.Background(), restore.Name, v1.DeleteOptions{})
		if err != nil {
			e2eutil.Die(t, fmt.Errorf("Failed to cleanup preserved restore record: %w", err))
		}
	})

	t.Run("PreserveRestoreRecord_False", func(t *testing.T) {
		cleanup := f.SetupSubTest(t)
		defer cleanup()

		// create new restore with default behavior (preserveRestoreRecord: false)
		restore := e2eutil.NewRestore(backup).FromObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).UseBlankBackupName(false).MustCreate(t, kubernetes)

		// restore job is too fast, just validate bucket item count
		e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

		// Wait longer to ensure the restore job completes and cleanup happens
		time.Sleep(30 * time.Second)

		// Verify that the restore record was deleted (default behavior)
		_, err := kubernetes.CRClient.CouchbaseV2().CouchbaseBackupRestores(backup.Namespace).Get(context.Background(), restore.Name, v1.GetOptions{})
		if err == nil {
			e2eutil.Die(t, fmt.Errorf("Restore record should have been deleted but still exists"))
		}

		// Check that we get a NotFound error, which is expected
		if !errors.IsNotFound(err) {
			e2eutil.Die(t, fmt.Errorf("Expected NotFound error but got: %w", err))
		}
	})
}

func TestBackupAndRestoreAdditionalArgs(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().AtLeastBackupVersion("1.4.3")

	// create provider
	provider := MustNewProvider(t, kubernetes, cloud.NoCloudProvider)

	// setup environmen
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size1

	// Create a normal cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetCouchstoreBucket(f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).WithAnnotations(map[string]string{"cao.couchbase.com/additionalArgs": "--default-recovery=resume"}).MustCreate(t, kubernetes)
	e2eutil.MustWaitForBackup(t, kubernetes, backup, time.Minute)

	container := e2eutil.MustGetBackupContainer(t, kubernetes, backup)

	if !slices.Contains(container.Args, "--default-recovery=resume") {
		e2eutil.Die(t, fmt.Errorf("expected --default-recovery=resume in backup container args"))
	}

	restore := e2eutil.NewRestore(backup).FromObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).WithAnnotations(map[string]string{"cao.couchbase.com/additionalArgs": "--default-recovery=purge"}).UseBlankBackupName(false).MustCreate(t, kubernetes)

	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, k8sutil.BackupRestoreCreateEvent(restore.Name, cluster), 2*time.Minute)
	container = e2eutil.MustGetRestoreContainer(t, kubernetes, restore)

	if !slices.Contains(container.Args, "--default-recovery=purge") {
		e2eutil.Die(t, fmt.Errorf("expected --default-recovery=purge in restore container args"))
	}
}

func TestRestoreDefaultRecoveryMethod(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().AtLeastBackupVersion("1.4.3")

	// create provider
	provider := MustNewProvider(t, kubernetes, cloud.NoCloudProvider)

	// setup environmen
	objStoreSecret, bucketName, storeCleanup := provider.SetupEnvironment(t, kubernetes)

	defer storeCleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size1

	// Create a normal cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetCouchstoreBucket(f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).MustCreate(t, kubernetes)
	e2eutil.MustWaitForBackup(t, kubernetes, backup, time.Minute)

	restore := e2eutil.NewRestore(backup).FromObjStore(provider.PrefixBucket(bucketName)).WithObjStoreSecret(objStoreSecret).WithDefaultRecoveryMethod(v2.DefaultRecoveryTypePurge).UseBlankBackupName(false).MustCreate(t, kubernetes)

	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, k8sutil.BackupRestoreCreateEvent(restore.Name, cluster), 2*time.Minute)
	container := e2eutil.MustGetRestoreContainer(t, kubernetes, restore)

	found := false
	for i, arg := range container.Args {
		if arg == "--default-recovery" {
			if container.Args[i+1] == string(v2.DefaultRecoveryTypePurge) {
				found = true
				break
			}

			e2eutil.Die(t, fmt.Errorf("expected '--default-recovery' and 'purge' in restore container args"))
		}
	}

	if !found {
		e2eutil.Die(t, fmt.Errorf("expected '--default-recovery' and 'purge' in restore container args"))
	}
}
