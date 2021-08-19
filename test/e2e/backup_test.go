package e2e

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	v2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/cluster"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

func createTestBackup(strategy v2.Strategy, fullSchedule, incrementalSchedule, s3BucketName string, s3 bool) *v2.CouchbaseBackup {
	backup := &v2.CouchbaseBackup{
		ObjectMeta: v1.ObjectMeta{
			Name: strings.ReplaceAll(string(strategy), "_", "-"),
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

	if framework.Global.StorageClassName != "" {
		backup.Spec.StorageClassName = &framework.Global.StorageClassName
	}

	if s3 {
		backup.Spec.S3Bucket = v2.S3BucketURI("s3://" + s3BucketName)
	}

	return backup
}

func createTestRestoreBackup(backup *v2.CouchbaseBackup, repo reflect.Value, s3BucketName string, s3 bool) *v2.CouchbaseBackupRestore {
	oldest := "oldest"
	latest := "latest"

	restore := &v2.CouchbaseBackupRestore{
		ObjectMeta: v1.ObjectMeta{
			Name: "test-restore",
		},
		Spec: v2.CouchbaseBackupRestoreSpec{
			Backup: backup.Name,
			Repo:   repo.String(),
			Start: &v2.StrOrInt{
				Str: &oldest,
			},
			End: &v2.StrOrInt{
				Str: &latest,
			},
			Threads: 4,
		},
	}

	if s3 {
		restore.Spec.S3Bucket = v2.S3BucketURI("s3://" + s3BucketName)
	}

	return restore
}

// cronScheduleOnceIn returns a one-time, predictable cron schedule.  It is
// highly recommended that this be at least 2 minutes to give things a chance
// to become created.
func cronScheduleOnceIn(duration time.Duration) string {
	when := time.Now().UTC().Add(duration)
	return fmt.Sprintf("%d * * * *", when.Minute())
}

func createS3Bucket(t *testing.T, bucket, accessKey, secretID, region string) error {
	// create S3 bucket
	token := ""

	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(region),
		Credentials: credentials.NewStaticCredentials(accessKey, secretID, token)},
	)

	if err != nil {
		return err
	}

	// Create S3 service client
	svc := s3.New(sess)

	if MustGetS3Bucket(t, svc, bucket) {
		return nil
	}

	// Create the S3 Bucket
	_, err = svc.CreateBucket(&s3.CreateBucketInput{
		ACL:    aws.String("private"),
		Bucket: aws.String(bucket),
		CreateBucketConfiguration: &s3.CreateBucketConfiguration{
			LocationConstraint: aws.String(region),
		},
	})

	if err != nil {
		return err
	}

	// Make Objects of the bucket private
	_, err = svc.PutPublicAccessBlock(&s3.PutPublicAccessBlockInput{
		Bucket: aws.String(bucket),
		PublicAccessBlockConfiguration: &s3.PublicAccessBlockConfiguration{
			BlockPublicAcls:       aws.Bool(true),
			IgnorePublicAcls:      aws.Bool(true),
			BlockPublicPolicy:     aws.Bool(true),
			RestrictPublicBuckets: aws.Bool(true),
		},
	})

	if err != nil {
		return err
	}

	err = svc.WaitUntilBucketExists(&s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	})

	if err != nil {
		return fmt.Errorf("Error occurred while waiting for bucket to be created, %w", err)
	}

	return nil
}

func MustCreateS3Bucket(t *testing.T, bucket, accessKey, secretID, region string) {
	if err := createS3Bucket(t, bucket, accessKey, secretID, region); err != nil {
		MustDeleteS3Bucket(t, bucket, accessKey, secretID, region)
		e2eutil.Die(t, err)
	}
}

func getS3Bucket(svc *s3.S3, bucket string) (bool, error) {
	result, err := svc.ListBuckets(&s3.ListBucketsInput{})

	if err != nil {
		return false, err
	}

	var bucketPresent bool = false

	for _, s3bucket := range result.Buckets {
		if bucket == *s3bucket.Name {
			bucketPresent = true
			break
		}
	}

	return bucketPresent, nil
}

func MustGetS3Bucket(t *testing.T, svc *s3.S3, bucket string) bool {
	bucketPresent, err := getS3Bucket(svc, bucket)

	if err != nil {
		e2eutil.Die(t, err)
	}

	return bucketPresent
}

func deleteS3Bucket(t *testing.T, bucket, accessKey, secretID, region string) error {
	// create S3 bucket
	token := ""

	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(region),
		Credentials: credentials.NewStaticCredentials(accessKey, secretID, token)},
	)

	if err != nil {
		return err
	}

	// Create S3 service client
	svc := s3.New(sess)

	// Check if the bucket is present
	if bucketPresent := MustGetS3Bucket(t, svc, bucket); bucketPresent == false {
		return nil
	}

	// Empty the bucket before deleting it
	// Setup BatchDeleteIterator to iterate through a list of objects.
	iter := s3manager.NewDeleteListIterator(svc, &s3.ListObjectsInput{
		Bucket: aws.String(bucket),
	})

	// Traverse iterator deleting each object
	if err := s3manager.NewBatchDeleteWithClient(svc).Delete(aws.BackgroundContext(), iter); err != nil {
		return fmt.Errorf("Unable to delete objects from bucket %q, %w", bucket, err)
	}

	// Create the S3 Bucket
	_, err = svc.DeleteBucket(&s3.DeleteBucketInput{
		Bucket: aws.String(bucket),
	})

	if err != nil {
		return fmt.Errorf("Bucket can not be deleted %w", err)
	}

	err = svc.WaitUntilBucketNotExists(&s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	})

	if err != nil {
		return fmt.Errorf("Error occurred while waiting for bucket to be deleted, %w", err)
	}

	return nil
}

func MustDeleteS3Bucket(t *testing.T, bucket, accessKey, secretID, region string) {
	if err := deleteS3Bucket(t, bucket, accessKey, secretID, region); err != nil {
		e2eutil.Die(t, err)
	}
}

func createS3Secret(t *testing.T, targetKube *types.Cluster, s3 bool, cleanup func()) (*corev1.Secret, string, func()) {
	if !s3 {
		return nil, "", cleanup
	}

	f := framework.Global

	framework.Requires(t, targetKube).AtLeastVersion("6.6.0").HasS3Parameters()

	s3secret := "s3-secret"
	s3BucketName := "s3bucket-" + targetKube.Namespace

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

	if _, err := targetKube.KubeClient.CoreV1().Secrets(targetKube.Namespace).Create(context.Background(), secret, v1.CreateOptions{}); err != nil {
		e2eutil.Die(t, err)
	}

	MustCreateS3Bucket(t, s3BucketName, f.S3AccessKey, f.S3SecretID, f.S3Region)

	// Note: deferred functions must not call Die.
	cleanup1 := func() {
		_ = deleteS3Bucket(t, s3BucketName, f.S3AccessKey, f.S3SecretID, f.S3Region)

		cleanup()
	}

	return secret, s3BucketName, cleanup1
}

func testFullIncremental(t *testing.T, s3 bool) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)

	s3secret, s3BucketName, cleanup := createS3Secret(t, targetKube, s3, cleanup)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	// Create a normal cluster.
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, targetKube)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 3*time.Minute)

	// Create a Backup object.
	backup := createTestBackup(v2.FullIncremental, cronScheduleOnceIn(2*time.Minute), cronScheduleOnceIn(5*time.Minute), s3BucketName, s3)
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

func TestBackupFullIncremental(t *testing.T) {
	testFullIncremental(t, false)
}

func TestBackupFullIncrementalS3(t *testing.T) {
	testFullIncremental(t, true)
}

func testFullOnly(t *testing.T, s3 bool) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)

	s3secret, s3BucketName, cleanup := createS3Secret(t, targetKube, s3, cleanup)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	// Create a normal cluster.
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, targetKube)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// Create a Backup object.
	backup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(2*time.Minute), "", s3BucketName, s3)
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

func TestBackupFullOnly(t *testing.T) {
	testFullOnly(t, false)
}

func TestBackupFullOnlyS3(t *testing.T) {
	testFullOnly(t, true)
}

// Cluster goes down during a backup (all pods go down)
// Tests --purge behaviour is working as expected - this should allow us to ignore the previous backup and start anew.
func testFailedBackupBehaviour(t *testing.T, s3 bool) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)

	s3secret, s3BucketName, cleanup := createS3Secret(t, targetKube, s3, cleanup)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2

	// Create cluster.
	testCouchbase := clusterOptions().WithMixedTopology(mdsGroupSize).Generate(targetKube)

	if s3secret != nil {
		testCouchbase.Spec.Backup.S3Secret = s3secret.Name
	}

	testCouchbase.Spec.Backup.Image = f.CouchbaseBackupImage

	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup, make a lot, we need this backup to be slow to the point
	// we can vaguely reliably kill pods while it's happening.
	e2eutil.MustPopulateWithDataSize(t, targetKube, testCouchbase, bucket.GetName(), f.CouchbaseServerImage, 1<<30, time.Minute)

	// create this backup to run every 2 minutes so we can test the backup still runs successfully after a failure.
	fullBackup := createTestBackup(v2.FullOnly, "*/2 * * * *", "", s3BucketName, s3)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)

	// Wait for the failure now, the backup has a tendency to fail and raise the event
	// while we are still deleting the pods.
	backupFailWait := e2eutil.WaitForPendingClusterEvent(targetKube, fullBackup, e2eutil.BackupFailedEvent(testCouchbase, fullBackup.Name), 15*time.Minute)

	// wait for the backup to start
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 10*time.Minute)

	// kill the cluster
	for i := 0; i < mdsGroupSize; i++ {
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, i, false)
	}

	// backup should fail
	e2eutil.MustReceiveErrorValue(t, backupFailWait)

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

	s3secret, s3BucketName, cleanup := createS3Secret(t, targetKube, s3, cleanup)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3

	// Create cluster.
	numOfDocs := f.DocsCount

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, targetKube)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// Create a Backup object.
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(7*time.Minute), "", s3BucketName, s3)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, time.Minute)

	// check pvc exists
	pvc, err := targetKube.KubeClient.CoreV1().PersistentVolumeClaims(targetKube.Namespace).Get(context.Background(), fullBackup.Name, v1.GetOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	// check pvc has same name as backup
	if pvc.Name != fullBackup.Name {
		e2eutil.Die(t, fmt.Errorf("pvc name %s is a mismatch with backup name %s", pvc.Name, fullBackup.Name))
	}

	// Expect backup to complete
	e2eutil.MustDeletePVC(t, targetKube, pvc, 5*time.Minute)
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

	s3secret, s3BucketName, cleanup := createS3Secret(t, targetKube, s3, cleanup)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3

	// Create a normal cluster.
	numOfDocs := f.DocsCount

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, targetKube)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// create initial backup
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(2*time.Minute), "", s3BucketName, s3)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)

	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)

	// wait for backup to be deleted
	e2eutil.MustDeleteBackup(t, targetKube, fullBackup)
	e2eutil.MustWaitForBackupDeletion(t, targetKube, fullBackup, 2*time.Minute)

	// create new backup
	fullIncrementalBackup := createTestBackup(v2.FullIncremental, cronScheduleOnceIn(2*time.Minute), cronScheduleOnceIn(5*time.Minute), s3BucketName, s3)
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

	s3secret, s3BucketName, cleanup := createS3Secret(t, targetKube, s3, cleanup)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3

	// Create a normal cluster.
	numOfDocs := f.DocsCount

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, targetKube)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// create initial backup
	fullIncrementalBackup := createTestBackup(v2.FullIncremental, cronScheduleOnceIn(2*time.Minute), cronScheduleOnceIn(5*time.Minute), s3BucketName, s3)
	fullIncrementalBackup = e2eutil.MustNewBackup(t, targetKube, fullIncrementalBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullIncrementalBackup, 2*time.Minute)

	// wait for backups completion and confirm they ran without error
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupStartedEvent(testCouchbase, fullIncrementalBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullIncrementalBackup.Name), 2*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupStartedEvent(testCouchbase, fullIncrementalBackup.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullIncrementalBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullIncrementalBackup.Name), 2*time.Minute)

	// wait for backup to be deleted
	e2eutil.MustDeleteBackup(t, targetKube, fullIncrementalBackup)
	e2eutil.MustWaitForBackupDeletion(t, targetKube, fullIncrementalBackup, 2*time.Minute)

	// create new backup
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(2*time.Minute), "", s3BucketName, s3)
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

	s3secret, s3BucketName, cleanup := createS3Secret(t, targetKube, s3, cleanup)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	fullFreq := 2

	// Create a normal cluster.
	clusterSize := constants.Size3

	numOfDocs := f.DocsCount

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, targetKube)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 5*time.Minute)

	// Create a Backup object.
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(time.Duration(fullFreq)*time.Minute), "", s3BucketName, s3)
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
			Threads: 4,
		},
	}

	if s3 {
		restore.Spec.S3Bucket = v2.S3BucketURI("s3://" + s3BucketName)
	}

	// delete bucket
	e2eutil.MustDeleteBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, targetKube, testCouchbase, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 10*time.Minute)

	bucket = e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)

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

	s3secret, s3BucketName, cleanup := createS3Secret(t, targetKube, s3, cleanup)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	// Create a normal cluster.
	clusterSize := constants.Size3

	numOfDocs := f.DocsCount

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, targetKube)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(2*time.Minute), "", s3BucketName, s3)
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

	s3secret, s3BucketName, cleanup := createS3Secret(t, targetKube, s3, cleanup)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	clusterSize := constants.Size3
	// Create a normal cluster.
	numOfDocs := f.DocsCount

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, targetKube)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// Create Backup object 1.
	fullIncrementalBackup := createTestBackup(v2.FullIncremental, cronScheduleOnceIn(2*time.Minute), cronScheduleOnceIn(5*time.Minute), s3BucketName, s3)
	fullIncrementalBackup = e2eutil.MustNewBackup(t, targetKube, fullIncrementalBackup)

	// Create Backup object 2.
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(7*time.Minute), "", s3BucketName, s3)
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

	s3secret, s3BucketName, cleanup := createS3Secret(t, targetKube, s3, cleanup)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	clusterSize := constants.Size3
	// Create the cluster.
	numOfDocs := f.DocsCount

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).WithS3(s3secret).MustCreate(t, targetKube)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// Create a Backup object.
	fullIncrementalBackup := createTestBackup(v2.FullIncremental, cronScheduleOnceIn(2*time.Minute), cronScheduleOnceIn(5*time.Minute), s3BucketName, s3)
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

func TestBackupFullIncrementalOverTLS(t *testing.T) {
	testFullIncrementalOverTLS(t, false)
}

func TestBackupFullIncrementalOverTLSS3(t *testing.T) {
	testFullIncrementalOverTLS(t, true)
}

func testFullOnlyOverTLS(t *testing.T, s3 bool, tls *e2eutil.TLSOpts, policy *v2.ClientCertificatePolicy) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)

	s3secret, s3BucketName, cleanup := createS3Secret(t, targetKube, s3, cleanup)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	// Create the cluster.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, targetKube, tls)

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithMutualTLS(ctx, policy).WithS3(s3secret).MustCreate(t, targetKube)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// Create a Backup object.
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(2*time.Minute), "", s3BucketName, s3)
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
		eventschema.Optional{
			Validator: eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Full)},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestBackupFullOnlyOverTLS(t *testing.T) {
	testFullOnlyOverTLS(t, false, &e2eutil.TLSOpts{}, nil)
}

func TestBackupFullOnlyOverTLSStandard(t *testing.T) {
	keyEncoding := e2eutil.KeyEncodingPKCS8
	opts := &e2eutil.TLSOpts{Source: e2eutil.TLSSourceTLSSecret, KeyEncoding: &keyEncoding}

	testFullOnlyOverTLS(t, false, opts, nil)
}

func TestBackupFullOnlyOverMutualTLS(t *testing.T) {
	policy := v2.ClientCertificatePolicyEnable

	testFullOnlyOverTLS(t, false, &e2eutil.TLSOpts{}, &policy)
}

func TestBackupFullOnlyOverMandatoryMutualTLS(t *testing.T) {
	policy := v2.ClientCertificatePolicyMandatory

	testFullOnlyOverTLS(t, false, &e2eutil.TLSOpts{}, &policy)
}

func TestBackupFullOnlyOverTLSS3(t *testing.T) {
	testFullOnlyOverTLS(t, true, &e2eutil.TLSOpts{}, nil)
}

func TestBackupFullOnlyOverTLSS3Standard(t *testing.T) {
	keyEncoding := e2eutil.KeyEncodingPKCS8
	opts := &e2eutil.TLSOpts{Source: e2eutil.TLSSourceTLSSecret, KeyEncoding: &keyEncoding}

	testFullOnlyOverTLS(t, true, opts, nil)
}

func testBackupRetention(t *testing.T, s3 bool) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)

	s3secret, s3BucketName, cleanup := createS3Secret(t, kubernetes, s3, cleanup)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// Trigger a full backup.
	backup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(5*time.Minute), "", s3BucketName, s3)
	backup.Spec.BackupRetention = e2espec.NewDurationS(60)
	backup = e2eutil.MustNewBackup(t, kubernetes, backup)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 10*time.Minute)

	// Trigger another full backup, the old one should be discarded because it is
	// too old.
	e2eutil.MustPatchBackup(t, kubernetes, backup, jsonpatch.NewPatchSet().Replace("/spec/full/schedule", cronScheduleOnceIn(2*time.Minute)), time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 10*time.Minute)

	// And one more time to make sure the discard didn't do anything stupid...
	e2eutil.MustPatchBackup(t, kubernetes, backup, jsonpatch.NewPatchSet().Replace("/spec/full/schedule", cronScheduleOnceIn(2*time.Minute)), time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 10*time.Minute)
}

func TestBackupRetention(t *testing.T) {
	testBackupRetention(t, false)
}

func TestBackupRetentionS3(t *testing.T) {
	testBackupRetention(t, true)
}

// Manually editing the size of the PVC in a CouchbaseBackup should be reflected in the PVC and PV
// N.B. Requires a SC with allowVolumeExpansion: true.
func testBackupPVCResize(t *testing.T, s3 bool) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)

	s3secret, s3BucketName, cleanup := createS3Secret(t, targetKube, s3, cleanup)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	// Static configuration.
	clusterSize := 3
	newBackupSize := resource.MustParse("5Gi")

	// Create cluster.
	numOfDocs := 100

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, targetKube)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 2*time.Minute)

	// Create a Backup object.
	backup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(4*time.Minute), "", s3BucketName, s3)
	backup = e2eutil.MustNewBackup(t, targetKube, backup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, backup, time.Minute)
	e2eutil.MustWaitForBackupEvent(t, targetKube, backup, e2eutil.BackupCompletedEvent(testCouchbase, backup.Name), 10*time.Minute)

	// edit CouchbaseBackup to trigger another backup which then triggers the PVC resize
	patchset := jsonpatch.NewPatchSet().
		Replace("/spec/full/schedule", cronScheduleOnceIn(4*time.Minute)).
		Replace("/spec/size", newBackupSize)
	e2eutil.MustPatchBackup(t, targetKube, backup, patchset, time.Minute)

	// Expect backup to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, backup, e2eutil.BackupCompletedEvent(testCouchbase, backup.Name), 10*time.Minute)

	// check pvc has been resized
	e2eutil.MustWaitForPVCSize(t, targetKube, backup.Name, &newBackupSize, 3*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket created
	// * Backup created
	// * Backup updated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Full)},
		eventschema.Event{Reason: k8sutil.EventReasonBackupUpdated, FuzzyMessage: string(cluster.Full)},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestBackupPVCResize(t *testing.T) {
	testBackupPVCResize(t, false)
}

func TestBackupPVCResizeS3(t *testing.T) {
	testBackupPVCResize(t, true)
}

// TestBackupAutoscaling populates the database with a load of data, then backs it up.
// We check the capacity and then ensure that by setting the right thresholds, it
// resizes the volume as we need it.
func TestBackupAutoscaling(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

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
	backup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(2*time.Minute), "", "", false)
	backup.Spec.Size = e2espec.NewResourceQuantityMi(1024)
	backup.Spec.AutoScaling = &v2.CouchbaseBackupAutoScaling{
		Limit:            volumeSizeLimit,
		ThresholdPercent: 99,
		IncrementPercent: 200,
	}

	backup = e2eutil.MustNewBackup(t, kubernetes, backup)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, k8sutil.BackupUpdateEvent(backup.Name, cluster), 20*time.Minute)

	// Trigger another backup, this will cause volume expansion when it's remounted.
	// Also reduce the threshold down so we don't trigger another update unnecessarily,
	// and also test that another update doesn't happen erroneously.  Beware of rounding,
	// My request for 1.5Gi turned into 2Gi in reality!!
	patchset := jsonpatch.NewPatchSet().
		Replace("/spec/full/schedule", cronScheduleOnceIn(2*time.Minute)).
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

func testBackupAndRestoreDisableEventing(t *testing.T, s3 bool) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)

	s3secret, s3BucketName, cleanup := createS3Secret(t, targetKube, s3, cleanup)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	fullFreq := 2

	// Static configuration.
	clusterSize := constants.Size3
	dataQuota := e2espec.NewResourceQuantityMi(int64(256 * 3))
	buckets := []v1.Object{}

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).Generate(targetKube)

	testCouchbase.Spec.Servers[0].Services = append(testCouchbase.Spec.Servers[0].Services, v2.EventingService)
	testCouchbase.Spec.ClusterSettings.DataServiceMemQuota = dataQuota
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	for i := 0; i < 3; i++ {
		bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
		bucket.SetName(fmt.Sprintf("%s-%d", bucket.GetName(), i))
		buckets = append(buckets, bucket)
		e2eutil.MustNewBucket(t, targetKube, bucket)
		e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)
	}

	// create eventing function and verify
	e2eutil.MustDeployEventingFunction(t, targetKube, testCouchbase, "test", buckets[0].GetName(), buckets[1].GetName(), buckets[2].GetName(), function, time.Minute)
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, buckets[0].GetName(), 0, f.DocsCount)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, buckets[2].GetName(), f.DocsCount, 5*time.Minute)

	// Create a Backup object.
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(time.Duration(fullFreq)*time.Minute), "", s3BucketName, s3)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)

	// wait for backup to start
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	// wait for backup status update
	repo := e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "Repo", 5*time.Minute)

	// create restore resource either with eventing disabled.
	restore := createTestRestoreBackup(fullBackup, repo, s3BucketName, s3)
	restore.Spec.Services.Eventing = pointer.BoolPtr(false)

	// delete bucket
	for _, bucket := range buckets {
		e2eutil.MustDeleteBucket(t, targetKube, bucket)
		e2eutil.MustWaitUntilBucketNotExists(t, targetKube, testCouchbase, bucket.GetName(), 2*time.Minute)
	}

	// delete the eventing function
	e2eutil.MustDeleteEventingFunction(t, targetKube, testCouchbase, time.Minute)
	time.Sleep(30 * time.Second)

	for _, bucket := range buckets {
		e2eutil.MustNewBucket(t, targetKube, bucket)
		e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)
	}

	// create new restore
	e2eutil.MustNewBackupRestore(t, targetKube, restore)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// if eventing function is restored raise error.
	if err := e2eutil.MustGetEventingFunction(t, targetKube, testCouchbase, time.Minute); err == nil {
		e2eutil.Die(t, err)
	}

	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, buckets[0].GetName(), f.DocsCount, 5*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, buckets[2].GetName(), f.DocsCount, 5*time.Minute)

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
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Full)},
		eventschema.Repeat{Times: len(buckets), Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted}},
		eventschema.Repeat{Times: len(buckets), Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketCreated}},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestBackupAndRestoreDisableEventing(t *testing.T) {
	testBackupAndRestoreDisableEventing(t, false)
}

func TestBackupAndRestoreDisableEventingS3(t *testing.T) {
	testBackupAndRestoreDisableEventing(t, true)
}

func testBackupAndRestoreDisableGSI(t *testing.T, s3 bool) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)

	s3secret, s3BucketName, cleanup := createS3Secret(t, targetKube, s3, cleanup)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	fullFreq := 2
	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, targetKube)

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// populate the bucket
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 5*time.Minute)

	// create eventing function and verify
	query := "CREATE PRIMARY INDEX `#primary` ON `default` USING GSI"
	e2eutil.MustExecuteIndexQuery(t, targetKube, testCouchbase, query, time.Minute)

	// verify index creation
	time.Sleep(20 * time.Second)
	indexCount := e2eutil.MustGetIndexCount(t, targetKube, testCouchbase, 2*time.Minute)

	if indexCount == 0 {
		e2eutil.Die(t, fmt.Errorf("Index `#primary` not created"))
	}

	// Create a Backup object.
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(time.Duration(fullFreq)*time.Minute), "", s3BucketName, s3)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)

	// wait for backup to start
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	// wait for backup status update
	repo := e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "Repo", 5*time.Minute)

	// disable GSI
	restore := createTestRestoreBackup(fullBackup, repo, s3BucketName, s3)
	restore.Spec.Services.GSIIndex = pointer.BoolPtr(false)

	// delete bucket
	e2eutil.MustDeleteBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, targetKube, testCouchbase, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// create new restore
	e2eutil.MustNewBackupRestore(t, targetKube, restore)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Check Index is not restored.
	indexCount = e2eutil.MustGetIndexCount(t, targetKube, testCouchbase, 2*time.Minute)
	if indexCount != 0 {
		e2eutil.Die(t, fmt.Errorf("Index `#primary` restored"))
	}

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

func TestBackupAndRestoreDisableGSI(t *testing.T) {
	testBackupAndRestoreDisableGSI(t, false)
}

func TestBackupAndRestoreDisableGSIS3(t *testing.T) {
	testBackupAndRestoreDisableGSI(t, true)
}

func testBackupAndRestoreDisableAnalytics(t *testing.T, s3 bool) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)

	s3secret, s3BucketName, cleanup := createS3Secret(t, targetKube, s3, cleanup)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	// Static configuration.
	fullFreq := 2
	numOfDocs := f.DocsCount
	clusterSize := constants.Size3

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).Generate(targetKube)

	testCouchbase.Spec.Servers[0].Services = append(testCouchbase.Spec.Servers[0].Services, v2.AnalyticsService)
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// populate the bucket
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 5*time.Minute)

	// create eventing function and verify
	analyticsDataset := "testDataset1"
	queries := []string{
		"CREATE DATASET " + analyticsDataset + " ON `default`",
		"CONNECT LINK Local",
	}

	for _, query := range queries {
		e2eutil.MustExecuteAnalyticsQuery(t, targetKube, testCouchbase, query, time.Minute)
	}

	time.Sleep(time.Minute) // let analytics catch up
	datasetItemCount := e2eutil.MustGetDatasetItemCount(t, targetKube, testCouchbase, analyticsDataset, time.Minute)

	if datasetItemCount != int64(numOfDocs) {
		e2eutil.Die(t, fmt.Errorf("dataset item mismatch %v/%v", datasetItemCount, numOfDocs))
	}

	// Create a Backup object.
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(time.Duration(fullFreq)*time.Minute), "", s3BucketName, s3)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)

	// wait for backup to start
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	// wait for backup status update
	repo := e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "Repo", 5*time.Minute)

	// disable analytics
	restore := createTestRestoreBackup(fullBackup, repo, s3BucketName, s3)
	restore.Spec.Services.Analytics = pointer.BoolPtr(false)

	// delete bucket
	e2eutil.MustDeleteBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, targetKube, testCouchbase, bucket.GetName(), 2*time.Minute)

	// drop dataset
	query := "DROP DATASET " + analyticsDataset
	e2eutil.MustExecuteAnalyticsQuery(t, targetKube, testCouchbase, query, time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// create new restore
	e2eutil.MustNewBackupRestore(t, targetKube, restore)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	datasetCount := e2eutil.MustGetDatasetCount(t, targetKube, testCouchbase, time.Minute)
	if datasetCount != 0 {
		e2eutil.Die(t, fmt.Errorf("dataset restored"))
	}

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

func TestBackupAndRestoreDisableAnalytics(t *testing.T) {
	testBackupAndRestoreDisableAnalytics(t, false)
}

func TestBackupAndRestoreDisableAnalyticsS3(t *testing.T) {
	testBackupAndRestoreDisableAnalytics(t, true)
}

func testBackupAndRestoreDisableData(t *testing.T, s3 bool) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)

	s3secret, s3BucketName, cleanup := createS3Secret(t, targetKube, s3, cleanup)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	// Static configuration.
	fullFreq := 2
	// Create a normal cluster.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, targetKube)

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 5*time.Minute)

	// Create a Backup object.
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(time.Duration(fullFreq)*time.Minute), "", s3BucketName, s3)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)

	// wait for backup to start
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	// wait for backup status update
	repo := e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "Repo", 5*time.Minute)

	restore := createTestRestoreBackup(fullBackup, repo, s3BucketName, s3)
	restore.Spec.Services.Data = pointer.BoolPtr(false)

	// delete bucket
	e2eutil.MustDeleteBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, targetKube, testCouchbase, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 10*time.Minute)

	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 5*time.Minute)

	// create new restore
	e2eutil.MustNewBackupRestore(t, targetKube, restore)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// restore job is too fast, just validate that no items were restored.
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, 5*time.Minute)

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

func TestBackupAndRestoreDisableData(t *testing.T) {
	testBackupAndRestoreDisableData(t, false)
}

func TestBackupAndRestoreDisableDataS3(t *testing.T) {
	testBackupAndRestoreDisableData(t, true)
}

func testBackupAndRestoreEnableBucketConfig(t *testing.T, s3 bool) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)

	s3secret, s3BucketName, cleanup := createS3Secret(t, targetKube, s3, cleanup)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	// Static configuration.
	fullFreq := 2
	numOfDocs := f.DocsCount
	clusterSize := constants.Size3

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, targetKube)

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 5*time.Minute)

	// Create a Backup object.
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(time.Duration(fullFreq)*time.Minute), "", s3BucketName, s3)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)

	// wait for backup to start
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	// wait for backup status update
	repo := e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "Repo", 5*time.Minute)

	restore := createTestRestoreBackup(fullBackup, repo, s3BucketName, s3)
	restore.Spec.Services.BucketConfig = true

	e2eutil.MustDeleteBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, targetKube, testCouchbase, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	newBucket := e2espec.DefaultBucketTwoReplicas()
	e2eutil.MustNewBucket(t, targetKube, newBucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, newBucket, 2*time.Minute)

	// create new restore
	e2eutil.MustNewBackupRestore(t, targetKube, restore)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// verify replica count was restored.
	e2eutil.MustVerifyReplicaCount(t, targetKube, testCouchbase, newBucket.GetName(), 5*time.Minute)
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
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketEdited},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestBackupAndRestoreEnableBucketConfig(t *testing.T) {
	testBackupAndRestoreEnableBucketConfig(t, false)
}

func TestBackupAndRestoreEnableBucketConfigS3(t *testing.T) {
	testBackupAndRestoreEnableBucketConfig(t, true)
}

func testBackupAndRestoreMapBuckets(t *testing.T, s3 bool) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)

	s3secret, s3BucketName, cleanup := createS3Secret(t, targetKube, s3, cleanup)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	fullFreq := 2
	targetBucketName := "bucketty-mcbuccketface"
	// Create a normal cluster.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, targetKube)

	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 5*time.Minute)

	// Create a Backup object.
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(time.Duration(fullFreq)*time.Minute), "", s3BucketName, s3)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)

	// wait for backup to start
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	// wait for backup status update
	repo := e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "Repo", 5*time.Minute)

	// Create a Restore object for later.
	restore := createTestRestoreBackup(fullBackup, repo, s3BucketName, s3)

	restore.Spec.Data = &v2.CouchbaseBackupRestoreDataFilter{
		Map: []v2.RestoreMapping{
			{
				Source: v2.BucketScopeOrCollectionNameWithDefaults(bucket.GetName()),
				Target: v2.BucketScopeOrCollectionNameWithDefaults(targetBucketName),
			},
		},
	}

	// delete bucket
	e2eutil.MustDeleteBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, targetKube, testCouchbase, bucket.GetName(), 2*time.Minute)

	newBucket := e2espec.DefaultBucket()
	newBucket.SetName(targetBucketName)
	e2eutil.MustNewBucket(t, targetKube, newBucket)

	// create new restore
	e2eutil.MustNewBackupRestore(t, targetKube, restore)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 10*time.Minute)

	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, newBucket.GetName(), f.DocsCount, 5*time.Minute)

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

func TestBackupAndRestoreMapBuckets(t *testing.T) {
	testBackupAndRestoreMapBuckets(t, false)
}

func TestBackupAndRestoreMapBucketsS3(t *testing.T) {
	testBackupAndRestoreMapBuckets(t, true)
}

func testBackupAndRestoreIncludeBuckets(t *testing.T, s3 bool) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)

	s3secret, s3BucketName, cleanup := createS3Secret(t, targetKube, s3, cleanup)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	fullFreq := 2
	// Create a normal cluster.
	clusterSize := constants.Size3
	buckets := []v1.Object{}

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).Generate(targetKube)
	testCouchbase.Spec.ClusterSettings.DataServiceMemQuota = e2espec.NewResourceQuantityMi(int64(2 * 256))
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	for i := 0; i < 2; i++ {
		bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
		bucket.SetName(fmt.Sprintf("%s-%d", bucket.GetName(), i))
		buckets = append(buckets, bucket)
		e2eutil.MustNewBucket(t, targetKube, bucket)
		e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)
		e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, f.DocsCount)
		e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), f.DocsCount, 5*time.Minute)
	}

	// Create a Backup object.
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(time.Duration(fullFreq)*time.Minute), "", s3BucketName, s3)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)
	// wait for backup to start
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	// wait for backup status update
	repo := e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "Repo", 5*time.Minute)

	// Create a Restore object for later.
	restore := createTestRestoreBackup(fullBackup, repo, s3BucketName, s3)

	// include the buckets to be restored
	restore.Spec.Data = &v2.CouchbaseBackupRestoreDataFilter{
		Include: []v2.BucketScopeOrCollectionNameWithDefaults{
			v2.BucketScopeOrCollectionNameWithDefaults(buckets[0].GetName()),
		},
	}

	// delete bucket and then create
	for _, bucket := range buckets {
		e2eutil.MustDeleteBucket(t, targetKube, bucket)
		e2eutil.MustWaitUntilBucketNotExists(t, targetKube, testCouchbase, bucket.GetName(), 2*time.Minute)

		e2eutil.MustNewBucket(t, targetKube, bucket)
		e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)
	}

	// create new restore
	e2eutil.MustNewBackupRestore(t, targetKube, restore)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 10*time.Minute)

	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, buckets[0].GetName(), f.DocsCount, 5*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, buckets[1].GetName(), 0, 5*time.Minute)

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
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Full)},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestBackupAndRestoreIncludeBuckets(t *testing.T) {
	testBackupAndRestoreIncludeBuckets(t, false)
}

func TestBackupAndRestoreIncludeBucketsS3(t *testing.T) {
	testBackupAndRestoreIncludeBuckets(t, true)
}

func testBackupAndRestoreExcludeBuckets(t *testing.T, s3 bool) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)

	s3secret, s3BucketName, cleanup := createS3Secret(t, targetKube, s3, cleanup)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	fullFreq := 2
	// Create a normal cluster.
	clusterSize := constants.Size3
	buckets := []v1.Object{}

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).Generate(targetKube)
	testCouchbase.Spec.ClusterSettings.DataServiceMemQuota = e2espec.NewResourceQuantityMi(int64(2 * 256))
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	for i := 0; i < 2; i++ {
		bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
		bucket.SetName(fmt.Sprintf("%s-%d", bucket.GetName(), i))
		buckets = append(buckets, bucket)

		e2eutil.MustNewBucket(t, targetKube, bucket)
		e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

		// Populate the bucket
		e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, f.DocsCount)
		e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), f.DocsCount, 5*time.Minute)
	}

	// Create a Backup object.
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(time.Duration(fullFreq)*time.Minute), "", s3BucketName, s3)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)
	// wait for backup to start
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	// wait for backup status update
	repo := e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "Repo", 5*time.Minute)

	// Create a Restore object for later.
	restore := createTestRestoreBackup(fullBackup, repo, s3BucketName, s3)

	// Specify the bucket to be excluded from getting restored.
	restore.Spec.Data = &v2.CouchbaseBackupRestoreDataFilter{
		Exclude: []v2.BucketScopeOrCollectionNameWithDefaults{
			v2.BucketScopeOrCollectionNameWithDefaults(buckets[0].GetName()),
		},
	}

	// delete buckets and then create it.
	for _, bucket := range buckets {
		e2eutil.MustDeleteBucket(t, targetKube, bucket)
		e2eutil.MustWaitUntilBucketNotExists(t, targetKube, testCouchbase, bucket.GetName(), 2*time.Minute)

		e2eutil.MustNewBucket(t, targetKube, bucket)
		e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)
	}

	// create new restore
	e2eutil.MustNewBackupRestore(t, targetKube, restore)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 10*time.Minute)

	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, buckets[0].GetName(), 0, 5*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, buckets[1].GetName(), f.DocsCount, 5*time.Minute)

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
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Full)},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestBackupAndRestoreExcludeBuckets(t *testing.T) {
	testBackupAndRestoreExcludeBuckets(t, false)
}

func TestBackupAndRestoreExcludeBucketsS3(t *testing.T) {
	testBackupAndRestoreExcludeBuckets(t, true)
}

func testBackupAndRestoreNodeSelector(t *testing.T, s3 bool) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)

	nodes, err := targetKube.KubeClient.CoreV1().Nodes().List(context.Background(), v1.ListOptions{})
	if err != nil {
		e2eutil.Die(t, fmt.Errorf("failed to get node list: %w", err))
	}

	s3secret, s3BucketName, cleanup := createS3Secret(t, targetKube, s3, cleanup)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	// Static configuration.
	fullFreq := 2
	// Create a normal cluster.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).Generate(targetKube)

	zone, ok := nodes.Items[0].Labels[constants.FailureDomainZoneLabel]
	if !ok {
		e2eutil.Die(nil, fmt.Errorf("node %s missing label %s", nodes.Items[0].Name, constants.FailureDomainZoneLabel))
	}

	nodeSelector := map[string]string{
		constants.FailureDomainZoneLabel: zone,
	}

	testCouchbase.Spec.Backup.NodeSelector = nodeSelector
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, 5*time.Minute)

	// Create a Backup object.
	fullBackup := createTestBackup(v2.FullOnly, cronScheduleOnceIn(time.Duration(fullFreq)*time.Minute), "", s3BucketName, s3)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, fullBackup)

	// wait for backup
	e2eutil.MustWaitForBackup(t, targetKube, fullBackup, 2*time.Minute)

	// wait for backup to start
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupStartedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, targetKube, fullBackup, e2eutil.BackupCompletedEvent(testCouchbase, fullBackup.Name), 5*time.Minute)
	// wait for backup status update
	repo := e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "Repo", 5*time.Minute)

	// create restore object
	restore := createTestRestoreBackup(fullBackup, repo, s3BucketName, s3)

	// delete bucket
	e2eutil.MustDeleteBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, targetKube, testCouchbase, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 10*time.Minute)

	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 5*time.Minute)

	// create new restore
	e2eutil.MustNewBackupRestore(t, targetKube, restore)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// restore job is too fast, just validate that no items were restored.
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), 0, 5*time.Minute)

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

func TestBackupAndRestoreNodeSelector(t *testing.T) {
	testBackupAndRestoreNodeSelector(t, false)
}

func TestBackupAndRestoreNodeSelectorS3(t *testing.T) {
	testBackupAndRestoreNodeSelector(t, true)
}
