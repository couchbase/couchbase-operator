package e2e

import (
	"fmt"
	"strconv"
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

func createTestBackup(strategy v2.Strategy, schedules []string) *v2.CouchbaseBackup {
	incr := ""
	full := ""

	switch strategy {
	case v2.FullIncremental:
		incr = schedules[0]
		full = schedules[1]
	case v2.FullOnly:
		full = schedules[0]
	}

	backup := &v2.CouchbaseBackup{
		ObjectMeta: v1.ObjectMeta{
			Name: strings.Replace(string(strategy), "_", "-", -1),
		},
		Spec: v2.CouchbaseBackupSpec{
			Strategy: strategy,
			Incremental: &v2.CouchbaseBackupSchedule{
				Schedule: incr,
			},
			Full: &v2.CouchbaseBackupSchedule{
				Schedule: full,
			},
			Size: e2espec.NewResourceQuantityMi(2048),
		},
	}

	return backup
}

func createTestBackupNew(strategy v2.Strategy, fullSchedule, incrementalSchedule string) *v2.CouchbaseBackup {
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
// go become created.
// TODO: how do time zones come into this??
func cronScheduleOnceIn(duration time.Duration) string {
	when := time.Now().Add(duration)
	return fmt.Sprintf("%d %d * * *", when.Minute(), when.Hour())
}

// createEveryXMinutesSchedule schedules a cron job once ever N minutes.  Do not
// use this, if something starts taking longer, then it will start failing randomly.
// You can simulate multiple firings by calling cronScheduleOnceIn, waiting for the
// indended result, then patching in a new firing time.
func createEveryXMinutesSchedule(x int) string {
	return "*/" + strconv.Itoa(x) + " * * * *"
}

func TestFullIncremental(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := 200

	// Create a normal cluster.
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, targetKube.Namespace, clusterSize)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, numOfDocs, 2*time.Minute)

	// Create a Backup object.
	backup := createTestBackupNew(v2.FullIncremental, cronScheduleOnceIn(2*time.Minute), cronScheduleOnceIn(5*time.Minute))
	backup = e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, backup)
	e2eutil.MustWaitForBackup(t, targetKube, backup, time.Minute)

	// Expect the full backup to complete, followed by the the incremental.
	e2eutil.MustWaitForBackupRun(t, targetKube, backup, 5*time.Minute)
	e2eutil.MustWaitForBackupRun(t, targetKube, backup, 5*time.Minute)

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
	numOfDocs := 200

	// Create a normal cluster.
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, targetKube.Namespace, clusterSize)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, numOfDocs, 2*time.Minute)

	// Create a Backup object.
	backup := createTestBackupNew(v2.FullOnly, cronScheduleOnceIn(2*time.Minute), "")
	backup = e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, backup)
	e2eutil.MustWaitForBackup(t, targetKube, backup, time.Minute)

	// Expect the full backup to complete.
	e2eutil.MustWaitForBackupRun(t, targetKube, backup, 5*time.Minute)

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
// Tests --purge behaviour is working as expected - this should allow us to ignore the previous backup and start anew
func TestFailedBackupBehaviour(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)
	fullFreq := 2

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2
	clusterName := "test-couchbase-" + e2eutil.RandomSuffix()

	// Create cluster.
	numOfDocs := 200
	testCouchbase := e2espec.NewSupportableCluster(mdsGroupSize)
	testCouchbase.Name = clusterName
	testCouchbase.Spec.Backup.Managed = true
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, targetKube.Namespace, testCouchbase)

	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, numOfDocs, 2*time.Minute)

	// Create a Backup object.
	schedules := []string{createEveryXMinutesSchedule(fullFreq)}
	fullBackup := createTestBackup(v2.FullOnly, schedules)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullBackup)

	// wait for backup
	options := v1.GetOptions{}
	backup, err := targetKube.CRClient.CouchbaseV2().CouchbaseBackups(targetKube.Namespace).Get(fullBackup.Name, options)
	if err != nil {
		e2eutil.Die(t, err)
	}

	if backup.Name != fullBackup.Name {
		e2eutil.Die(t, fmt.Errorf("backup name actual value %s mismatch expected value %s", backup.Name, fullBackup.Name))
	}

	// wait for cronjobs to be reconciled
	e2eutil.MustWaitForCronjob(t, targetKube, testCouchbase, string(cluster.Full), time.Minute)
	// wait for cronjob first run
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, string(cluster.Full), time.Duration(fullFreq*2)*time.Minute)
	// kill the cluster
	for i := 0; i < mdsGroupSize; i++ {
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, i, false)
	}
	// wait for cluster to return
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 10*time.Minute)

	// check backup and cronjob are still up
	if _, err = targetKube.CRClient.CouchbaseV2().CouchbaseBackups(targetKube.Namespace).Get(fullBackup.Name, options); err != nil {
		e2eutil.Die(t, err)
	}
	e2eutil.MustWaitForCronjob(t, targetKube, testCouchbase, string(cluster.Full), time.Minute)

	// check for job and wait for completion
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, string(cluster.Full), time.Duration(fullFreq*2)*time.Minute)
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, string(cluster.Full), 15*time.Minute)

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
// N.B. Obviously all old data on the old PVC is gone forever and cannot be recovered
func TestBackupPVCReconcile(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)
	fullFreq := 2

	// Static configuration.
	clusterSize := constants.Size3

	// Create cluster.
	numOfDocs := 200
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, targetKube.Namespace, clusterSize)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, numOfDocs, 2*time.Minute)

	// Create a Backup object.
	schedules := []string{createEveryXMinutesSchedule(fullFreq)}
	fullBackup := createTestBackup(v2.FullOnly, schedules)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullBackup)

	// wait for backup
	options := v1.GetOptions{}
	backup, err := targetKube.CRClient.CouchbaseV2().CouchbaseBackups(targetKube.Namespace).Get(fullBackup.Name, options)
	if err != nil {
		e2eutil.Die(t, err)
	}

	if backup.Name != fullBackup.Name {
		e2eutil.Die(t, fmt.Errorf("backup name actual value %s mismatch expected value %s", backup.Name, fullBackup.Name))
	}

	// wait for cronjobs to be reconciled
	e2eutil.MustWaitForCronjob(t, targetKube, testCouchbase, string(cluster.Full), time.Minute)
	// check for job
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, string(cluster.Full), time.Duration(fullFreq*2)*time.Minute)

	// check pvc exists
	pvc, err := targetKube.KubeClient.CoreV1().PersistentVolumeClaims(targetKube.Namespace).Get(fullBackup.Name, v1.GetOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}
	// check pvc has same name as backup
	if pvc.Name != backup.Name {
		e2eutil.Die(t, fmt.Errorf("pvc name %s is a mismatch with backup name %s", pvc.Name, backup.Name))
	}

	// delete backup!
	e2eutil.MustDeleteBackup(t, targetKube, fullBackup)

	// DELET this
	if err := e2eutil.DeleteAndWaitForPVCDeletionSingle(targetKube, pvc.Name, targetKube.Namespace, 5*time.Minute); err != nil {
		e2eutil.Die(t, err)
	}

	// create backup again
	fullBackup = e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullBackup)

	// wait for backup
	backup, err = targetKube.CRClient.CouchbaseV2().CouchbaseBackups(targetKube.Namespace).Get(fullBackup.Name, options)
	if err != nil {
		e2eutil.Die(t, err)
	}

	if backup.Name != fullBackup.Name {
		e2eutil.Die(t, fmt.Errorf("backup name actual value %s mismatch expected value %s", backup.Name, fullBackup.Name))
	}

	e2eutil.MustWaitForCronjob(t, targetKube, testCouchbase, string(cluster.Full), time.Minute)

	// check pvc exists
	pvc, err = targetKube.KubeClient.CoreV1().PersistentVolumeClaims(targetKube.Namespace).Get(fullBackup.Name, v1.GetOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}
	// check pvc has same name as backup
	if pvc.Name != backup.Name {
		e2eutil.Die(t, fmt.Errorf("pvc name %s is a mismatch with backup name %s", pvc.Name, backup.Name))
	}

	// check for job and wait for completion
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, string(cluster.Full), time.Duration(fullFreq*2)*time.Minute)
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, string(cluster.Full), 15*time.Minute)

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
		eventschema.Event{Reason: k8sutil.EventReasonBackupDeleted, FuzzyMessage: string(cluster.Full)},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Full)},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// check that replacing a CouchbaseBackup works as expected
// delete CouchbaseBackup which should then delete Cronjobs and Jobs
// create new full-only CouchbaseBackup
// wait for backup to perform
// delete backup
// create new full/incremental CouchbaseBackup
func TestReplaceFullOnlyBackup(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	initialFullFreq := 1

	incrFreq := 2
	fullFreq := 5

	// Static configuration.
	clusterSize := constants.Size3

	// Create a normal cluster.
	numOfDocs := 200
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, targetKube.Namespace, clusterSize)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, numOfDocs, 2*time.Minute)

	// Create Backup objects.
	schedules := []string{createEveryXMinutesSchedule(initialFullFreq)}
	fullBackup := createTestBackup(v2.FullOnly, schedules)

	schedules = []string{createEveryXMinutesSchedule(incrFreq), createEveryXMinutesSchedule(fullFreq)}
	fullIncrementalBackup := createTestBackup(v2.FullIncremental, schedules)

	// create initial backup
	fullBackup = e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullBackup)

	// wait for backup
	options := v1.GetOptions{}
	backup, err := targetKube.CRClient.CouchbaseV2().CouchbaseBackups(targetKube.Namespace).Get(fullBackup.Name, options)
	if err != nil {
		e2eutil.Die(t, err)
	}

	if backup.Name != fullBackup.Name {
		e2eutil.Die(t, fmt.Errorf("backup name actual value %s mismatch expected value %s", backup.Name, fullBackup.Name))
	}

	// wait for cronjob to be reconciled
	e2eutil.MustWaitForCronjob(t, targetKube, testCouchbase, string(cluster.Full), time.Minute)

	// check for jobs, initial job and job run via Cron
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, string(cluster.Full), time.Duration(initialFullFreq*2)*time.Minute)
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, string(cluster.Full), 5*time.Minute)

	// DELET THIS
	e2eutil.MustDeleteBackup(t, targetKube, fullBackup)
	// wait for backup to be deleted
	e2eutil.MustWaitForBackupDeletion(t, targetKube, targetKube.Namespace, 2*time.Minute)

	// create new backup
	fullIncrementalBackup = e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullIncrementalBackup)
	newBackup, err := targetKube.CRClient.CouchbaseV2().CouchbaseBackups(targetKube.Namespace).Get(fullIncrementalBackup.Name, options)
	if err != nil {
		e2eutil.Die(t, err)
	}

	if newBackup.Name != fullIncrementalBackup.Name {
		e2eutil.Die(t, fmt.Errorf("backup name actual value %s mismatch expected value %s", backup.Name, fullBackup.Name))
	}

	// wait for cronjobs to be reconciled
	e2eutil.MustWaitForCronjob(t, targetKube, testCouchbase, string(cluster.Incremental), time.Minute)
	e2eutil.MustWaitForCronjob(t, targetKube, testCouchbase, string(cluster.Full), time.Minute)

	// check for jobs, initial job and job run via Cron
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, string(cluster.Full), time.Duration(fullFreq*2)*time.Minute)
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, string(cluster.Incremental), time.Duration(incrFreq)*time.Minute)

	// wait for jobs completion and confirm they ran without error
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, string(cluster.Full), 5*time.Minute)
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, string(cluster.Incremental), 5*time.Minute)

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
// create new full-only CouchbaseBackup
func TestReplaceFullIncrementalBackup(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	incrFreq := 2
	fullFreq := 5

	laterFullFreq := 1

	// Static configuration.
	clusterSize := constants.Size3

	// Create a normal cluster.
	numOfDocs := 200
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, targetKube.Namespace, clusterSize)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, numOfDocs, 2*time.Minute)

	// Create Backup objects.
	schedules := []string{createEveryXMinutesSchedule(laterFullFreq)}
	fullBackup := createTestBackup(v2.FullOnly, schedules)

	schedules = []string{createEveryXMinutesSchedule(incrFreq), createEveryXMinutesSchedule(fullFreq)}
	fullIncrementalBackup := createTestBackup(v2.FullIncremental, schedules)

	// create initial backup
	fullIncrementalBackup = e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullIncrementalBackup)

	// wait for backup
	options := v1.GetOptions{}
	backup, err := targetKube.CRClient.CouchbaseV2().CouchbaseBackups(targetKube.Namespace).Get(fullIncrementalBackup.Name, options)
	if err != nil {
		e2eutil.Die(t, err)
	}

	if backup.Name != fullIncrementalBackup.Name {
		e2eutil.Die(t, fmt.Errorf("backup name actual value %s mismatch expected value %s", backup.Name, fullIncrementalBackup.Name))
	}

	// wait for cronjobs to be reconciled
	e2eutil.MustWaitForCronjob(t, targetKube, testCouchbase, string(cluster.Incremental), time.Minute)
	e2eutil.MustWaitForCronjob(t, targetKube, testCouchbase, string(cluster.Full), time.Minute)

	// check for jobs, initial job and job run via Cron
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, string(cluster.Full), time.Duration(fullFreq*2)*time.Minute)
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, string(cluster.Incremental), time.Duration(incrFreq)*time.Minute)

	// wait for jobs completion and confirm they ran without error
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, string(cluster.Full), 5*time.Minute)
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, string(cluster.Incremental), 5*time.Minute)

	// DELET THIS
	e2eutil.MustDeleteBackup(t, targetKube, fullIncrementalBackup)
	// wait for backup to be deleted
	e2eutil.MustWaitForBackupDeletion(t, targetKube, targetKube.Namespace, 2*time.Minute)

	// create new backup
	fullBackup = e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullBackup)
	newBackup, err := targetKube.CRClient.CouchbaseV2().CouchbaseBackups(targetKube.Namespace).Get(fullBackup.Name, options)
	if err != nil {
		e2eutil.Die(t, err)
	}

	if newBackup.Name != fullBackup.Name {
		e2eutil.Die(t, fmt.Errorf("backup name actual value %s mismatch expected value %s", backup.Name, fullBackup.Name))
	}

	// wait for cronjob to be reconciled
	e2eutil.MustWaitForCronjob(t, targetKube, testCouchbase, string(cluster.Full), time.Minute)

	// check for jobs, initial job and job run via Cron
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, string(cluster.Full), time.Duration(laterFullFreq*2)*time.Minute)
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, string(cluster.Full), 5*time.Minute)

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
	fullFreq := 1

	// Create a normal cluster.
	clusterSize := constants.Size3

	numOfDocs := 2000
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, targetKube.Namespace, clusterSize)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	time.Sleep(time.Minute) // wait for docs to be inserted
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, numOfDocs, time.Minute)

	// Create a Backup object.
	schedules := []string{createEveryXMinutesSchedule(fullFreq)}
	fullBackup := createTestBackup(v2.FullOnly, schedules)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullBackup)

	// wait for backup
	options := v1.GetOptions{}
	backup, err := targetKube.CRClient.CouchbaseV2().CouchbaseBackups(targetKube.Namespace).Get(fullBackup.Name, options)
	if err != nil {
		e2eutil.Die(t, err)
	}

	if backup.Name != fullBackup.Name {
		e2eutil.Die(t, fmt.Errorf("backup name actual value %s mismatch expected value %s", backup.Name, fullBackup.Name))
	}

	// wait for cronjob to be reconciled
	e2eutil.MustWaitForCronjob(t, targetKube, testCouchbase, string(cluster.Full), time.Minute)

	// check and wait for initial job
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, string(cluster.Full), time.Duration(fullFreq)*time.Minute)
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, string(cluster.Full), 5*time.Minute)

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

	// check and wait for job run via Cron
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, "full-", time.Duration(fullFreq*2)*time.Minute)
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, "full-", 5*time.Minute)

	// delete bucket
	e2eutil.MustDeleteBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketNotExists(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 10*time.Minute)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, 5*time.Minute)

	// create new restore
	_ = e2eutil.MustNewBackupRestore(t, targetKube, targetKube.Namespace, restore)

	// restore job is too fast, just validate bucket item count
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, numOfDocs, time.Minute)

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
// LastSuccess, RepoList and Duration fields will be updated once the job is finished
func TestUpdateBackupStatus(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)
	fullFreq := 1

	// Create a normal cluster.
	clusterSize := constants.Size3

	numOfDocs := 200
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, targetKube.Namespace, clusterSize)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, numOfDocs, time.Minute)

	// Create a Backup object.
	schedules := []string{createEveryXMinutesSchedule(fullFreq)}
	fullBackup := createTestBackup(v2.FullOnly, schedules)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullBackup)

	// wait for backup
	options := v1.GetOptions{}
	backup, err := targetKube.CRClient.CouchbaseV2().CouchbaseBackups(targetKube.Namespace).Get(fullBackup.Name, options)
	if err != nil {
		e2eutil.Die(t, err)
	}

	if backup.Name != fullBackup.Name {
		e2eutil.Die(t, fmt.Errorf("backup name actual value %s mismatch expected value %s", backup.Name, fullBackup.Name))
	}

	// wait for cronjob to be reconciled
	e2eutil.MustWaitForCronjob(t, targetKube, testCouchbase, string(cluster.Full), time.Minute)

	// check and wait for first cronjob run
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, string(cluster.Full), time.Duration(fullFreq)*2*time.Minute)
	// wait for job completion
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, string(cluster.Full), time.Minute)
	e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "LastRun", time.Minute)

	// wait for backup status update
	e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "Archive", time.Minute)
	e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "Repo", time.Minute)
	e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "RepoList", time.Minute)
	e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "LastSuccess", time.Minute)
	e2eutil.MustWaitStatusUpdate(t, targetKube, fullBackup.Name, "Duration", time.Minute)

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

	incrementalFreq := 2
	fullFreq := 5

	clusterSize := constants.Size3
	// Create a normal cluster.
	numOfDocs := 200
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, targetKube.Namespace, clusterSize)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, numOfDocs, 2*time.Minute)

	// Create Backup object 1.
	incrSchedules := []string{createEveryXMinutesSchedule(incrementalFreq), createEveryXMinutesSchedule(fullFreq)}
	fullIncrementalBackup := createTestBackup(v2.FullIncremental, incrSchedules)
	fullIncrementalBackup = e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullIncrementalBackup)

	// Create Backup object 2.
	fullSchedules := []string{createEveryXMinutesSchedule(fullFreq)}
	fullBackup := createTestBackup(v2.FullOnly, fullSchedules)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullBackup)

	// wait for backups
	options := v1.GetOptions{}
	backup, err := targetKube.CRClient.CouchbaseV2().CouchbaseBackups(targetKube.Namespace).Get(fullIncrementalBackup.Name, options)
	if err != nil {
		e2eutil.Die(t, err)
	}

	if backup.Name != fullIncrementalBackup.Name {
		e2eutil.Die(t, fmt.Errorf("backup name actual value %s mismatch expected value %s", backup.Name, fullIncrementalBackup.Name))
	}

	options = v1.GetOptions{}
	backup, err = targetKube.CRClient.CouchbaseV2().CouchbaseBackups(targetKube.Namespace).Get(fullBackup.Name, options)
	if err != nil {
		e2eutil.Die(t, err)
	}

	if backup.Name != fullBackup.Name {
		e2eutil.Die(t, fmt.Errorf("backup name actual value %s mismatch expected value %s", backup.Name, fullBackup.Name))
	}

	// wait for cronjob to be reconciled
	e2eutil.MustWaitForCronjob(t, targetKube, testCouchbase, string(cluster.Full), time.Minute)
	e2eutil.MustWaitForCronjob(t, targetKube, testCouchbase, string(cluster.Incremental), time.Minute)
	e2eutil.MustWaitForCronjob(t, targetKube, testCouchbase, string(cluster.Full), time.Minute)

	// check for jobs, initial jobs and jobs run via Cron
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, "full-", time.Duration(fullFreq*2)*time.Minute)
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, "incremental-", time.Duration(incrementalFreq*2)*time.Minute)
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, "full-", time.Duration(fullFreq*2)*time.Minute)

	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, "full-", 5*time.Minute)
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, "incremental-", 5*time.Minute)
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, "full-", 5*time.Minute)

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

	incrementalFreq := 2
	fullFreq := 5

	clusterSize := constants.Size3

	// Create the cluster.
	numOfDocs := 200

	// Create the cluster.
	ctx, teardown := e2eutil.MustInitClusterTLS(t, targetKube, targetKube.Namespace, &e2eutil.TLSOpts{})
	defer teardown()

	testCouchbase := e2espec.NewBackupCluster(clusterSize)
	testCouchbase.Spec.Networking.TLS = &v2.TLSPolicy{
		Static: &v2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, targetKube.Namespace, testCouchbase)

	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, numOfDocs, 2*time.Minute)

	// Create a Backup object.
	schedules := []string{createEveryXMinutesSchedule(incrementalFreq), createEveryXMinutesSchedule(fullFreq)}
	fullIncrementalBackup := createTestBackup(v2.FullIncremental, schedules)
	fullIncrementalBackup = e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullIncrementalBackup)

	// wait for backup
	options := v1.GetOptions{}
	backup, err := targetKube.CRClient.CouchbaseV2().CouchbaseBackups(targetKube.Namespace).Get(fullIncrementalBackup.Name, options)
	if err != nil {
		e2eutil.Die(t, err)
	}

	if backup.Name != fullIncrementalBackup.Name {
		e2eutil.Die(t, fmt.Errorf("backup name actual value %s mismatch expected value %s", backup.Name, fullIncrementalBackup.Name))
	}

	// wait for cronjobs to be reconciled
	e2eutil.MustWaitForCronjob(t, targetKube, testCouchbase, string(cluster.Full), 2*time.Minute)
	e2eutil.MustWaitForCronjob(t, targetKube, testCouchbase, string(cluster.Incremental), 2*time.Minute)

	// check for jobs, initial job and jobs run via Cron
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, "full-", time.Duration(fullFreq*2)*time.Minute)
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, "incremental-", time.Duration(incrementalFreq*2)*time.Minute)

	// wait for jobs completion and confirm they ran without error
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, "full-", 5*time.Minute)
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, "incremental-", 5*time.Minute)

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

	fullFreq := 2

	clusterSize := constants.Size3

	// Create the cluster.
	numOfDocs := 200

	// Create the cluster.
	ctx, teardown := e2eutil.MustInitClusterTLS(t, targetKube, targetKube.Namespace, &e2eutil.TLSOpts{})
	defer teardown()

	testCouchbase := e2espec.NewBackupCluster(clusterSize)
	testCouchbase.Spec.Networking.TLS = &v2.TLSPolicy{
		Static: &v2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, targetKube.Namespace, testCouchbase)

	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, 2*time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, numOfDocs, 2*time.Minute)

	// Create a Backup object.
	schedules := []string{createEveryXMinutesSchedule(fullFreq)}
	fullBackup := createTestBackup(v2.FullOnly, schedules)
	fullBackup = e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullBackup)

	// wait for backup
	options := v1.GetOptions{}
	backup, err := targetKube.CRClient.CouchbaseV2().CouchbaseBackups(targetKube.Namespace).Get(fullBackup.Name, options)
	if err != nil {
		e2eutil.Die(t, err)
	}

	if backup.Name != fullBackup.Name {
		e2eutil.Die(t, fmt.Errorf("backup name actual value %s mismatch expected value %s", backup.Name, fullBackup.Name))
	}

	// wait for cronjobs to be reconciled
	e2eutil.MustWaitForCronjob(t, targetKube, testCouchbase, string(cluster.Full), 2*time.Minute)

	// check for jobs, initial job and jobs run via Cron
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, "full-", time.Duration(fullFreq*2)*time.Minute)

	// wait for jobs completion and confirm they ran without error
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, "full-", 5*time.Minute)

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
