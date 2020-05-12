package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	e2e_constants "github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// enableMonitoring enables monitoring.
func enableMonitoring(f *framework.Framework) *couchbasev2.CouchbaseClusterMonitoringSpec {
	if imageName := strings.TrimSpace(f.CouchbaseExporterImage); imageName != "" {
		monitoring := &couchbasev2.CouchbaseClusterMonitoringSpec{
			Prometheus: &couchbasev2.CouchbaseClusterMonitoringPrometheusSpec{
				Enabled: true,
				Image:   imageName,
			},
		}
		return monitoring
	}

	monitoring := &couchbasev2.CouchbaseClusterMonitoringSpec{
		Prometheus: &couchbasev2.CouchbaseClusterMonitoringPrometheusSpec{
			Enabled: true,
		},
	}
	return monitoring
}

func TestPrometheusMetrics(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.GenerateName = e2e_constants.ClusterNamePrefix
	testCouchbase.Spec.Monitoring = &couchbasev2.CouchbaseClusterMonitoringSpec{
		Prometheus: &couchbasev2.CouchbaseClusterMonitoringPrometheusSpec{
			Enabled: true,
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, targetKube.Namespace, testCouchbase)

	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// Wait for Prometheus to be ready on each pod, then check that each pod is exporting the expected Couchbase metrics
	e2eutil.MustWaitForPrometheusReady(t, targetKube, testCouchbase, 2*time.Minute)
	e2eutil.MustCheckPrometheus(t, targetKube, testCouchbase)

	// Check the events match what we expect:
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// create a cluster and then enable prometheus monitoring after its creation.
func TestPrometheusMetricsEnable(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, targetKube.Namespace, clusterSize)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// Enable monitoring
	monitoring := enableMonitoring(f)

	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Add("/Spec/Monitoring", monitoring), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, targetKube, couchbasev2.ClusterConditionUpgrading, corev1.ConditionTrue, testCouchbase, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 20*time.Minute)

	// Wait for Prometheus to be ready on each pod, then check that each pod is exporting the expected Couchbase metrics
	e2eutil.MustWaitForPrometheusReady(t, targetKube, testCouchbase, 2*time.Minute)
	e2eutil.MustCheckPrometheus(t, targetKube, testCouchbase)

	// Check the events match what we expect:
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestPrometheusMetricsEnableAndPerformOps(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, targetKube.Namespace, clusterSize)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// Enable monitoring
	monitoring := enableMonitoring(f)

	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Add("/Spec/Monitoring", monitoring), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, targetKube, couchbasev2.ClusterConditionUpgrading, corev1.ConditionTrue, testCouchbase, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 20*time.Minute)

	// Perform Bucket Ops: Inserting 200 documents in the default Bucket
	e2eutil.MustPopulateBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 200)

	// Perform ClusterOps: Failover a given node
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, 3, true)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Wait for Prometheus to be ready on each pod, then check that each pod is exporting the expected Couchbase metrics
	e2eutil.MustWaitForPrometheusReady(t, targetKube, testCouchbase, 2*time.Minute)
	e2eutil.MustCheckPrometheus(t, targetKube, testCouchbase)

	e2eutil.MustExposeMetric(t, targetKube, testCouchbase, `cbbucketstat_curr_items{bucket="default"}`, `200`, time.Minute)
	e2eutil.MustExposeMetric(t, targetKube, testCouchbase, `cbnode_failover_complete`, `1`, time.Minute)

	// Check the events match what we expect:
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
		e2eutil.PodDownFailoverRecoverySequence(),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestPrometheusMetricsBearerTokenAuth(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)
	// MonitoringAuthSecret is the name used for all Monitoring Authorisation secret.
	var monitoringAuthSecret string = "cb-metrics-token"

	// Static configuration.
	clusterSize := 3

	// deleting monitoring secret
	if err := targetKube.KubeClient.CoreV1().Secrets(targetKube.Namespace).Delete(monitoringAuthSecret, nil); err != nil {
		fmt.Println("Warning: Unable to delete monitoring secret: ", err)
	}

	// Create the cluster.
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, targetKube.Namespace, clusterSize)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// creating auth secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: monitoringAuthSecret,
		},
		StringData: map[string]string{
			"token": "hello",
		},
	}

	if _, err := targetKube.KubeClient.CoreV1().Secrets(targetKube.Namespace).Create(secret); err != nil {
		e2eutil.Die(t, err)
	}

	// Enable monitoring
	monitoring := enableMonitoring(f)
	monitoring.Prometheus.AuthorizationSecret = &monitoringAuthSecret

	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Add("/Spec/Monitoring", monitoring), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, targetKube, couchbasev2.ClusterConditionUpgrading, corev1.ConditionTrue, testCouchbase, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 20*time.Minute)

	// Wait for Prometheus to be ready on each pod, then check that each pod is exporting the expected Couchbase metrics
	var token []byte
	if authSecret, err := targetKube.KubeClient.CoreV1().Secrets(targetKube.Namespace).Get(monitoringAuthSecret, metav1.GetOptions{}); err != nil {
		e2eutil.Die(t, err)
	} else {
		token = authSecret.Data["token"]
	}

	e2eutil.MustWaitForPrometheusReady(t, targetKube, testCouchbase, 2*time.Minute)
	e2eutil.MustCheckPrometheusWithAuthSecret(t, targetKube, testCouchbase, token)

	// Check the events match what we expect:
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}
