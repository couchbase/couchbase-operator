package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	v2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/cluster"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func createTestBackup(strategy v2.Strategy, fullSchedule, incrementalSchedule string, s3 bool) *v2.CouchbaseBackup {
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

	if s3 {
		backup.Spec.S3Bucket = framework.Global.S3Bucket
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

func createS3Secret(t *testing.T, targetKube *types.Cluster, s3 bool) *corev1.Secret {
	if !s3 {
		return nil
	}

	f := framework.Global

	var s3secret string = "s3-secret"

	secret := &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{
			Name: s3secret,
		},
		Data: map[string][]byte{
			"region":            []byte(f.S3Region),
			"access-key-id":     []byte(f.S3AccessKey),
			"secret-access-key": []byte(f.S3SecretID),
		},
	}

	if _, err := targetKube.KubeClient.CoreV1().Secrets(targetKube.Namespace).Create(secret); err != nil {
		e2eutil.Die(t, err)
	}

	return secret
}

func skipBackup(t *testing.T, s3 bool) {
	f := framework.Global

	versionStr, err := k8sutil.CouchbaseVersion(f.CouchbaseServerImage)
	if err != nil {
		e2eutil.Die(t, err)
	}

	version, err := couchbaseutil.NewVersion(versionStr)
	if err != nil {
		e2eutil.Die(t, err)
	}

	if version.Semver() != "6.6.0" && s3 {
		t.Skip("Backup to S3 is a 6.6.0 feature")
	}

	versionStr, err = k8sutil.CouchbaseVersion(f.CouchbaseBackupImage)
	if err != nil {
		e2eutil.Die(t, err)
	}

	version, err = couchbaseutil.NewVersion(versionStr)
	if err != nil {
		e2eutil.Die(t, err)
	}

	if version.Major() >= 6 && version.Minor() <= 5 && s3 {
		t.Skip("S3 supported with 6.6.0 images only")
	}

	if (f.S3Bucket == "" || f.S3AccessKey == "" || f.S3SecretID == "") && s3 {
		t.Skip("Either of S3 Bucket/AccessKey/SecretId missing")
	}
}

func testFullIncremental(t *testing.T, s3 bool) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipBackup(t, s3)

	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	s3secret := createS3Secret(t, targetKube, s3)

	// Create a normal cluster.
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, clusterSize, f.CouchbaseBackupImage, s3secret)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 3*time.Minute)

	// Create a Backup object.
	backup := createTestBackup(v2.FullIncremental, cronScheduleOnceIn(2*time.Minute), cronScheduleOnceIn(5*time.Minute), s3)
	backup = e2eutil.MustNewBackup(t, targetKube, backup)
	e2eutil.MustWaitForBackup(t, targetKube, backup, time.Minute)

	// Expect the full backup to complete, followed by the the incremental.
	e2eutil.MustWaitForBackupEvent(t, targetKube, backup, e2eutil.BackupStartedEvent(testCouchbase, backup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, backup, e2eutil.BackupCompletedEvent(testCouchbase, backup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, backup, e2eutil.BackupStartedEvent(testCouchbase, backup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, backup, e2eutil.BackupCompletedEvent(testCouchbase, backup.Name), 5*time.Minute)

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

func TestFullIncremental(t *testing.T) {
	testFullIncremental(t, false)
}

func TestFullIncrementalS3(t *testing.T) {
	testFullIncremental(t, true)
}

func testFullOnly(t *testing.T, s3 bool) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipBackup(t, s3)

	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	s3secret := createS3Secret(t, targetKube, s3)

	// Create a normal cluster.
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, clusterSize, f.CouchbaseBackupImage, s3secret)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// Create a Backup object.
	backup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(2*time.Minute), "", s3)
	backup = e2eutil.MustNewBackup(t, targetKube, backup)
	e2eutil.MustWaitForBackup(t, targetKube, backup, time.Minute)

	// Expect the full backup to complete.
	e2eutil.MustWaitForBackupEvent(t, targetKube, backup, e2eutil.BackupStartedEvent(testCouchbase, backup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, backup, e2eutil.BackupCompletedEvent(testCouchbase, backup.Name), 5*time.Minute)

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

func TestFullOnly(t *testing.T) {
	testFullOnly(t, false)
}

func TestFullOnlyS3(t *testing.T) {
	testFullOnly(t, true)
}

// Cluster goes down during a backup (all pods go down)
// Tests --purge behaviour is working as expected - this should allow us to ignore the previous backup and start anew.
func testFailedBackupBehaviour(t *testing.T, s3 bool) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipBackup(t, s3)

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2
	clusterName := "test-couchbase-" + e2eutil.RandomSuffix()

	s3secret := createS3Secret(t, targetKube, s3)

	// Create cluster.
	numOfDocs := f.DocsCount
	testCouchbase := e2espec.NewSupportableCluster(mdsGroupSize)
	testCouchbase.Name = clusterName
	testCouchbase.Spec.Backup.Managed = true

	if s3secret != nil {
		testCouchbase.Spec.Backup.S3Secret = s3secret.Name
	}

	testCouchbase.Spec.Backup.Image = f.CouchbaseBackupImage

	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// create this backup to run every 2 minutes so we can test the backup still runs successfully after a failure.
	fullBackup := createTestBackup(v2.FullOnly, "*/2 * * * *", "", s3)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)

	// wait for the backup to start
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 10*time.Minute)

	// kill the cluster
	for i := 0; i < mdsGroupSize; i++ {
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, i, false)
	}

	// backup should fail
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupFailedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)

	// check backup and cronjob are still up
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)
	e2eutil.MustWaitForCronjob(t, targetKube, testCouchbase, string(cluster.Full), time.Minute)

	// Expect the full backup to now complete.
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 15*time.Minute)

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

func TestFailedBackupBehaviour(t *testing.T) {
	testFailedBackupBehaviour(t, false)
}

func TestFailedBackupBehaviourS3(t *testing.T) {
	testFailedBackupBehaviour(t, true)
}

// Make sure a new Backup PVC comes up if the Backup PVC is deleted (stupidly)
// N.B. Obviously all old data on the old PVC is gone forever and cannot be recovered.
func testBackupPVCReconcile(t *testing.T, s3 bool) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipBackup(t, s3)

	// Static configuration.
	clusterSize := constants.Size3

	// Create cluster.
	numOfDocs := f.DocsCount

	s3secret := createS3Secret(t, targetKube, s3)

	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, clusterSize, f.CouchbaseBackupImage, s3secret)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// Create a Backup object.
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(7*time.Minute), "", s3)
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

func TestBackupPVCReconcile(t *testing.T) {
	testBackupPVCReconcile(t, false)
}

func TestBackupPVCReconcileS3(t *testing.T) {
	testBackupPVCReconcile(t, true)
}

// check that replacing a CouchbaseBackup works as expected
// delete CouchbaseBackup which should then delete Cronjobs and Jobs
// create new full-only CouchbaseBackup
// wait for backup to perform
// delete backup
// create new full/incremental CouchbaseBackup.
func testReplaceFullOnlyBackup(t *testing.T, s3 bool) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipBackup(t, s3)

	// Static configuration.
	clusterSize := constants.Size3

	// Create a normal cluster.
	numOfDocs := f.DocsCount

	s3secret := createS3Secret(t, targetKube, s3)

	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, clusterSize, f.CouchbaseBackupImage, s3secret)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// create initial backup
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(2*time.Minute), "", s3)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)

	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)

	// DELET THIS
	e2eutil.MustDeleteBackup(t, targetKube, fullBackup)
	// wait for backup to be deleted
	e2eutil.MustWaitForBackupDeletion(t, targetKube, 2*time.Minute)

	// create new backup
	fullIncrementalBackup := createTestBackup(v2.FullIncremental, cronScheduleOnceIn(2*time.Minute), cronScheduleOnceIn(5*time.Minute), s3)
	fullIncrementalBackup = e2eutil.MustNewBackup(t, targetKube, fullIncrementalBackup)
	e2eutil.MustWaitForBackup(t, targetKube, fullIncrementalBackup, 2*time.Minute)

	// wait for backups to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupStartedEvent(testCouchbase, fullIncrementalBackup.Name), 10*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullIncrementalBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupStartedEvent(testCouchbase, fullIncrementalBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullIncrementalBackup.Name), 5*time.Minute)

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

func TestReplaceFullOnlyBackup(t *testing.T) {
	testReplaceFullOnlyBackup(t, false)
}

func TestReplaceFullOnlyBackupS3(t *testing.T) {
	testReplaceFullOnlyBackup(t, true)
}

// check that replacing a CouchbaseBackup works as expected
// delete CouchbaseBackup which should then delete Cronjobs and Jobs
// create new full/incremental CouchbaseBackup
// wait for backup to perform
// delete backup
// create new full-only CouchbaseBackup.
func testReplaceFullIncrementalBackup(t *testing.T, s3 bool) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipBackup(t, s3)

	// Static configuration.
	clusterSize := constants.Size3

	// Create a normal cluster.
	numOfDocs := f.DocsCount

	s3secret := createS3Secret(t, targetKube, s3)

	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, clusterSize, f.CouchbaseBackupImage, s3secret)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// create initial backup
	fullIncrementalBackup := createTestBackup(v2.FullIncremental, cronScheduleOnceIn(2*time.Minute), cronScheduleOnceIn(5*time.Minute), s3)
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
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(2*time.Minute), "", s3)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)

	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)

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

func TestReplaceFullIncrementalBackup(t *testing.T) {
	testReplaceFullIncrementalBackup(t, false)
}

func TestReplaceFullIncrementalBackupS3(t *testing.T) {
	testReplaceFullIncrementalBackup(t, true)
}

func testBackupAndRestore(t *testing.T, s3 bool) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipBackup(t, s3)

	fullFreq := 2

	// Create a normal cluster.
	clusterSize := constants.Size3

	numOfDocs := f.DocsCount

	s3secret := createS3Secret(t, targetKube, s3)

	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, clusterSize, f.CouchbaseBackupImage, s3secret)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 5*time.Minute)

	// Create a Backup object.
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(time.Duration(fullFreq)*time.Minute), "", s3)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)

	// wait for backup to start
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	// wait for backup status update
	repo := e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "Repo", 5*time.Minute)

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

	if f.S3Bucket != "" && s3 {
		restore.Spec.S3Bucket = f.S3Bucket
	}

	// delete bucket
	e2eutil.MustDeleteBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, targetKube, testCouchbase, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 10*time.Minute)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 5*time.Minute)

	// create new restore
	e2eutil.MustNewBackupRestore(t, targetKube, restore)

	// restore job is too fast, just validate bucket item count
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 5*time.Minute)

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

func TestBackupAndRestore(t *testing.T) {
	testBackupAndRestore(t, false)
}

func TestBackupAndRestoreS3(t *testing.T) {
	testBackupAndRestore(t, true)
}

// Test that CouchbaseBackup Status fields update when the initial backup job is created
// Archive, Repo, RepoList, LastRun, Running will be updated once the job is started
// LastSuccess, RepoList and Duration fields will be updated once the job is finished.
func testUpdateBackupStatus(t *testing.T, s3 bool) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipBackup(t, s3)

	// Create a normal cluster.
	clusterSize := constants.Size3

	numOfDocs := f.DocsCount

	s3secret := createS3Secret(t, targetKube, s3)

	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, clusterSize, f.CouchbaseBackupImage, s3secret)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(2*time.Minute), "", s3)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)

	// wait for backup to start
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	// wait for backup to finish
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 3*time.Minute)

	// check for backup status updates
	e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "LastRun", time.Minute)
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

func TestUpdateBackupStatus(t *testing.T) {
	testUpdateBackupStatus(t, false)
}

func TestUpdateBackupStatusS3(t *testing.T) {
	testUpdateBackupStatus(t, true)
}

func testMultipleBackups(t *testing.T, s3 bool) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipBackup(t, s3)

	clusterSize := constants.Size3
	// Create a normal cluster.
	numOfDocs := f.DocsCount

	s3secret := createS3Secret(t, targetKube, s3)

	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, clusterSize, f.CouchbaseBackupImage, s3secret)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// Create Backup object 1.
	fullIncrementalBackup := createTestBackup(v2.FullIncremental, cronScheduleOnceIn(2*time.Minute), cronScheduleOnceIn(5*time.Minute), s3)
	fullIncrementalBackup = e2eutil.MustNewBackup(t, targetKube, fullIncrementalBackup)

	// Create Backup object 2.
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(7*time.Minute), "", s3)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backups
	e2eutil.MustWaitForBackup(t, targetKube, fullIncrementalBackup, 2*time.Minute)
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)

	// wait for backups to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupStartedEvent(testCouchbase, fullIncrementalBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullIncrementalBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupStartedEvent(testCouchbase, fullIncrementalBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullIncrementalBackup.Name), 5*time.Minute)

	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 8*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)

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

func TestMultipleBackups(t *testing.T) {
	testMultipleBackups(t, false)
}

func TestMultipleBackupsS3(t *testing.T) {
	testMultipleBackups(t, true)
}

func testFullIncrementalOverTLS(t *testing.T, s3 bool) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipBackup(t, s3)

	clusterSize := constants.Size3
	// Create the cluster.
	numOfDocs := f.DocsCount

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	s3secret := createS3Secret(t, targetKube, s3)

	testCouchbase := e2espec.NewBackupCluster(clusterSize, f.CouchbaseBackupImage, s3secret)
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
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// Create a Backup object.
	fullIncrementalBackup := createTestBackup(v2.FullIncremental, cronScheduleOnceIn(2*time.Minute), cronScheduleOnceIn(5*time.Minute), s3)
	fullIncrementalBackup = e2eutil.MustNewBackup(t, targetKube, fullIncrementalBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullIncrementalBackup, 2*time.Minute)

	// wait for backups to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupStartedEvent(testCouchbase, fullIncrementalBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullIncrementalBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupStartedEvent(testCouchbase, fullIncrementalBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullIncrementalBackup.Name), 5*time.Minute)

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

func TestFullIncrementalOverTLS(t *testing.T) {
	testFullIncrementalOverTLS(t, false)
}

func TestFullIncrementalOverTLSS3(t *testing.T) {
	testFullIncrementalOverTLS(t, true)
}

func testFullOnlyOverTLS(t *testing.T, s3 bool) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipBackup(t, s3)

	// Create the cluster.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	s3secret := createS3Secret(t, targetKube, s3)

	testCouchbase := e2espec.NewBackupCluster(clusterSize, f.CouchbaseBackupImage, s3secret)
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
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// Create a Backup object.
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(2*time.Minute), "", s3)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)

	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)

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

func TestFullOnlyOverTLS(t *testing.T) {
	testFullOnlyOverTLS(t, false)
}

func TestFullOnlyOverTLSS3(t *testing.T) {
	testFullOnlyOverTLS(t, true)
}

func testBackupRetention(t *testing.T, s3 bool) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	skipBackup(t, s3)

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	s3secret := createS3Secret(t, kubernetes, s3)

	cluster := e2eutil.MustNewBackupCluster(t, kubernetes, clusterSize, f.CouchbaseBackupImage, s3secret)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// Trigger a full backup.
	backup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(5*time.Minute), "", s3)
	backup.Spec.BackupRetention = e2espec.NewDurationS(60)
	backup = e2eutil.MustNewBackup(t, kubernetes, backup)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 10*time.Minute)

	// Trigger another full backup, the old one should be discarded because it is
	// too old.
	e2eutil.MustPatchBackup(t, kubernetes, backup, jsonpatch.NewPatchSet().Replace("/Spec/Full/Schedule", cronScheduleOnceIn(2*time.Minute)), time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 10*time.Minute)

	// And one more time to make sure the discard didn't do anything stupid...
	e2eutil.MustPatchBackup(t, kubernetes, backup, jsonpatch.NewPatchSet().Replace("/Spec/Full/Schedule", cronScheduleOnceIn(2*time.Minute)), time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 10*time.Minute)
}

func TestBackupRetention(t *testing.T) {
	testBackupRetention(t, false)
}

func TestBackupRetentionS3(t *testing.T) {
	testBackupRetention(t, true)
}
