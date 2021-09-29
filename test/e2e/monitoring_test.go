package e2e

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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

func testPrometheusMetrics(t *testing.T, targetKube *types.Cluster, tls *e2eutil.TLSContext, policy *couchbasev2.ClientCertificatePolicy, enabled bool) {
	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithMutualTLS(tls, policy).Generate(targetKube)

	if enabled {
		testCouchbase.Spec.Monitoring = &couchbasev2.CouchbaseClusterMonitoringSpec{
			Prometheus: &couchbasev2.CouchbaseClusterMonitoringPrometheusSpec{
				Enabled: true,
				Image:   framework.Global.CouchbaseExporterImage,
			},
		}
	}

	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	bucket := e2eutil.MustGetBucket(t, framework.Global.BucketType, framework.Global.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, time.Minute)

	if !enabled {
		// Enable monitoring
		monitoring := enableMonitoring(framework.Global)

		testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Add("/spec/monitoring", monitoring), time.Minute)
		e2eutil.MustWaitForClusterCondition(t, targetKube, couchbasev2.ClusterConditionUpgrading, corev1.ConditionTrue, testCouchbase, 5*time.Minute)
		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 20*time.Minute)
	}

	if tls != nil {
		// When the cluster is healthy, check the TLS is correctly configured.
		e2eutil.MustCheckClusterTLS(t, targetKube, testCouchbase, tls, 5*time.Minute)
	}

	// Wait for Prometheus to be ready on each pod, then check that each pod is exporting the expected Couchbase metrics
	e2eutil.MustWaitForPrometheusReady(t, targetKube, testCouchbase, 5*time.Minute)
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

// enable prometheus monitoring and create a cluster.
func TestPrometheusMetrics(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	testPrometheusMetrics(t, targetKube, nil, nil, true)
}

func TestPrometheusMetricsTLS(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	testPrometheusMetrics(t, targetKube, tls, nil, true)
}

func TestPrometheusMetricsMutualTLS(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyEnable

	testPrometheusMetrics(t, targetKube, tls, &policy, true)
}

func TestPrometheusMetricsMandatoryMutualTLS(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyMandatory

	testPrometheusMetrics(t, targetKube, tls, &policy, true)
}

// create a cluster and then enable prometheus monitoring after its creation.
func TestPrometheusMetricsEnable(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	testPrometheusMetrics(t, targetKube, nil, nil, false)
}

func TestPrometheusMetricsEnableTLS(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	testPrometheusMetrics(t, targetKube, tls, nil, false)
}

func TestPrometheusMetricsEnableMutualTLS(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyEnable

	testPrometheusMetrics(t, targetKube, tls, &policy, false)
}

func TestPrometheusMetricsEnableMandatoryMutualTLS(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyMandatory

	testPrometheusMetrics(t, targetKube, tls, &policy, false)
}

func TestPrometheusMetricsPerformOps(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3
	numOfDocs := f.DocsCount

	// Create the cluster.
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithMonitoring().MustCreate(t, targetKube)

	// Wait for the cluster to be ready.
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustWaitForPrometheusReady(t, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustCheckPrometheus(t, targetKube, testCouchbase, nil)

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, time.Minute)

	// Perform Bucket Ops: Inserting documents in the default Bucket
	e2eutil.NewDocumentSet(bucket.GetName(), f.DocsCount).MustCreate(t, targetKube, testCouchbase)

	// Perform ClusterOps: Failover a given node.
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, 1, true)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Wait for Prometheus to be ready on each pod, then check that each pod is exporting the expected Couchbase metrics.
	e2eutil.MustWaitForPrometheusReady(t, targetKube, testCouchbase, 2*time.Minute)
	e2eutil.MustCheckPrometheus(t, targetKube, testCouchbase, nil)

	bucketStat := `cbbucketstat_curr_items{bucket="default",cluster="` + testCouchbase.Name + `"}`
	nodeStat := `cbnode_failover_complete{cluster="` + testCouchbase.Name + `"}`

	e2eutil.MustExposeMetric(t, targetKube, testCouchbase, nil, bucketStat, strconv.Itoa(numOfDocs), time.Minute)
	e2eutil.MustExposeMetric(t, targetKube, testCouchbase, nil, nodeStat, `1`, time.Minute)

	// Check the events match what we expect:
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		e2eutil.PodDownFailoverRecoverySequence(),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestPrometheusMetricsBearerTokenAuth(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// MonitoringAuthSecret is the name used for all Monitoring Authorisation secret.
	monitoringAuthSecret := "cb-metrics-token"

	// Static configuration.
	clusterSize := 3

	// deleting monitoring secret.
	if err := targetKube.KubeClient.CoreV1().Secrets(targetKube.Namespace).Delete(context.Background(), monitoringAuthSecret, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
		e2eutil.Die(t, err)
	}

	// Create the cluster.
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, targetKube)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, targetKube, bucket)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, time.Minute)

	// creating auth secret.
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: monitoringAuthSecret,
		},
		StringData: map[string]string{
			"token": "hello",
		},
	}

	if _, err := targetKube.KubeClient.CoreV1().Secrets(targetKube.Namespace).Create(context.Background(), secret, metav1.CreateOptions{}); err != nil {
		e2eutil.Die(t, err)
	}

	// Enable monitoring.
	monitoring := enableMonitoring(f)
	monitoring.Prometheus.AuthorizationSecret = &monitoringAuthSecret

	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Add("/spec/monitoring", monitoring), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, targetKube, couchbasev2.ClusterConditionUpgrading, corev1.ConditionTrue, testCouchbase, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 20*time.Minute)

	// Wait for Prometheus to be ready on each pod, then check that each pod is exporting the expected Couchbase metrics.
	var token []byte

	if authSecret, err := targetKube.KubeClient.CoreV1().Secrets(targetKube.Namespace).Get(context.Background(), monitoringAuthSecret, metav1.GetOptions{}); err != nil {
		e2eutil.Die(t, err)
	} else {
		token = authSecret.Data["token"]
	}

	e2eutil.MustWaitForPrometheusReady(t, targetKube, testCouchbase, 5*time.Minute)
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

func TestPrometheusMetricsUpgrade(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, targetKube).ExporterUpgradable()

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	testCouchbase := clusterOptionsUpgradeMonitoring().WithEphemeralTopology(clusterSize).WithMonitoring().MustCreate(t, targetKube)

	// Wait for the cluster to be ready.
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 20*time.Minute)
	e2eutil.MustWaitForPrometheusReady(t, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustCheckPrometheus(t, targetKube, testCouchbase, nil)

	// Upgrade the cluster with new exporter image and check again if the monitoring is enabled.
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/monitoring/prometheus/image", f.CouchbaseExporterImage), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, targetKube, couchbasev2.ClusterConditionUpgrading, corev1.ConditionTrue, testCouchbase, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 20*time.Minute)

	// Wait for Prometheus to be ready on each pod, then check that each pod is exporting the expected Couchbase metrics.
	e2eutil.MustWaitForPrometheusReady(t, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustCheckPrometheus(t, targetKube, testCouchbase, nil)

	// Check the events match what we expect:
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestPrometheusMetricsOperator checks that the operator, when operating is
// generating API level statistics.
func TestPrometheusMetricsOperator(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, targetKube)

	// Confirm we can scrape various metrics from the endpoint
	e2eutil.MustCheckOperatorMetrics(t, targetKube, testCouchbase, nil)

	// Check the events match what we expect:
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestPrometheusRotateAdminPassword(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithMonitoring().MustCreate(t, targetKube)

	// Wait for the cluster to be ready.
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustWaitForPrometheusReady(t, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustCheckPrometheus(t, targetKube, testCouchbase, nil)

	// Rotate the password.
	e2eutil.MustRotateClusterPassword(t, targetKube)

	// Check Prometheus is still functioning.
	e2eutil.MustCheckPrometheus(t, targetKube, testCouchbase, nil)

	// Check the events match what we expect:
	// * Cluster created
	// * Password rotated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Optional{
			Validator: eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		},
		eventschema.Event{Reason: k8sutil.EventReasonAdminPasswordChanged},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestPrometheusRotateCA(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithMonitoring().WithTLS(ctx).MustCreate(t, targetKube)

	// Wait for the cluster to be ready.
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustWaitForPrometheusReady(t, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustCheckPrometheus(t, targetKube, testCouchbase, ctx)

	// Rotate the CA, and give Prometheus a moment to catch up.
	e2eutil.MustRotateServerCertificateAndCA(t, ctx)
	e2eutil.MustCheckClusterTLS(t, targetKube, testCouchbase, ctx, 5*time.Minute)
	time.Sleep(10 * time.Second)

	// Check Prometheus is still functioning.
	e2eutil.MustCheckPrometheus(t, targetKube, testCouchbase, ctx)

	// Check the events match what we expect:
	// * Cluster created
	// * TLS update event occurred
	// * Client TLS updated (new CA)
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		// Race condition updating secrets.
		eventschema.Optional{
			Validator: eventschema.Event{Reason: k8sutil.EventReasonTLSInvalid},
		},
		eventschema.Repeat{
			Times:     clusterSize,
			Validator: eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
		},
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated, Message: string(k8sutil.ClientTLSUpdateReasonUpdateCA)},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestCouchbaseMetricsDocumentCount checks that we can query Server 7.0's prometheus endpoints for document counts.
func TestCouchbaseMetricsDocumentCount(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0")

	// Static configuration.
	clusterSize := 1
	scopeName := "pinky"
	collectionName := "brain"

	// Create scope & collection in a bucket.
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)
	scope := e2eutil.NewScope(scopeName).WithCollections(collection).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Check scope & collection have been created.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// Create documents; one set in the default collection, and another in the created one.
	e2eutil.NewDocumentSet(bucket.GetName(), f.DocsCount).MustCreate(t, kubernetes, cluster)
	e2eutil.NewDocumentSet(bucket.GetName(), f.DocsCount).IntoScopeAndCollection(scopeName, collectionName).MustCreate(t, kubernetes, cluster)

	// Check document count - the former should get the count from both the default and custom collections using the /pools/default/buckets,
	// while the latter should just get the count from the created collection using the prometheus endpoints.
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), 2*f.DocsCount, time.Minute)
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, cluster, bucket.GetName(), scopeName, collectionName, f.DocsCount, time.Minute)
}
