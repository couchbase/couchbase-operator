package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	v2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/cluster"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func getBackupImage(f *framework.Framework) string {
	if f.CouchbaseBackupImage != "" {
		return f.CouchbaseBackupImage
	}

	return ""
}

func createTestBackup(strategy v2.Strategy, fullSchedule, incrementalSchedule string) *v2.CouchbaseBackup {
	backup := &v2.CouchbaseBackup{
		ObjectMeta: v1.ObjectMeta{
			Name: strings.Replace(string(strategy), "_", "-", -1),
		},
		Spec: v2.CouchbaseBackupSpec{
			Strategy: strategy,
			Incremental: &v2.CouchbaseBackupSchedule{
				Schedule: incrementalSchedule,
			},
			Full: &v2.CouchbaseBackupSchedule{
				Schedule: fullSchedule,
			},
			Size: e2espec.NewResourceQuantityMi(2048),
		},
	}

	return backup
}

// cronScheduleOnceIn returns a one-time, predictable cron schedule.  It is
// highly recommended that this be at least 2 minutes to give things a chance
// to become created.
func cronScheduleOnceIn(duration time.Duration) string {
	when := time.Now().Add(duration)
	return fmt.Sprintf("%d * * * *", when.Minute())
}

func TestFullIncremental(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := 100

	// Create a normal cluster.
	imageName := getBackupImage(f)
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, clusterSize, imageName)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{bucket.GetName()}, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// Create a Backup object.
	backup := createTestBackup(v2.FullIncremental, cronScheduleOnceIn(2*time.Minute), cronScheduleOnceIn(5*time.Minute))
	backup = e2eutil.MustNewBackup(t, targetKube, backup)
	e2eutil.MustWaitForBackup(t, targetKube, backup, time.Minute)

	// Expect the full backup to complete, followed by the the incremental.
	e2eutil.MustWaitForBackupEvent(t, targetKube, backup, e2eutil.BackupStartedEvent(testCouchbase, backup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, backup, e2eutil.BackupCompletedEvent(testCouchbase, backup.Name), 2*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, backup, e2eutil.BackupStartedEvent(testCouchbase, backup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, backup, e2eutil.BackupCompletedEvent(testCouchbase, backup.Name), 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Incremental)},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestFullOnly(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := 100

	// Create a normal cluster.
	imageName := getBackupImage(f)
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, clusterSize, imageName)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{bucket.GetName()}, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// Create a Backup object.
	backup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(2*time.Minute), "")
	backup = e2eutil.MustNewBackup(t, targetKube, backup)
	e2eutil.MustWaitForBackup(t, targetKube, backup, time.Minute)

	// Expect the full backup to complete.
	e2eutil.MustWaitForBackupEvent(t, targetKube, backup, e2eutil.BackupStartedEvent(testCouchbase, backup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, backup, e2eutil.BackupCompletedEvent(testCouchbase, backup.Name), 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Full)},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Cluster goes down during a backup (all pods go down)
// Tests --purge behaviour is working as expected - this should allow us to ignore the previous backup and start anew.
func TestFailedBackupBehaviour(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2
	clusterName := "test-couchbase-" + e2eutil.RandomSuffix()

	// Create cluster.
	numOfDocs := 100
	testCouchbase := e2espec.NewSupportableCluster(mdsGroupSize)
	testCouchbase.Name = clusterName
	testCouchbase.Spec.Backup.Managed = true

	imageName := getBackupImage(f)
	if imageName = strings.TrimSpace(imageName); imageName != "" {
		testCouchbase.Spec.Backup.Image = imageName
	}

	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{bucket.GetName()}, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// create this backup to run every 2 minutes so we can test the backup still runs successfully after a failure.
	fullBackup := createTestBackup(v2.FullOnly, "*/2 * * * *", "")
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)

	// wait for the backup to start
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 2*time.Minute)

	// kill the cluster
	for i := 0; i < mdsGroupSize; i++ {
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, i, false)
	}

	// backup should fail
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupFailedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)

	// check backup and cronjob are still up
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, time.Minute)
	e2eutil.MustWaitForCronjob(t, targetKube, testCouchbase, string(cluster.Full), time.Minute)

	// Expect the full backup to now complete.
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 10*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Members go down
	// * Members recover
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Full)},
		e2eutil.PodDownWithPVCRecoverySequence(clusterSize, mdsGroupSize),
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Make sure a new Backup PVC comes up if the Backup PVC is deleted (stupidly)
// N.B. Obviously all old data on the old PVC is gone forever and cannot be recovered.
func TestBackupPVCReconcile(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3

	// Create cluster.
	numOfDocs := 100
	imageName := getBackupImage(f)
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, clusterSize, imageName)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{bucket.GetName()}, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// Create a Backup object.
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(7*time.Minute), "")
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, time.Minute)

	// check pvc exists
	pvc, err := targetKube.KubeClient.CoreV1().PersistentVolumeClaims(targetKube.Namespace).Get(fullBackup.Name, v1.GetOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	// check pvc has same name as backup
	if pvc.Name != fullBackup.Name {
		e2eutil.Die(t, fmt.Errorf("pvc name %s is a mismatch with backup name %s", pvc.Name, fullBackup.Name))
	}

	// DELET the PVC!!
	if err := e2eutil.DeleteAndWaitForPVCDeletionSingle(targetKube, pvc.Name, 5*time.Minute); err != nil {
		e2eutil.Die(t, err)
	}

	// check pvc is recreated and exists
	e2eutil.MustWaitForPVC(t, targetKube, fullBackup.Name, 5*time.Minute)

	// Expect backup to complete
	//e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 10*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 10*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Backup deleted
	// * Backup created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Full)},
		eventschema.Event{Reason: k8sutil.EventReasonBackupUpdated, FuzzyMessage: string(cluster.Full)},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// check that replacing a CouchbaseBackup works as expected
// delete CouchbaseBackup which should then delete Cronjobs and Jobs
// create new full-only CouchbaseBackup
// wait for backup to perform
// delete backup
// create new full/incremental CouchbaseBackup.
func TestReplaceFullOnlyBackup(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3

	// Create a normal cluster.
	numOfDocs := 100
	imageName := getBackupImage(f)
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, clusterSize, imageName)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{bucket.GetName()}, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// create initial backup
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(2*time.Minute), "")
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)

	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 2*time.Minute)

	// DELET THIS
	e2eutil.MustDeleteBackup(t, targetKube, fullBackup)
	// wait for backup to be deleted
	e2eutil.MustWaitForBackupDeletion(t, targetKube, 2*time.Minute)

	// create new backup
	fullIncrementalBackup := createTestBackup(v2.FullIncremental, cronScheduleOnceIn(2*time.Minute), cronScheduleOnceIn(5*time.Minute))
	fullIncrementalBackup = e2eutil.MustNewBackup(t, targetKube, fullIncrementalBackup)
	e2eutil.MustWaitForBackup(t, targetKube, fullIncrementalBackup, 2*time.Minute)

	// wait for backups to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupStartedEvent(testCouchbase, fullIncrementalBackup.Name), 10*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullIncrementalBackup.Name), 2*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupStartedEvent(testCouchbase, fullIncrementalBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullIncrementalBackup.Name), 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Backup deleted
	// * Backup created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: fullBackup.Name},
		eventschema.Set{Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonBackupDeleted, FuzzyMessage: fullBackup.Name},
			eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Incremental)},
		}},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// check that replacing a CouchbaseBackup works as expected
// delete CouchbaseBackup which should then delete Cronjobs and Jobs
// create new full/incremental CouchbaseBackup
// wait for backup to perform
// delete backup
// create new full-only CouchbaseBackup.
func TestReplaceFullIncrementalBackup(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3

	// Create a normal cluster.
	numOfDocs := 100
	imageName := getBackupImage(f)
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, clusterSize, imageName)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{bucket.GetName()}, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// create initial backup
	fullIncrementalBackup := createTestBackup(v2.FullIncremental, cronScheduleOnceIn(2*time.Minute), cronScheduleOnceIn(5*time.Minute))
	fullIncrementalBackup = e2eutil.MustNewBackup(t, targetKube, fullIncrementalBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullIncrementalBackup, 2*time.Minute)

	// wait for backups completion and confirm they ran without error
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupStartedEvent(testCouchbase, fullIncrementalBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullIncrementalBackup.Name), 2*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupStartedEvent(testCouchbase, fullIncrementalBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullIncrementalBackup.Name), 2*time.Minute)

	// DELET THIS
	e2eutil.MustDeleteBackup(t, targetKube, fullIncrementalBackup)
	// wait for backup to be deleted
	e2eutil.MustWaitForBackupDeletion(t, targetKube, 2*time.Minute)

	// create new backup
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(2*time.Minute), "")
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)

	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Backup deleted
	// * Backup created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Incremental)},
		eventschema.Set{Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonBackupDeleted, FuzzyMessage: string(cluster.Incremental)},
			eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Full)},
		}},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestBackupAndRestore(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)
	fullFreq := 2

	// Create a normal cluster.
	clusterSize := constants.Size3

	numOfDocs := 100
	imageName := getBackupImage(f)
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, clusterSize, imageName)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{bucket.GetName()}, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(time.Duration(fullFreq)*time.Minute), "")
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)

	// wait for backup to start
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	// wait for backup status update
	repo := e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "Repo", 5*time.Minute)
	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 2*time.Minute)

	latest := "latest"
	// Create a Restore object for later.
	restore := &v2.CouchbaseBackupRestore{
		ObjectMeta: v1.ObjectMeta{
			Name: "test-restore",
		},
		Spec: v2.CouchbaseBackupRestoreSpec{
			Backup: fullBackup.Name,
			Repo:   repo.String(),
			Start: &v2.StrOrInt{
				Str: &latest,
			},
		},
	}

	// delete bucket
	e2eutil.MustDeleteBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, targetKube, testCouchbase, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 10*time.Minute)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{bucket.GetName()}, 5*time.Minute)

	// create new restore
	e2eutil.MustNewBackupRestore(t, targetKube, restore)

	// restore job is too fast, just validate bucket item count
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, time.Minute)

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
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Full)},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Test that CouchbaseBackup Status fields update when the initial backup job is created
// Archive, Repo, RepoList, LastRun, Running will be updated once the job is started
// LastSuccess, RepoList and Duration fields will be updated once the job is finished.
func TestUpdateBackupStatus(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Create a normal cluster.
	clusterSize := constants.Size3

	numOfDocs := 100
	imageName := getBackupImage(f)
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, clusterSize, imageName)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{bucket.GetName()}, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(2*time.Minute), "")
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)

	// wait for backup to start
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "Running", time.Minute)
	e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "LastRun", time.Minute)

	// wait for backup to finish
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 3*time.Minute)

	// wait for backup status update
	e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "Archive", time.Minute)
	e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "Repo", time.Minute)
	e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "Backups", time.Minute)
	e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "LastSuccess", time.Minute)
	e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "Duration", time.Minute)
	e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "Output", time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Full)},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestMultipleBackups(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	clusterSize := constants.Size3
	// Create a normal cluster.
	numOfDocs := 100
	imageName := getBackupImage(f)
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, clusterSize, imageName)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{bucket.GetName()}, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// Create Backup object 1.
	fullIncrementalBackup := createTestBackup(v2.FullIncremental, cronScheduleOnceIn(2*time.Minute), cronScheduleOnceIn(5*time.Minute))
	fullIncrementalBackup = e2eutil.MustNewBackup(t, targetKube, fullIncrementalBackup)

	// Create Backup object 2.
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(7*time.Minute), "")
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backups
	e2eutil.MustWaitForBackup(t, targetKube, fullIncrementalBackup, 2*time.Minute)
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)

	// wait for backups to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupStartedEvent(testCouchbase, fullIncrementalBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullIncrementalBackup.Name), 2*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupStartedEvent(testCouchbase, fullIncrementalBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullIncrementalBackup.Name), 2*time.Minute)

	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 8*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backups created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Set{Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: fullIncrementalBackup.Name},
			eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: fullBackup.Name},
		}},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestFullIncrementalOverTLS(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	clusterSize := constants.Size3
	// Create the cluster.
	numOfDocs := 100

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	imageName := getBackupImage(f)
	testCouchbase := e2espec.NewBackupCluster(clusterSize, imageName)
	testCouchbase.Name = ctx.ClusterName
	testCouchbase.Spec.Networking.TLS = &v2.TLSPolicy{
		Static: &v2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{bucket.GetName()}, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// Create a Backup object.
	fullIncrementalBackup := createTestBackup(v2.FullIncremental, cronScheduleOnceIn(2*time.Minute), cronScheduleOnceIn(5*time.Minute))
	fullIncrementalBackup = e2eutil.MustNewBackup(t, targetKube, fullIncrementalBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullIncrementalBackup, 2*time.Minute)

	// wait for backups to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupStartedEvent(testCouchbase, fullIncrementalBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullIncrementalBackup.Name), 2*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupStartedEvent(testCouchbase, fullIncrementalBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullIncrementalBackup.Name), 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Incremental)},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestFullOnlyOverTLS(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Create the cluster.
	clusterSize := constants.Size3
	numOfDocs := 100

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	imageName := getBackupImage(f)
	testCouchbase := e2espec.NewBackupCluster(clusterSize, imageName)
	testCouchbase.Name = ctx.ClusterName
	testCouchbase.Spec.Networking.TLS = &v2.TLSPolicy{
		Static: &v2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{bucket.GetName()}, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// Create a Backup object.
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(2*time.Minute), "")
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)

	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Full)},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}
