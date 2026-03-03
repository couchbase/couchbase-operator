/*
Copyright 2020-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

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

func testPrometheusMetrics(t *testing.T, kubernetes *types.Cluster, tls *e2eutil.TLSContext, policy *couchbasev2.ClientCertificatePolicy, enabled bool) {
	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithMutualTLS(tls, policy).Generate(kubernetes)

	if enabled {
		cluster.Spec.Monitoring = &couchbasev2.CouchbaseClusterMonitoringSpec{
			Prometheus: &couchbasev2.CouchbaseClusterMonitoringPrometheusSpec{
				Enabled: true,
				Image:   framework.Global.CouchbaseExporterImage,
			},
		}
	}

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	bucket := e2eutil.MustGetBucket(t, framework.Global.BucketType, framework.Global.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	if !enabled {
		// Enable monitoring
		monitoring := enableMonitoring(framework.Global)

		cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/monitoring", monitoring), time.Minute)
		e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, corev1.ConditionTrue, cluster, 5*time.Minute)
		e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)
	}

	if tls != nil {
		// When the cluster is healthy, check the TLS is correctly configured.
		e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, tls, 5*time.Minute)
	}

	// Wait for Prometheus to be ready on each pod, then check that each pod is exporting the expected Couchbase metrics
	e2eutil.MustWaitForPrometheusReady(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckPrometheus(t, kubernetes, cluster, tls)

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
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// enable prometheus monitoring and create a cluster.
func TestPrometheusMetrics(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	testPrometheusMetrics(t, kubernetes, nil, nil, true)
}

func TestPrometheusMetricsTLS(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	testPrometheusMetrics(t, kubernetes, tls, nil, true)
}

func TestPrometheusMetricsMutualTLS(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyEnable

	testPrometheusMetrics(t, kubernetes, tls, &policy, true)
}

func TestPrometheusMetricsMandatoryMutualTLS(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyMandatory

	testPrometheusMetrics(t, kubernetes, tls, &policy, true)
}

// create a cluster and then enable prometheus monitoring after its creation.
func TestPrometheusMetricsEnable(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	testPrometheusMetrics(t, kubernetes, nil, nil, false)
}

func TestPrometheusMetricsEnableTLS(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	testPrometheusMetrics(t, kubernetes, tls, nil, false)
}

func TestPrometheusMetricsEnableMutualTLS(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyEnable

	testPrometheusMetrics(t, kubernetes, tls, &policy, false)
}

func TestPrometheusMetricsEnableMandatoryMutualTLS(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyMandatory

	testPrometheusMetrics(t, kubernetes, tls, &policy, false)
}

func TestPrometheusMetricsEnableShadowTLS(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	keyEncoding := e2eutil.KeyEncodingPKCS8
	tls := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{Source: e2eutil.TLSSourceCertManagerSecret, KeyEncoding: &keyEncoding})

	testPrometheusMetrics(t, kubernetes, tls, nil, false)
}

func TestPrometheusMetricsEnableMutualShadowTLS(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	keyEncoding := e2eutil.KeyEncodingPKCS8
	tls := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{Source: e2eutil.TLSSourceCertManagerSecret, KeyEncoding: &keyEncoding})

	policy := couchbasev2.ClientCertificatePolicyEnable

	testPrometheusMetrics(t, kubernetes, tls, &policy, false)
}

func TestPrometheusMetricsEnableMandatoryMutualShadowTLS(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	keyEncoding := e2eutil.KeyEncodingPKCS8
	tls := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{Source: e2eutil.TLSSourceCertManagerSecret, KeyEncoding: &keyEncoding})

	policy := couchbasev2.ClientCertificatePolicyMandatory

	testPrometheusMetrics(t, kubernetes, tls, &policy, false)
}

func TestPrometheusMetricsPerformOps(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3
	numOfDocs := f.DocsCount

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithMonitoring().MustCreate(t, kubernetes)

	// Wait for the cluster to be ready.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustWaitForPrometheusReady(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckPrometheus(t, kubernetes, cluster, nil)

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// Perform Bucket Ops: Inserting documents in the default Bucket
	e2eutil.NewDocumentSet(bucket.GetName(), f.DocsCount).MustCreate(t, kubernetes, cluster)

	// Perform ClusterOps: Failover a given node.
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, 1, true)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceCompletedEvent(cluster), 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Wait for Prometheus to be ready on each pod, then check that each pod is exporting the expected Couchbase metrics.
	e2eutil.MustWaitForPrometheusReady(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustCheckPrometheus(t, kubernetes, cluster, nil)

	bucketStat := `cbbucketstat_curr_items{bucket="default",cluster="` + cluster.Name + `"}`
	nodeStat := `cbnode_failover_complete{cluster="` + cluster.Name + `"}`

	e2eutil.MustExposeMetric(t, kubernetes, cluster, nil, bucketStat, strconv.Itoa(numOfDocs), time.Minute)
	e2eutil.MustExposeMetric(t, kubernetes, cluster, nil, nodeStat, `1`, time.Minute)

	// Check the events match what we expect:
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		e2eutil.PodDownFailoverRecoverySequence(),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestPrometheusMetricsBearerTokenAuth(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// MonitoringAuthSecret is the name used for all Monitoring Authorisation secret.
	monitoringAuthSecret := "cb-metrics-token"

	// Static configuration.
	clusterSize := 3

	// deleting monitoring secret.
	if err := kubernetes.KubeClient.CoreV1().Secrets(kubernetes.Namespace).Delete(context.Background(), monitoringAuthSecret, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
		e2eutil.Die(t, err)
	}

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// creating auth secret.
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: monitoringAuthSecret,
		},
		StringData: map[string]string{
			"token": "hello",
		},
	}

	if _, err := kubernetes.KubeClient.CoreV1().Secrets(kubernetes.Namespace).Create(context.Background(), secret, metav1.CreateOptions{}); err != nil {
		e2eutil.Die(t, err)
	}

	// Enable monitoring.
	monitoring := enableMonitoring(f)
	monitoring.Prometheus.AuthorizationSecret = &monitoringAuthSecret

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/monitoring", monitoring), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, corev1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Wait for Prometheus to be ready on each pod, then check that each pod is exporting the expected Couchbase metrics.
	var token []byte

	if authSecret, err := kubernetes.KubeClient.CoreV1().Secrets(kubernetes.Namespace).Get(context.Background(), monitoringAuthSecret, metav1.GetOptions{}); err != nil {
		e2eutil.Die(t, err)
	} else {
		token = authSecret.Data["token"]
	}

	e2eutil.MustWaitForPrometheusReady(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckPrometheusWithAuthSecret(t, kubernetes, cluster, nil, token)

	// Check the events match what we expect:
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestPrometheusMetricsUpgrade(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).ExporterUpgradable()

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	cluster := clusterOptionsUpgradeMonitoring().WithEphemeralTopology(clusterSize).WithMonitoring().MustCreate(t, kubernetes)

	// Wait for the cluster to be ready.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)
	e2eutil.MustWaitForPrometheusReady(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckPrometheus(t, kubernetes, cluster, nil)

	// Upgrade the cluster with new exporter image and check again if the monitoring is enabled.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/monitoring/prometheus/image", f.CouchbaseExporterImage), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, corev1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Wait for Prometheus to be ready on each pod, then check that each pod is exporting the expected Couchbase metrics.
	e2eutil.MustWaitForPrometheusReady(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckPrometheus(t, kubernetes, cluster, nil)

	// Check the events match what we expect:
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestPrometheusMetricsOperator checks that the operator, when operating is
// generating API level statistics.
func TestPrometheusMetricsOperator(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Confirm we can scrape various metrics from the endpoint
	e2eutil.MustCheckOperatorMetrics(t, kubernetes, cluster, nil)

	// Check the events match what we expect:
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestPrometheusRotateAdminPassword(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithMonitoring().MustCreate(t, kubernetes)

	// Wait for the cluster to be ready.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustWaitForPrometheusReady(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckPrometheus(t, kubernetes, cluster, nil)

	// Rotate the password.
	e2eutil.MustRotateClusterPassword(t, kubernetes)

	// Check Prometheus is still functioning.
	e2eutil.MustCheckPrometheus(t, kubernetes, cluster, nil)

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
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestPrometheusRotateCA(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// test scenario was broken pre exporter:1.0.5
	framework.Requires(t, kubernetes).AtLeastExporterVersion("1.0.5")

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithMonitoring().WithTLS(ctx).MustCreate(t, kubernetes)

	// Wait for the cluster to be ready.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustWaitForPrometheusReady(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckPrometheus(t, kubernetes, cluster, ctx)

	// Rotate the CA, and give Prometheus a moment to catch up.
	e2eutil.MustRotateServerCertificateAndCA(t, ctx)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)
	time.Sleep(10 * time.Second)

	// Check Prometheus is still functioning.
	e2eutil.MustCheckPrometheus(t, kubernetes, cluster, ctx)

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

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
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
