package e2e

import (
	"fmt"
	v2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/cluster"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strconv"
	"strings"
	"testing"
	"time"
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
		},
	}

	return backup
}

func createEveryXMinutesSchedule(x int) string {
	return "*/" + strconv.Itoa(x) + " * * * *"
}

func TestFullIncremental(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	incrementalFreq := 1
	fullFreq := 2

	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2
	// Create a normal cluster.
	numOfDocs := 200
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, targetKube.Namespace, mdsGroupSize)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 0, numOfDocs)
	//time.Sleep(time.Minute) // wait for documents to be inserted

	// Create a Backup object.
	schedules := []string{createEveryXMinutesSchedule(incrementalFreq), createEveryXMinutesSchedule(fullFreq)}
	fullIncrementalBackup := createTestBackup(v2.FullIncremental, schedules)
	e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullIncrementalBackup)

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
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, "full-", 2*time.Minute)
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, "incremental-", 2*time.Minute)

	// cleanup
	e2eutil.MustDeleteBackup(t, targetKube, targetKube.Namespace, fullIncrementalBackup)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket Created
	// * Correct Cronjob(s) created
	// * Initial job created
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
	fullFreq := 2

	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2
	// Create a normal cluster.
	numOfDocs := 200
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, targetKube.Namespace, mdsGroupSize)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 0, numOfDocs)
	time.Sleep(time.Minute) // wait for documents to be inserted

	// Create a Backup object.
	schedules := []string{createEveryXMinutesSchedule(fullFreq)}
	fullBackup := createTestBackup(v2.FullOnly, schedules)
	e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullBackup)

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
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, "full-", time.Duration(fullFreq*2)*time.Minute)
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, "full-", 2*time.Minute)

	// cleanup
	e2eutil.MustDeleteBackup(t, targetKube, targetKube.Namespace, fullBackup)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket Created
	// * Correct Cronjob(s) created
	// * Initial job created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Full)},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Cluster goes down during a backup
// Test --purge option - this should allow us to ignore the previous backup and start anew
func TestFailedBackupBehaviour(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)
	fullFreq := 2

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2

	// Create cluster.
	numOfDocs := 2000
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, targetKube.Namespace, mdsGroupSize)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 0, numOfDocs)

	// Create a Backup object.
	schedules := []string{createEveryXMinutesSchedule(fullFreq)}
	fullBackup := createTestBackup(v2.FullOnly, schedules)
	e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullBackup)

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
	fmt.Println("kill cluster")
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, 0, false)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, 1, false)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, 2, false)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, 3, false)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, 1), time.Minute)
	// wait for cluster to return
	fmt.Println("wait for cluster to return")
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 10*time.Minute)

	// check backup and cronjob are still up
	backup, err = targetKube.CRClient.CouchbaseV2().CouchbaseBackups(targetKube.Namespace).Get(fullBackup.Name, options)
	if err != nil {
		e2eutil.Die(t, err)
	}

	if backup.Name != fullBackup.Name {
		e2eutil.Die(t, fmt.Errorf("backup name actual value %s mismatch expected value %s", backup.Name, fullBackup.Name))
	}
	e2eutil.MustWaitForCronjob(t, targetKube, testCouchbase, string(cluster.Full), time.Minute)

	// check for job and wait for completion
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, string(cluster.Full), time.Duration(fullFreq*2)*time.Minute)
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, string(cluster.Full), 2*time.Minute)

	// cleanup
	e2eutil.MustDeleteBackup(t, targetKube, targetKube.Namespace, fullBackup)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket Created
	// * Correct Cronjob(s) created
	// * Initial job created
	// * Cluster created
	// * Bucket created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Full)},
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
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
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2

	// Create cluster.
	numOfDocs := 2000
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, targetKube.Namespace, mdsGroupSize)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 0, numOfDocs)

	// Create a Backup object.
	schedules := []string{createEveryXMinutesSchedule(fullFreq)}
	fullBackup := createTestBackup(v2.FullOnly, schedules)
	e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullBackup)

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
	err = targetKube.CRClient.CouchbaseV2().CouchbaseBackups(targetKube.Namespace).Delete(fullBackup.Name, &v1.DeleteOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	// DELET this
	if err := e2eutil.DeleteAndWaitForPVCDeletionSingle(targetKube, pvc.Name, targetKube.Namespace, 5*time.Minute); err != nil {
		e2eutil.Die(t, err)
	}

	// create backup again
	e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullBackup)

	// wait for backup
	backup, err = targetKube.CRClient.CouchbaseV2().CouchbaseBackups(targetKube.Namespace).Get(fullBackup.Name, options)
	if err != nil {
		e2eutil.Die(t, err)
	}

	if backup.Name != fullBackup.Name {
		e2eutil.Die(t, fmt.Errorf("backup name actual value %s mismatch expected value %s", backup.Name, fullBackup.Name))
	}

	e2eutil.MustWaitForCronjob(t, targetKube, testCouchbase, string(cluster.Full), time.Minute)

	// check for job and wait for completion
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, string(cluster.Full), time.Duration(fullFreq*2)*time.Minute)
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, string(cluster.Full), 2*time.Minute)

	// check pvc exists
	pvc, err = targetKube.KubeClient.CoreV1().PersistentVolumeClaims(targetKube.Namespace).Get(fullBackup.Name, v1.GetOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}
	// check pvc has same name as backup
	if pvc.Name != backup.Name {
		e2eutil.Die(t, fmt.Errorf("pvc name %s is a mismatch with backup name %s", pvc.Name, backup.Name))
	}

	// cleanup
	e2eutil.MustDeleteBackup(t, targetKube, targetKube.Namespace, fullBackup)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket Created
	// * Correct Cronjob(s) created
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Full)},
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

	incrFreq := 3
	fullFreq := 2

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2

	// Create a normal cluster.
	numOfDocs := 200
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, targetKube.Namespace, mdsGroupSize)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 0, numOfDocs)
	time.Sleep(time.Minute) // wait for documents to be inserted

	// Create Backup objects.
	schedules := []string{createEveryXMinutesSchedule(fullFreq)}
	fullBackup := createTestBackup(v2.FullOnly, schedules)

	schedules = []string{createEveryXMinutesSchedule(incrFreq), createEveryXMinutesSchedule(fullFreq)}
	fullIncrementalBackup := createTestBackup(v2.FullIncremental, schedules)

	// create initial backup
	e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullBackup)

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
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, string(cluster.Full), time.Duration(fullFreq*2)*time.Minute)
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, string(cluster.Full), 2*time.Minute)

	// DELET THIS
	e2eutil.MustDeleteBackup(t, targetKube, targetKube.Namespace, fullBackup)

	// create new backup
	e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullIncrementalBackup)
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
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, string(cluster.Full), 2*time.Minute)
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, string(cluster.Incremental), 2*time.Minute)

	// cleanup
	e2eutil.MustDeleteBackup(t, targetKube, targetKube.Namespace, fullIncrementalBackup)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket Created
	// * Backup Created
	// * Correct Cronjob(s) created
	// * Backup Deleted
	// * Backup Created
	// * Correct Cronjob(s) created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: fullBackup.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Incremental)},
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

	incrFreq := 3
	fullFreq := 2

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2

	// Create a normal cluster.
	numOfDocs := 200
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, targetKube.Namespace, mdsGroupSize)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 0, numOfDocs)
	time.Sleep(time.Minute) // wait for documents to be inserted

	// Create Backup objects.
	schedules := []string{createEveryXMinutesSchedule(fullFreq)}
	fullBackup := createTestBackup(v2.FullOnly, schedules)

	schedules = []string{createEveryXMinutesSchedule(incrFreq), createEveryXMinutesSchedule(fullFreq)}
	fullIncrementalBackup := createTestBackup(v2.FullIncremental, schedules)

	// create initial backup
	e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullIncrementalBackup)

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
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, string(cluster.Full), 2*time.Minute)
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, string(cluster.Incremental), 2*time.Minute)

	// DELET THIS
	e2eutil.MustDeleteBackup(t, targetKube, targetKube.Namespace, fullIncrementalBackup)

	// create new backup
	e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullBackup)
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
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, string(cluster.Full), time.Duration(fullFreq*2)*time.Minute)
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, string(cluster.Full), 2*time.Minute)

	// cleanup
	e2eutil.MustDeleteBackup(t, targetKube, targetKube.Namespace, fullBackup)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket Created
	// * Backup Created
	// * Correct Cronjob(s) created
	// * Backup Deleted
	// * Backup Created
	// * Correct Cronjob(s) created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Incremental)},
		eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Full)},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestBackupAndRestore(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)
	fullFreq := 3

	// Create a normal cluster.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2

	numOfDocs := 200
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, targetKube.Namespace, mdsGroupSize)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 0, numOfDocs)
	time.Sleep(time.Minute) // wait for documents to be inserted
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, numOfDocs, time.Minute)

	// Create a Backup object.
	schedules := []string{createEveryXMinutesSchedule(fullFreq)}
	fullBackup := createTestBackup(v2.FullOnly, schedules)
	e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullBackup)

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
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, string(cluster.Full), 2*time.Minute)

	// wait for backup status update
	repo := e2eutil.MustWaitStatusUpdate(t, targetKube, testCouchbase, fullBackup.Name, "Repo", 2*time.Minute)

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
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, "full-", 2*time.Minute)
	fmt.Println("backup should be completed")

	// delete bucket
	e2eutil.MustDeleteBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketNotExists(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 2*time.Minute)

	// wait for new bucket to be created and create new bucket
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 4*time.Minute)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, 5*time.Minute)

	// create new restore
	e2eutil.MustNewBackupRestore(t, targetKube, targetKube.Namespace, restore)

	// wait for job and completion
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, restore.Name, time.Minute)
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, restore.Name, 2*time.Minute)

	// check that cluster holds the correct amount of items
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, numOfDocs, time.Minute)

	// cleanup
	e2eutil.MustDeleteBackup(t, targetKube, targetKube.Namespace, fullBackup)
	e2eutil.MustDeleteBackupRestore(t, targetKube, targetKube.Namespace, restore)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket Created
	// * Correct Cronjob(s) created
	// * Initial job created
	// * Remove Bucket
	// * Bucket Created
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
	fullFreq := 3

	// Create a normal cluster.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2

	numOfDocs := 200
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, targetKube.Namespace, mdsGroupSize)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 0, numOfDocs)
	time.Sleep(time.Minute) // wait for documents to be inserted
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, numOfDocs, time.Minute)

	// Create a Backup object.
	schedules := []string{createEveryXMinutesSchedule(fullFreq)}
	fullBackup := createTestBackup(v2.FullOnly, schedules)
	e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullBackup)

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
	e2eutil.MustWaitForJob(t, targetKube, testCouchbase, string(cluster.Full), time.Minute)
	e2eutil.MustWaitStatusUpdate(t, targetKube, testCouchbase, fullBackup.Name, "LastRun", time.Minute)
	e2eutil.MustWaitStatusUpdate(t, targetKube, testCouchbase, fullBackup.Name, "Running", time.Minute) // race condition issue?
	// wait for job completion
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, string(cluster.Full), time.Minute)

	// wait for backup status update
	archive := e2eutil.MustWaitStatusUpdate(t, targetKube, testCouchbase, fullBackup.Name, "Archive", time.Minute)
	if archive.String() != "/data/backups/" {
		t.FailNow()
	}
	e2eutil.MustWaitStatusUpdate(t, targetKube, testCouchbase, fullBackup.Name, "Repo", time.Minute)
	e2eutil.MustWaitStatusUpdate(t, targetKube, testCouchbase, fullBackup.Name, "RepoList", time.Minute)
	e2eutil.MustWaitStatusUpdate(t, targetKube, testCouchbase, fullBackup.Name, "LastSuccess", time.Minute)
	e2eutil.MustWaitStatusUpdate(t, targetKube, testCouchbase, fullBackup.Name, "Duration", time.Minute)

	// cleanup
	e2eutil.MustDeleteBackup(t, targetKube, targetKube.Namespace, fullBackup)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket Created
	// * Correct Cronjob(s) created
	// * Initial job created
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

	incrementalFreq := 1
	fullFreq := 2

	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2
	// Create a normal cluster.
	numOfDocs := 200
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, targetKube.Namespace, mdsGroupSize)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 0, numOfDocs)
	time.Sleep(time.Minute) // wait for documents to be inserted

	// Create Backup object 1.
	incrSchedules := []string{createEveryXMinutesSchedule(incrementalFreq), createEveryXMinutesSchedule(fullFreq)}
	fullIncrementalBackup := createTestBackup(v2.FullIncremental, incrSchedules)
	e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullIncrementalBackup)

	// Create Backup object 2.
	fullSchedules := []string{createEveryXMinutesSchedule(fullFreq)}
	fullBackup := createTestBackup(v2.FullOnly, fullSchedules)
	e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullBackup)

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

	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, "full-", 2*time.Minute)
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, "incremental-", 2*time.Minute)
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, "full-", 2*time.Minute)

	// cleanup
	e2eutil.MustDeleteBackup(t, targetKube, targetKube.Namespace, fullBackup)
	e2eutil.MustDeleteBackup(t, targetKube, targetKube.Namespace, fullIncrementalBackup)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket Created
	// * Correct Cronjob(s) created
	// * Initial job created
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

	incrementalFreq := 1
	fullFreq := 2

	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2
	clusterName := "test-couchbase-" + e2eutil.RandomSuffix()

	// Create the cluster.
	numOfDocs := 200

	// Create the cluster.
	tlsOptions := &e2eutil.TLSOpts{
		ClusterName: clusterName,
		AltNames: []string{
			"localhost",
			fmt.Sprintf("*.%s.%s.svc", clusterName, targetKube.Namespace),
			fmt.Sprintf("*.%s.%s", clusterName, domain),
		},
	}
	ctx, teardown := e2eutil.MustInitClusterTLS(t, targetKube, targetKube.Namespace, tlsOptions)
	defer teardown()
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, targetKube.Namespace, mdsGroupSize)

	testCouchbase.Name = clusterName
	testCouchbase.Spec.Networking.TLS = &v2.TLSPolicy{
		Static: &v2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, targetKube.Namespace, testCouchbase)

	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 0, numOfDocs)
	//time.Sleep(time.Minute) // wait for documents to be inserted

	// Create a Backup object.
	schedules := []string{createEveryXMinutesSchedule(incrementalFreq), createEveryXMinutesSchedule(fullFreq)}
	fullIncrementalBackup := createTestBackup(v2.FullIncremental, schedules)
	e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullIncrementalBackup)

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
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, "full-", 2*time.Minute)
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, "incremental-", 2*time.Minute)

	// cleanup
	e2eutil.MustDeleteBackup(t, targetKube, targetKube.Namespace, fullIncrementalBackup)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket Created
	// * Correct Cronjob(s) created
	// * Initial job created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Set{Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Full)},
			eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Incremental)},
		}},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestFullOnlyOverTLS(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	fullFreq := 2

	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2
	clusterName := "test-couchbase-" + e2eutil.RandomSuffix()

	// Create the cluster.
	numOfDocs := 200

	// Create the cluster.
	tlsOptions := &e2eutil.TLSOpts{
		ClusterName: clusterName,
		AltNames: []string{
			"localhost",
			fmt.Sprintf("*.%s.%s.svc", clusterName, targetKube.Namespace),
			fmt.Sprintf("*.%s.%s", clusterName, domain),
		},
	}
	ctx, teardown := e2eutil.MustInitClusterTLS(t, targetKube, targetKube.Namespace, tlsOptions)
	defer teardown()
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	testCouchbase := e2eutil.MustNewBackupCluster(t, targetKube, targetKube.Namespace, mdsGroupSize)

	testCouchbase.Name = clusterName
	testCouchbase.Spec.Networking.TLS = &v2.TLSPolicy{
		Static: &v2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, targetKube.Namespace, testCouchbase)

	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// insert docs to backup
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 0, numOfDocs)
	//time.Sleep(time.Minute) // wait for documents to be inserted

	// Create a Backup object.
	schedules := []string{createEveryXMinutesSchedule(fullFreq)}
	fullBackup := createTestBackup(v2.FullOnly, schedules)
	e2eutil.MustNewBackup(t, targetKube, targetKube.Namespace, fullBackup)

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
	e2eutil.MustWaitForJobCompletion(t, targetKube, testCouchbase, "full-", 2*time.Minute)

	// cleanup
	e2eutil.MustDeleteBackup(t, targetKube, targetKube.Namespace, fullBackup)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket Created
	// * Correct Cronjob(s) created
	// * Initial job created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Set{Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonBackupCreated, FuzzyMessage: string(cluster.Full)},
		}},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}
