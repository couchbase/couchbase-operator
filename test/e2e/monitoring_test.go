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
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// enableMonitoring enables monitoring
func enableMonitoring(t *testing.T, f *framework.Framework) *couchbasev2.CouchbaseClusterMonitoringSpec {
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

func testPrometheusMetrics(t *testing.T, targetKube *types.Cluster, tls *e2eutil.TLSContext, policy *couchbasev2.ClientCertificatePolicy, enabled bool) {
	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	testCouchbase := e2eutil.MustNewPrometheusTLSClusterBasic(t, targetKube, targetKube.Namespace, clusterSize, tls, policy, enabled)

	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	if !enabled {
		// Enable monitoring
		monitoring := enableMonitoring(t, framework.Global)

		testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Add("/Spec/Monitoring", monitoring), time.Minute)
		e2eutil.MustWaitForClusterCondition(t, targetKube, couchbasev2.ClusterConditionUpgrading, corev1.ConditionTrue, testCouchbase, 5*time.Minute)
		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 20*time.Minute)

	}

	if tls != nil {
		// When the cluster is healthy, check the TLS is correctly configured.
		e2eutil.MustCheckClusterTLS(t, targetKube, targetKube.Namespace, tls)
	}

	// Wait for Prometheus to be ready on each pod, then check that each pod is exporting the expected Couchbase metrics
	e2eutil.MustWaitForPrometheusReady(t, targetKube, testCouchbase, 2*time.Minute)
	e2eutil.MustCheckPrometheus(t, targetKube, testCouchbase, tls)

	// Check the events match what we expect:
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Optional{
			Validator: eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Optional{
			Validator: eventschema.Sequence{
				Validators: []eventschema.Validatable{
					eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
					eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
					eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
				},
			},
		},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// enable prometheus monitoring and create a cluster
func TestPrometheusMetrics(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	testPrometheusMetrics(t, targetKube, nil, nil, true)
}

func TestPrometheusMetricsTLS(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	tls, teardown := e2eutil.MustInitClusterTLS(t, targetKube, targetKube.Namespace, &e2eutil.TLSOpts{})
	defer teardown()

	testPrometheusMetrics(t, targetKube, tls, nil, true)
}

func TestPrometheusMetricsMutualTLS(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	tls, teardown := e2eutil.MustInitClusterTLS(t, targetKube, targetKube.Namespace, &e2eutil.TLSOpts{})
	defer teardown()

	policy := couchbasev2.ClientCertificatePolicyEnable

	testPrometheusMetrics(t, targetKube, tls, &policy, true)
}

func TestPrometheusMetricsMandatoryMutualTLS(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	tls, teardown := e2eutil.MustInitClusterTLS(t, targetKube, targetKube.Namespace, &e2eutil.TLSOpts{})
	defer teardown()

	policy := couchbasev2.ClientCertificatePolicyMandatory

	testPrometheusMetrics(t, targetKube, tls, &policy, true)
}

// create a cluster and then enable prometheus monitoring after its creation
func TestPrometheusMetricsEnable(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	testPrometheusMetrics(t, targetKube, nil, nil, false)
}

func TestPrometheusMetricsEnableTLS(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	tls, teardown := e2eutil.MustInitClusterTLS(t, targetKube, targetKube.Namespace, &e2eutil.TLSOpts{})
	defer teardown()

	testPrometheusMetrics(t, targetKube, tls, nil, false)
}

func TestPrometheusMetricsEnableMutualTLS(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	tls, teardown := e2eutil.MustInitClusterTLS(t, targetKube, targetKube.Namespace, &e2eutil.TLSOpts{})
	defer teardown()

	policy := couchbasev2.ClientCertificatePolicyEnable

	testPrometheusMetrics(t, targetKube, tls, &policy, false)
}

func TestPrometheusMetricsEnableMandatoryMutualTLS(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	tls, teardown := e2eutil.MustInitClusterTLS(t, targetKube, targetKube.Namespace, &e2eutil.TLSOpts{})
	defer teardown()

	policy := couchbasev2.ClientCertificatePolicyMandatory

	testPrometheusMetrics(t, targetKube, tls, &policy, false)
}

func TestPrometheusMetricsEnableAndPerformOps(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 3
	numOfDocs := 200

	// Create the cluster.
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Name = "test-couchbase-" + e2eutil.RandomSuffix()
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, targetKube.Namespace, testCouchbase)

	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// Enable monitoring
	monitoring := enableMonitoring(t, f)

	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Add("/Spec/Monitoring", monitoring), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, targetKube, couchbasev2.ClusterConditionUpgrading, corev1.ConditionTrue, testCouchbase, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 20*time.Minute)

	// Perform Bucket Ops: Inserting 200 documents in the default Bucket
	e2eutil.MustPopulateBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, numOfDocs)

	// Perform ClusterOps: Failover a given node
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, 3, true)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Wait for Prometheus to be ready on each pod, then check that each pod is exporting the expected Couchbase metrics
	e2eutil.MustWaitForPrometheusReady(t, targetKube, testCouchbase, 2*time.Minute)
	e2eutil.MustCheckPrometheus(t, targetKube, testCouchbase, nil)

	bucketStat := `cbbucketstat_curr_items{bucket="default",cluster="` + testCouchbase.Name + `"}`
	nodeStat := `cbnode_failover_complete{cluster="` + testCouchbase.Name + `"}`

	e2eutil.MustExposeMetric(t, targetKube, testCouchbase, nil, bucketStat, `200`, time.Minute)
	e2eutil.MustExposeMetric(t, targetKube, testCouchbase, nil, nodeStat, `1`, time.Minute)

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
	if err := targetKube.KubeClient.CoreV1().Secrets(targetKube.Namespace).Delete(monitoringAuthSecret, nil); err != nil && errors.IsNotFound(err) {
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
	monitoring := enableMonitoring(t, f)
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
	e2eutil.MustCheckPrometheusWithAuthSecret(t, targetKube, testCouchbase, nil, token)

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
