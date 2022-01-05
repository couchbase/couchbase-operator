package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	v2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
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

const (
	fullBackupResourceName = "full"
)

func createS3Bucket(t *testing.T, bucket, accessKey, secretID, region, endpoint string, cert []byte) error {
	// create S3 bucket
	helper := e2eutil.AwsHelper(accessKey, secretID, region).WithEndpoint(endpoint).WithEndpointCert(cert).Create()

	// Create S3 service client
	svc := s3.New(helper.Sess)

	if MustGetS3Bucket(t, svc, bucket) {
		return nil
	}

	// Create the S3 Bucket
	_, err := svc.CreateBucket(&s3.CreateBucketInput{
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
	if !strings.Contains(endpoint, "minio") { // minio doesn't support this action.
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
	}

	err = svc.WaitUntilBucketExists(&s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	})

	if err != nil {
		return fmt.Errorf("Error occurred while waiting for bucket to be created, %w", err)
	}

	return nil
}

func MustCreateS3Bucket(t *testing.T, bucket, accessKey, secretID, region string, endpoint string, cert []byte) {
	if err := createS3Bucket(t, bucket, accessKey, secretID, region, endpoint, cert); err != nil {
		MustDeleteS3Bucket(t, bucket, accessKey, secretID, region, endpoint, cert)
		e2eutil.Die(t, err)
	}
}

func getS3Bucket(svc *s3.S3, bucket string) (bool, error) {
	result, err := svc.ListBuckets(&s3.ListBucketsInput{})
	if err != nil {
		return false, err
	}

	var bucketPresent bool

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

func deleteS3Bucket(t *testing.T, bucket, accessKey, secretID, region string, endpoint string, cert []byte) error {
	// create S3 bucket
	helper := e2eutil.AwsHelper(accessKey, secretID, region).WithEndpoint(endpoint).WithEndpointCert(cert).Create()

	// Create S3 service client
	svc := s3.New(helper.Sess)

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
	_, err := svc.DeleteBucket(&s3.DeleteBucketInput{
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

func MustDeleteS3Bucket(t *testing.T, bucket, accessKey, secretID, region string, endpoint string, cert []byte) {
	if err := deleteS3Bucket(t, bucket, accessKey, secretID, region, endpoint, cert); err != nil {
		e2eutil.Die(t, err)
	}
}

// exact same as createS3Secret but with custom endpoint and cert.
func createObjEndpointS3Secret(t *testing.T, kubernetes *types.Cluster, endpoint string, cert []byte) (*corev1.Secret, string, func()) {
	f := framework.Global

	framework.Requires(t, kubernetes).AtLeastVersion("6.6.0").PlatformIs(v2.PlatformTypeAWS).HasS3Parameters()

	s3secret := "s3-secret"
	s3BucketName := "s3bucket-" + kubernetes.Namespace

	secret := &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{
			Name: s3secret,
		},
		Data: map[string][]byte{
			"region":            []byte(f.MinioRegion),
			"access-key-id":     []byte(f.MinioAccessKey),
			"secret-access-key": []byte(f.MinioSecretID),
		},
	}

	MustCreateS3Bucket(t, s3BucketName, f.MinioAccessKey, f.MinioSecretID, f.MinioRegion, endpoint, cert)

	// Note: deferred functions must not call Die.
	cleanup := func() {
		_ = deleteS3Bucket(t, s3BucketName, f.MinioAccessKey, f.MinioSecretID, f.MinioRegion, endpoint, cert)
	}

	return secret, s3BucketName, cleanup
}

func createS3RegionSecret(t *testing.T, kubernetes *types.Cluster) (*corev1.Secret, string, func()) {
	f := framework.Global

	framework.Requires(t, kubernetes).AtLeastVersion("6.6.0").PlatformIs(v2.PlatformTypeAWS).HasS3Parameters()

	s3secret := "s3-secret"
	s3BucketName := "s3bucket-" + kubernetes.Namespace

	secret := &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{
			Name: s3secret,
		},
		Data: map[string][]byte{
			"region": []byte(f.S3Region),
		},
	}

	if _, err := kubernetes.KubeClient.CoreV1().Secrets(kubernetes.Namespace).Create(context.Background(), secret, v1.CreateOptions{}); err != nil {
		e2eutil.Die(t, err)
	}

	MustCreateS3Bucket(t, s3BucketName, f.S3AccessKey, f.S3SecretID, f.S3Region, "", nil)

	// Note: deferred functions must not call Die.
	cleanup := func() {
		_ = deleteS3Bucket(t, s3BucketName, f.S3AccessKey, f.S3SecretID, f.S3Region, "", nil)
	}

	return secret, s3BucketName, cleanup
}

// creates an s3 bucket AND the s3 secret.
func createS3Secret(t *testing.T, kubernetes *types.Cluster, s3 bool) (*corev1.Secret, string, func()) {
	if !s3 {
		return nil, "", func() {}
	}

	f := framework.Global

	framework.Requires(t, kubernetes).AtLeastVersion("6.6.0").HasS3Parameters()

	s3secret := "s3-secret"
	s3BucketName := "s3bucket-" + kubernetes.Namespace

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

	if _, err := kubernetes.KubeClient.CoreV1().Secrets(kubernetes.Namespace).Create(context.Background(), secret, v1.CreateOptions{}); err != nil {
		e2eutil.Die(t, err)
	}

	MustCreateS3Bucket(t, s3BucketName, f.S3AccessKey, f.S3SecretID, f.S3Region, "", nil)

	// Note: deferred functions must not call Die.
	cleanup := func() {
		_ = deleteS3Bucket(t, s3BucketName, f.S3AccessKey, f.S3SecretID, f.S3Region, "", nil)
	}

	return secret, s3BucketName, cleanup
}

func testFullIncremental(t *testing.T, s3 bool) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	// Create a normal cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewIncrementalBackup(e2eutil.DefaultSchedule(), e2eutil.ScheduleIn(5*time.Minute)).ToS3(s3BucketName).MustCreate(t, kubernetes)
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
	testFullIncremental(t, false)
}

func TestBackupFullIncrementalS3(t *testing.T) {
	testFullIncremental(t, true)
}

func testFullOnly(t *testing.T, s3 bool) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	// Create a normal cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToS3(s3BucketName).MustCreate(t, kubernetes)
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
	testFullOnly(t, false)
}

func TestBackupFullOnlyS3(t *testing.T) {
	testFullOnly(t, true)
}

// Cluster goes down during a backup (all pods go down)
// Tests --purge behaviour is working as expected - this should allow us to ignore the previous backup and start anew.
func testFailedBackupBehaviour(t *testing.T, s3 bool) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2

	// Create cluster.
	cluster := clusterOptions().WithMixedTopology(mdsGroupSize).Generate(kubernetes)

	if s3secret != nil {
		cluster.Spec.Backup.S3Secret = s3secret.Name
	}

	cluster.Spec.Backup.Image = f.CouchbaseBackupImage

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup, make a lot, we need this backup to be slow to the point
	// we can vaguely reliably kill pods while it's happening.
	e2eutil.MustPopulateWithDataSize(t, kubernetes, cluster, bucket.GetName(), f.CouchbaseServerImage, 1<<30, time.Minute)

	// create this backup to run every 2 minutes so we can test the backup still runs successfully after a failure.
	backup := e2eutil.NewFullBackup("*/2 * * * *").ToS3(s3BucketName).MustCreate(t, kubernetes)

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
	testFailedBackupBehaviour(t, false)
}

func TestFailedBackupBehaviourS3(t *testing.T) {
	testFailedBackupBehaviour(t, true)
}

// Make sure a new Backup PVC comes up if the Backup PVC is deleted (stupidly)
// N.B. Obviously all old data on the old PVC is gone forever and cannot be recovered.
func testBackupPVCReconcile(t *testing.T, s3 bool) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3

	// Create cluster.
	numOfDocs := f.DocsCount

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.ScheduleIn(7*time.Minute)).ToS3(s3BucketName).MustCreate(t, kubernetes)

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

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3

	// Create a normal cluster.
	numOfDocs := f.DocsCount

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// create initial backup
	backup1 := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToS3(s3BucketName).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup1, 2*time.Minute)

	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup1, e2eutil.BackupStartedEvent(cluster, backup1.Name), 5*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup1, e2eutil.BackupCompletedEvent(cluster, backup1.Name), 5*time.Minute)

	// wait for backup to be deleted
	e2eutil.MustDeleteBackup(t, kubernetes, backup1)
	e2eutil.MustWaitForBackupDeletion(t, kubernetes, backup1, 2*time.Minute)

	// create new backup
	backup2 := e2eutil.NewIncrementalBackup(e2eutil.DefaultSchedule(), e2eutil.ScheduleIn(5*time.Minute)).ToS3(s3BucketName).MustCreate(t, kubernetes)
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

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3

	// Create a normal cluster.
	numOfDocs := f.DocsCount

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// create initial backup
	backup1 := e2eutil.NewIncrementalBackup(e2eutil.DefaultSchedule(), e2eutil.ScheduleIn(5*time.Minute)).ToS3(s3BucketName).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup1, 2*time.Minute)

	// wait for backups completion and confirm they ran without error
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup1, e2eutil.BackupCompletedEvent(cluster, backup1.Name), 7*time.Minute)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup1, e2eutil.BackupCompletedEvent(cluster, backup1.Name), 7*time.Minute)

	// wait for backup to be deleted
	e2eutil.MustDeleteBackup(t, kubernetes, backup1)
	e2eutil.MustWaitForBackupDeletion(t, kubernetes, backup1, 2*time.Minute)

	// create new backup
	backup2 := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToS3(s3BucketName).MustCreate(t, kubernetes)
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
	testReplaceFullIncrementalBackup(t, false)
}

func TestReplaceFullIncrementalBackupS3(t *testing.T) {
	testReplaceFullIncrementalBackup(t, true)
}

func testBackupAndRestore(t *testing.T, s3 bool) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)

	defer s3cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Create a normal cluster.
	clusterSize := constants.Size3

	numOfDocs := f.DocsCount
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
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

	bucket = e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)

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

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Create a normal cluster.
	clusterSize := constants.Size3

	numOfDocs := f.DocsCount

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToS3(s3BucketName).MustCreate(t, kubernetes)

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
	testUpdateBackupStatus(t, false)
}

func TestUpdateBackupStatusS3(t *testing.T) {
	testUpdateBackupStatus(t, true)
}

func testMultipleBackups(t *testing.T, s3 bool) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	clusterSize := constants.Size3
	// Create a normal cluster.
	numOfDocs := f.DocsCount

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create Backup object 1.
	backup1 := e2eutil.NewIncrementalBackup(e2eutil.DefaultSchedule(), e2eutil.ScheduleIn(5*time.Minute)).ToS3(s3BucketName).MustCreate(t, kubernetes)

	// Create Backup object 2.
	backup2 := e2eutil.NewFullBackup(e2eutil.ScheduleIn(7*time.Minute)).ToS3(s3BucketName).MustCreate(t, kubernetes)

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
	testMultipleBackups(t, false)
}

func TestMultipleBackupsS3(t *testing.T) {
	testMultipleBackups(t, true)
}

func testFullIncrementalOverTLS(t *testing.T, s3 bool) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	clusterSize := constants.Size3
	// Create the cluster.
	numOfDocs := f.DocsCount

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).WithS3(s3secret).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewIncrementalBackup(e2eutil.DefaultSchedule(), e2eutil.ScheduleIn(5*time.Minute)).ToS3(s3BucketName).MustCreate(t, kubernetes)

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
	testFullIncrementalOverTLS(t, false)
}

func TestBackupFullIncrementalOverTLSS3(t *testing.T) {
	testFullIncrementalOverTLS(t, true)
}

func testFullOnlyOverTLS(t *testing.T, s3 bool, tls *e2eutil.TLSOpts, policy *v2.ClientCertificatePolicy) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Create the cluster.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, tls)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithMutualTLS(ctx, policy).WithS3(s3secret).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)

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
	testFullOnlyOverTLS(t, false, &e2eutil.TLSOpts{}, nil)
}

func TestBackupFullOnlyOverTLSStandard(t *testing.T) {
	keyEncoding := e2eutil.KeyEncodingPKCS8
	opts := &e2eutil.TLSOpts{Source: e2eutil.TLSSourceCertManagerSecret, KeyEncoding: &keyEncoding}

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
	opts := &e2eutil.TLSOpts{Source: e2eutil.TLSSourceCertManagerSecret, KeyEncoding: &keyEncoding}

	testFullOnlyOverTLS(t, true, opts, nil)
}

func testBackupRetention(t *testing.T, s3 bool) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// Trigger a full backup.
	backup := e2eutil.NewFullBackup(e2eutil.ScheduleIn(5*time.Minute)).ToS3(s3BucketName).WithRetention(time.Minute).MustCreate(t, kubernetes)
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
	testBackupRetention(t, false)
}

func TestBackupRetentionS3(t *testing.T) {
	testBackupRetention(t, true)
}

// Manually editing the size of the PVC in a CouchbaseBackup should be reflected in the PVC and PV
// N.B. Requires a SC with allowVolumeExpansion: true.
func testBackupPVCResize(t *testing.T, s3 bool) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

	framework.Requires(t, kubernetes).StaticCluster().ExpandableStorage()

	// Static configuration.
	clusterSize := 3

	// Create cluster.
	numOfDocs := 100

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToS3(s3BucketName).MustCreate(t, kubernetes)

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

func testBackupAndRestoreDisableEventing(t *testing.T, s3 bool) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3
	dataQuota := e2espec.NewResourceQuantityMi(int64(256 * 3))
	buckets := []v1.Object{}
	numOfDocs := f.DocsCount

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).Generate(kubernetes)

	cluster.Spec.Servers[0].Services = append(cluster.Spec.Servers[0].Services, v2.EventingService)
	cluster.Spec.ClusterSettings.DataServiceMemQuota = dataQuota
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	for i := 0; i < 3; i++ {
		bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
		bucket.SetName(fmt.Sprintf("%s-%d", bucket.GetName(), i))
		buckets = append(buckets, bucket)
		e2eutil.MustNewBucket(t, kubernetes, bucket)
		e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)
	}

	// create eventing function and verify
	e2eutil.MustDeployEventingFunction(t, kubernetes, cluster, "test", buckets[0].GetName(), buckets[1].GetName(), buckets[2].GetName(), function, time.Minute)
	e2eutil.NewDocumentSet(buckets[0].GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, buckets[2].GetName(), f.DocsCount, 5*time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToS3(s3BucketName).MustCreate(t, kubernetes)

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
	e2eutil.MustDeleteEventingFunction(t, kubernetes, cluster, time.Minute)
	time.Sleep(30 * time.Second)

	for _, bucket := range buckets {
		e2eutil.MustNewBucket(t, kubernetes, bucket)
		e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)
	}

	// create new restore
	e2eutil.NewRestore(backup).FromS3(s3BucketName).WithoutEventing().MustCreate(t, kubernetes)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// if eventing function is restored raise error.
	if err := e2eutil.MustGetEventingFunction(t, kubernetes, cluster, time.Minute); err == nil {
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
	testBackupAndRestoreDisableEventing(t, false)
}

func TestBackupAndRestoreDisableEventingS3(t *testing.T) {
	testBackupAndRestoreDisableEventing(t, true)
}

func testBackupAndRestoreDisableGSI(t *testing.T, s3 bool) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, kubernetes)

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
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
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToS3(s3BucketName).MustCreate(t, kubernetes)

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
	e2eutil.NewRestore(backup).FromS3(s3BucketName).WithoutGSI().MustCreate(t, kubernetes)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check Index is not restored.
	indexCount = e2eutil.MustGetIndexCount(t, kubernetes, cluster, 2*time.Minute)
	if indexCount != 0 {
		e2eutil.Die(t, fmt.Errorf("Index `#primary` restored"))
	}

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

func TestBackupAndRestoreDisableGSI(t *testing.T) {
	testBackupAndRestoreDisableGSI(t, false)
}

func TestBackupAndRestoreDisableGSIS3(t *testing.T) {
	testBackupAndRestoreDisableGSI(t, true)
}

func testBackupAndRestoreDisableAnalytics(t *testing.T, s3 bool) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	numOfDocs := f.DocsCount
	clusterSize := constants.Size3

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).Generate(kubernetes)

	cluster.Spec.Servers[0].Services = append(cluster.Spec.Servers[0].Services, v2.AnalyticsService)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
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
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToS3(s3BucketName).MustCreate(t, kubernetes)

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
	e2eutil.NewRestore(backup).FromS3(s3BucketName).WithoutAnalytics().MustCreate(t, kubernetes)
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
	testBackupAndRestoreDisableAnalytics(t, false)
}

func TestBackupAndRestoreDisableAnalyticsS3(t *testing.T) {
	testBackupAndRestoreDisableAnalytics(t, true)
}

func testBackupAndRestoreDisableData(t *testing.T, s3 bool) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, kubernetes)

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToS3(s3BucketName).MustCreate(t, kubernetes)

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
	e2eutil.NewRestore(backup).FromS3(s3BucketName).WithoutData().MustCreate(t, kubernetes)
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
	testBackupAndRestoreDisableData(t, false)
}

func TestBackupAndRestoreDisableDataS3(t *testing.T) {
	testBackupAndRestoreDisableData(t, true)
}

func testBackupAndRestoreEnableBucketConfig(t *testing.T, s3 bool) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	numOfDocs := f.DocsCount
	clusterSize := constants.Size3

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, kubernetes)

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToS3(s3BucketName).MustCreate(t, kubernetes)

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
	e2eutil.NewRestore(backup).FromS3(s3BucketName).WithBucketConfig().MustCreate(t, kubernetes)
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
	testBackupAndRestoreEnableBucketConfig(t, false)
}

func TestBackupAndRestoreEnableBucketConfigS3(t *testing.T) {
	testBackupAndRestoreEnableBucketConfig(t, true)
}

func testBackupAndRestoreMapBuckets(t *testing.T, s3 bool) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	targetBucketName := "bucketty-mcbuccketface"
	// Create a normal cluster.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, kubernetes)

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToS3(s3BucketName).MustCreate(t, kubernetes)

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
	e2eutil.NewRestore(backup).FromS3(s3BucketName).WithMapping(bucket.GetName(), targetBucketName).MustCreate(t, kubernetes)
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
	testBackupAndRestoreMapBuckets(t, false)
}

func TestBackupAndRestoreMapBucketsS3(t *testing.T) {
	testBackupAndRestoreMapBuckets(t, true)
}

func testBackupAndRestoreIncludeBuckets(t *testing.T, s3 bool) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Create a normal cluster.
	clusterSize := constants.Size3
	buckets := []v1.Object{}
	numOfDocs := f.DocsCount

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).Generate(kubernetes)
	cluster.Spec.ClusterSettings.DataServiceMemQuota = e2espec.NewResourceQuantityMi(int64(2 * 256))
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	for i := 0; i < 2; i++ {
		bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
		bucket.SetName(fmt.Sprintf("%s-%d", bucket.GetName(), i))
		buckets = append(buckets, bucket)
		e2eutil.MustNewBucket(t, kubernetes, bucket)
		e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)
		e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
		e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), f.DocsCount, time.Minute)
	}

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToS3(s3BucketName).MustCreate(t, kubernetes)

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
	e2eutil.NewRestore(backup).FromS3(s3BucketName).WithIncludes(buckets[0].GetName()).MustCreate(t, kubernetes)
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
	testBackupAndRestoreIncludeBuckets(t, false)
}

func TestBackupAndRestoreIncludeBucketsS3(t *testing.T) {
	testBackupAndRestoreIncludeBuckets(t, true)
}

func testBackupAndRestoreExcludeBuckets(t *testing.T, s3 bool) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Create a normal cluster.
	clusterSize := constants.Size3
	buckets := []v1.Object{}
	numOfDocs := f.DocsCount

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).Generate(kubernetes)
	cluster.Spec.ClusterSettings.DataServiceMemQuota = e2espec.NewResourceQuantityMi(int64(2 * 256))
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	for i := 0; i < 2; i++ {
		bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
		bucket.SetName(fmt.Sprintf("%s-%d", bucket.GetName(), i))
		buckets = append(buckets, bucket)

		e2eutil.MustNewBucket(t, kubernetes, bucket)
		e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

		// Populate the bucket
		e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
		e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), f.DocsCount, time.Minute)
	}

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToS3(s3BucketName).MustCreate(t, kubernetes)

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
	e2eutil.NewRestore(backup).FromS3(s3BucketName).WithExcludes(buckets[0].GetName()).MustCreate(t, kubernetes)
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
	testBackupAndRestoreExcludeBuckets(t, false)
}

func TestBackupAndRestoreExcludeBucketsS3(t *testing.T) {
	testBackupAndRestoreExcludeBuckets(t, true)
}

func testBackupAndRestoreNodeSelector(t *testing.T, s3 bool) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	nodes, err := kubernetes.KubeClient.CoreV1().Nodes().List(context.Background(), v1.ListOptions{})
	if err != nil {
		e2eutil.Die(t, fmt.Errorf("failed to get node list: %w", err))
	}

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := f.DocsCount

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).Generate(kubernetes)

	zone, ok := nodes.Items[0].Labels[constants.FailureDomainZoneLabel]
	if !ok {
		e2eutil.Die(nil, fmt.Errorf("node %s missing label %s", nodes.Items[0].Name, constants.FailureDomainZoneLabel))
	}

	nodeSelector := map[string]string{
		constants.FailureDomainZoneLabel: zone,
	}

	cluster.Spec.Backup.NodeSelector = nodeSelector
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToS3(s3BucketName).WithStorageClass(f.StorageClassName).MustCreate(t, kubernetes)

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
	e2eutil.NewRestore(backup).FromS3(s3BucketName).MustCreate(t, kubernetes)
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
	testBackupAndRestoreNodeSelector(t, false)
}

func TestBackupAndRestoreNodeSelectorS3(t *testing.T) {
	testBackupAndRestoreNodeSelector(t, true)
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
func testBackupAndRestoreScopesAndCollections(t *testing.T, s3 bool) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

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
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollection(collectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// insert docs in the created scope.collection.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, collectionName).MustCreate(t, kubernetes, cluster)

	// Create and wait for a backup
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).MustCreate(t, kubernetes)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// delete bucket
	e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)
	bucket = e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	// create new restore
	e2eutil.NewRestore(backup).FromS3(s3BucketName).MustCreate(t, kubernetes)
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
	testBackupAndRestoreScopesAndCollections(t, false)
}

func TestBackupAndRestoreScopesAndCollectionsS3(t *testing.T) {
	testBackupAndRestoreScopesAndCollections(t, true)
}

// testBackupAndRestoreCollection tests data backup taken of a bucket
// with managed scopes and collections,
// is restored successfully in a managed bucket with managed scopes
// and unmanaged collection.
func testBackupAndRestoreCollections(t *testing.T, s3 bool) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

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
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollection(collectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// insert docs in the created scope.collection.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, collectionName).MustCreate(t, kubernetes, cluster)

	// Create and wait for a backup
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToS3(s3BucketName).MustCreate(t, kubernetes)
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

	bucket = e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scopeNew)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	// create new restore
	e2eutil.NewRestore(backup).FromS3(s3BucketName).MustCreate(t, kubernetes)
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
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated, FuzzyMessage: bucket.GetName()},
		eventschema.Event{Reason: k8sutil.EventReasonBackupRestoreCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBackupAndRestoreCollections(t *testing.T) {
	testBackupAndRestoreCollections(t, false)
}

func TestBackupAndRestoreCollectionsS3(t *testing.T) {
	testBackupAndRestoreCollections(t, true)
}

// testBackupAndRestoreScope tests data backup taken of a managed scope
// is restored successfully in an unmanaged scope with the same name.
func testBackupAndRestoreScope(t *testing.T, s3 bool) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

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
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollection(collectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// insert docs.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, collectionName).MustCreate(t, kubernetes, cluster)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)

	// take backup of a single scope.
	backupInclude := fmt.Sprintf("%s.%s", bucket.GetName(), scopeName)

	// Create and wait for a backup
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToS3(s3BucketName).Include(backupInclude).MustCreate(t, kubernetes)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// delete bucket
	e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)
	bucket = e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	// create new restore
	e2eutil.NewRestore(backup).FromS3(s3BucketName).MustCreate(t, kubernetes)

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
	testBackupAndRestoreScope(t, false)
}

func TestBackupAndRestoreScopeS3(t *testing.T) {
	testBackupAndRestoreScope(t, true)
}

// testBackupAndRestoreCollection tests data backup taken of a managed collection
// is restored successfully in an unmanaged collection with the same name.
func testBackupAndRestoreCollection(t *testing.T, s3 bool) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3Secret(t, kubernetes, s3)
	defer s3cleanup()

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
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName1, collectionName2)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// insert docs in the created scope.collection.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, collectionName1).MustCreate(t, kubernetes, cluster)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, collectionName2).MustCreate(t, kubernetes, cluster)

	// take backup of a single scope.
	backupInclude := fmt.Sprintf("%s.%s.%s", bucket.GetName(), scopeName, collectionName1)

	// Create and wait for a backup
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToS3(s3BucketName).Include(backupInclude).MustCreate(t, kubernetes)
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 5*time.Minute)

	// delete bucket
	e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)
	bucket = e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	// create new restore
	e2eutil.NewRestore(backup).FromS3(s3BucketName).MustCreate(t, kubernetes)
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
	testBackupAndRestoreCollection(t, false)
}

func TestBackupAndRestoreCollectionS3(t *testing.T) {
	testBackupAndRestoreCollection(t, true)
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
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
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
	testBackupCustomObjectEndpoint(t, false)
}

func TestBackupCustomObjEndpointWithCert(t *testing.T) {
	testBackupCustomObjectEndpoint(t, true)
}

func testBackupCustomObjectEndpoint(t *testing.T, withCA bool) {
	f := framework.Global
	kubernetes, cleanup := f.SetupTest(t)

	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	minio, err := e2eutil.MinioOptions(kubernetes).WithName("minio").WithTLS(withCA).WithCredentials(f.MinioAccessKey, f.MinioSecretID, f.MinioRegion).Create(t)

	defer minio.CleanUp()

	if err != nil {
		e2eutil.Die(t, err)
	}

	err = minio.WaitTillReady(5 * time.Minute)
	if err != nil {
		e2eutil.Die(t, err)
	}

	time.Sleep(5 * time.Second)

	var cert []byte

	if withCA {
		cert = minio.CASecret.Data["tls.crt"]
	}

	s3secret, s3BucketName, s3cleanup := createObjEndpointS3Secret(t, kubernetes, minio.Endpoint, cert)

	defer s3cleanup()

	numOfDocs := f.DocsCount

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).WithObjEndpoint(minio.Endpoint).WithObjEndpointCert(minio.CASecret).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToS3(s3BucketName).MustCreate(t, kubernetes)

	// wait for backup
	e2eutil.MustWaitForBackup(t, kubernetes, backup, 2*time.Minute)

	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 12*time.Minute)

	// delete bucket
	e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	bucket = e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)

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
func TestBackupAndRestoreS3WithIAMRole(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	s3secret, s3BucketName, s3cleanup := createS3RegionSecret(t, kubernetes)

	defer s3cleanup()

	framework.Requires(t, kubernetes).StaticCluster().HasIAMParameters()

	// Create a normal cluster.
	clusterSize := constants.Size3

	numOfDocs := f.DocsCount
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithS3(s3secret).WithS3IAMRole(true).MustCreate(t, kubernetes)

	aws := e2eutil.AwsHelper(f.IAMAccessKey, f.IAMSecretID, f.S3Region).Create()
	e2eutil.MustSetupBackupIAM(t, kubernetes, aws, f.AWSAccountID, f.AWSOIDCProvider, s3BucketName)

	defer aws.Cleanup()

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Create a Backup object.
	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).ToS3(s3BucketName).MustCreate(t, kubernetes)

	// wait for backup to complete
	e2eutil.MustWaitForBackupEvent(t, kubernetes, backup, e2eutil.BackupCompletedEvent(cluster, backup.Name), 12*time.Minute)

	// delete bucket
	e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	bucket = e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)

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
