package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	v2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	clusterSize int    = 1
	pvcName     string = "couchbase"
)

func enableAuditing(withGC bool) *v2.CouchbaseClusterAuditLoggingSpec {
	spec := v2.CouchbaseClusterAuditLoggingSpec{
		Enabled:        true,
		DisabledEvents: []int{},
		DisabledUsers:  []v2.AuditDisabledUser{},
		Rotation: &v2.CouchbaseClusterLogRotationSpec{
			Size: k8sutil.NewResourceQuantityMi(1),
		},
	}

	if withGC {
		spec.GarbageCollection = &v2.CouchbaseClusterAuditGarbageCollectionSpec{
			Sidecar: &v2.CouchbaseClusterAuditCleanupSidecarSpec{
				Enabled:  true,
				Interval: k8sutil.NewDurationS(30),
			},
		}
	}

	return &spec
}

func enableLogging(f *framework.Framework) *v2.CouchbaseClusterLoggingConfigurationSpec {
	spec := v2.CouchbaseClusterLoggingConfigurationSpec{
		Enabled:           true,
		ConfigurationName: "fluent-bit-config",
		Sidecar: &v2.LogShipperSidecarSpec{
			Image: "fluent/fluent-bit:1.7",
		},
	}

	if imageName := strings.TrimSpace(f.CouchbaseLoggingImage); imageName != "" {
		spec.Sidecar.Image = imageName
	}

	return &spec
}

func createValidCustomLogConfig(t *testing.T, targetKube *types.Cluster, testCouchbase *v2.CouchbaseCluster) {
	// Make sure we clear out any existing config maps just in case to ensure a clean run
	if err := targetKube.KubeClient.CoreV1().Secrets(targetKube.Namespace).Delete(context.Background(), testCouchbase.Spec.Logging.Server.ConfigurationName, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
		e2eutil.Die(t, err)
	}

	if testCouchbase.Spec.Logging.Server.ManageConfiguration == nil || (*testCouchbase.Spec.Logging.Server.ManageConfiguration) {
		err := fmt.Errorf("incorrectly specified cluster - need to disable management of configuration")
		e2eutil.Die(t, err)
	}

	// Set up a valid custom default
	defaultConfig := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: testCouchbase.Spec.Logging.Server.ConfigurationName,
		},
		StringData: map[string]string{
			// The formatting of the file is very important so using space & newlines to control rather than multi-line strings
			// Do not use tabs
			// Configure fluent bit to also include the pod name and some helper keys
			"fluent-bit.conf": "[SERVICE]\n" +
				"    flush        	1\n" +
				"    daemon       	Off\n" +
				"    log_level    	warn\n" +
				"    parsers_file 	parsers.conf\n" +
				"# This is required to simplify downstream parsing to filter the different pod logs\n" +
				"[FILTER]\n" +
				"    Name           modify\n" +
				"    Match          *\n" +
				"    Add            pod        ${HOSTNAME}\n" +
				"    Add            logshipper fluentbit-sidecar\n" +
				"@include output.conf\n" +
				"@include input.conf\n",

			// CUSTOMISATION
			"input.conf": "[INPUT]\n" +
				"    Name           tail\n" +
				"    Path           ${COUCHBASE_LOGS}/audit.log\n" +
				"    Parser         auditdb_log\n" +
				"    Path_Key       filename\n" +
				"    Tag            couchbase.log.audit\n\n" +

				"[INPUT]\n" +
				"    Name tail\n" +
				"    Path ${COUCHBASE_LOGS}/indexer.log\n" +
				"    Parser simple_log\n" +
				"    Path_Key filename\n" +
				"    Tag couchbase.log.lookforme\n\n" +

				"[INPUT]\n" +
				"    Name tail\n" +
				"    Path ${COUCHBASE_LOGS}/memcached.log.000000.txt\n" +
				"    Parser simple_log\n" +
				"    Path_Key filename\n" +
				"    Tag couchbase.log.memcached\n\n" +

				"[INPUT]\n" +
				"    Name tail\n" +
				"    Path ${COUCHBASE_LOGS}/babysitter.log,${COUCHBASE_LOGS}/couchdb.log\n" +
				"    Multiline On\n" +
				"    Parser_Firstline erlang_multiline\n" +
				"    Path_Key filename\n" +
				"    Skip_Long_Lines On\n" +
				"    Tag couchbase.log.<logname>\n" +
				"    Tag_Regex ${COUCHBASE_LOGS}/(?<logname>[^.]+).log$\n\n",

			// Default to only sending to stdout
			"output.conf": "[OUTPUT]\n" +
				"    name           stdout\n" +
				"    match          couchbase.log.*\n",

			// Parsers provided for audit log, a general erlang one that copes with multiline messages and
			// the simple_log copes with indexer.log or similar TIME LEVEL MESSAGE log formats. In each case
			// we only 'parse' the time, level and message fields - no further decoding is done.
			"parsers.conf": "[PARSER]\n" +
				"    Name           auditdb_log\n" +
				"    Format       	json\n" +
				"    Time_Key     	timestamp\n" +
				"    Time_Format  	%Y-%m-%dT%H:%M:%S.%L\n\n" +

				"[PARSER]\n" +
				"    Name           simple_log\n" +
				"    Format         regex\n" +
				"    Regex          ^(?<time>\\d+-\\d+-\\d+T\\d+:\\d+:\\d+.\\d+(\\+|-)\\d+:\\d+)\\s+\\[(?<level>\\w+)\\](?<message>.*)$\n" +
				"    Time_Key       time\n" +
				"    Time_Format    %Y-%m-%dT%H:%M:%S.%L%z\n\n" +

				"[PARSER]\n" +
				"    Name           erlang_multiline\n" +
				"    Format         regex\n" +
				"    Regex          ^\\[(?<logger>\\w+):(?<level>\\w+),(?<time>\\d+-\\d+-\\d+T\\d+:\\d+:\\d+.\\d+Z).*](?<message>.*)$\n" +
				"    Time_Key       time\n" +
				"    Time_Format    %Y-%m-%dT%H:%M:%S.%L\n",
		},
	}

	if _, err := targetKube.KubeClient.CoreV1().Secrets(targetKube.Namespace).Create(context.Background(), defaultConfig, metav1.CreateOptions{}); err != nil {
		e2eutil.Die(t, err)
	}
}

func createInvalidLogConfig(t *testing.T, targetKube *types.Cluster, testCouchbase *v2.CouchbaseCluster) {
	// Make sure we clear out any existing config maps just in case to ensure a clean run
	if err := targetKube.KubeClient.CoreV1().Secrets(targetKube.Namespace).Delete(context.Background(), testCouchbase.Spec.Logging.Server.ConfigurationName, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
		e2eutil.Die(t, err)
	}
	// Set up a invalid custom default - right name, just nothing in it but indicate it is operator managed
	defaultConfig := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: testCouchbase.Spec.Logging.Server.ConfigurationName,
		},
	}
	if _, err := targetKube.KubeClient.CoreV1().Secrets(targetKube.Namespace).Create(context.Background(), defaultConfig, metav1.CreateOptions{}); err != nil {
		e2eutil.Die(t, err)
	}
}

// Audit configuration reconciled (CRD = disabled, manually enabled and then reset back to disabled).
func TestNoLogOrAuditConfig(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Check no audit enabled
	e2eutil.MustCheckAuditConfiguration(t, targetKube, testCouchbase)
	// Check no extra sidecars
	e2eutil.MustCheckLoggingSidecarsCount(t, targetKube, testCouchbase, 0)

	// Check we can manually enable
	e2eutil.MustEnableAuditLogging(t, targetKube, testCouchbase)

	// Prior to applying the new config, we start watching for the event that indicates it - it may happen too quickly otherwise to observe
	op := e2eutil.WaitForPendingClusterEvent(targetKube, testCouchbase, k8sutil.ClusterSettingsEditedEvent("audit", testCouchbase), 2*time.Minute)
	// Apply the new configuration
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/logging/audit", &v2.CouchbaseClusterAuditLoggingSpec{Enabled: false}), time.Minute)
	// Check we have seen the event
	e2eutil.MustReceiveErrorValue(t, op)
	// Check the audit configuration has been applied correctly now
	e2eutil.MustCheckAuditConfiguration(t, targetKube, testCouchbase)

	// Check that the user can see the cluster being edited.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(1),
		eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Audit log appears on standard output when enabled.
// Audit logs are removed automatically by the GC - confirm with short rotation interval and check output of the sidecar includes log names.
func TestLoggingAndAuditingDefaults(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	testCouchbase := clusterOptions().WithPersisitentTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.Servers[0].VolumeMounts = &v2.VolumeMounts{
		DefaultClaim: pvcName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []v2.PersistentVolumeClaimTemplate{
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 1),
	}
	testCouchbase.Spec.Logging.Audit = enableAuditing(true)
	testCouchbase.Spec.Logging.Server = enableLogging(f)

	// Create a dodgy log config to prove it is reconciled correctly later
	createInvalidLogConfig(t, targetKube, testCouchbase)
	e2eutil.MustFailLoggingConfig(t, targetKube, testCouchbase)

	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Confirm that config is now correct
	e2eutil.MustCheckLoggingConfig(t, targetKube, testCouchbase)
	e2eutil.MustCheckAuditConfiguration(t, targetKube, testCouchbase)

	// Wait for logging to be ready on each pod, then check that each pod is exporting some logs
	e2eutil.MustWaitForLoggingSidecarReady(t, targetKube, testCouchbase, 5*time.Minute)
	// Note that the sidecars start fast but have to wait for logs to appear from the slower server container
	e2eutil.MustCheckLogging(t, targetKube, testCouchbase, 5*time.Minute)

	// Check that the user can see the cluster settings being edited but only once.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(1),
		eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// If no GC enabled then sidecar is not running.
// Can enable auditing without logging.
// Audit configuration passed through to REST API - verify with some custom updates to the CRD and check the audit REST API response.
func TestAuditingNoLogging(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.Servers[0].VolumeMounts = &v2.VolumeMounts{
		DefaultClaim: pvcName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []v2.PersistentVolumeClaimTemplate{
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 1),
	}

	// GC is not supported without logging - tested by validation - so disable here.
	testCouchbase.Spec.Logging.Audit = enableAuditing(false)
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	e2eutil.MustCheckAuditConfiguration(t, targetKube, testCouchbase)
	// Check no extra sidecars
	e2eutil.MustCheckLoggingSidecarsCount(t, targetKube, testCouchbase, 0)

	// Patch in some deltas to configuration and check it is in place now
	// The REST API for audit is particularly difficult/non-standard so testing each of the fields as a sanity check
	newConfig := enableAuditing(false)
	// Single element arrays seem troublesome for Erlang to parse
	newConfig.DisabledEvents = []int{8243}
	newConfig.DisabledUsers = []v2.AuditDisabledUser{"@eventing/local", "@cbq-engine/local"}

	// Prior to applying the new config, we start watching for the event that indicates it - it may happen too quickly otherwise to observe
	op := e2eutil.WaitForPendingClusterEvent(targetKube, testCouchbase, k8sutil.ClusterSettingsEditedEvent("audit", testCouchbase), 2*time.Minute)
	// Apply the new configuration
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/logging/audit", newConfig), time.Minute)
	// Check we have seen the event
	e2eutil.MustReceiveErrorValue(t, op)
	// Check the audit configuration has been applied correctly now
	e2eutil.MustCheckAuditConfiguration(t, targetKube, testCouchbase)

	// Check that the user can see the cluster settings being edited twice.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(1),
		eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Additional logs can be streamed to standard output with a custom log configuration. Custom configuration is ignored for reconcile.
func TestCustomLogging(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	testCouchbase := clusterOptions().WithPersisitentTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.Servers[0].VolumeMounts = &v2.VolumeMounts{
		DefaultClaim: pvcName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []v2.PersistentVolumeClaimTemplate{
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 1),
	}

	// Do not enable auditing so no logging will be enabled unless we have a custom configuration
	testCouchbase.Spec.Logging.Server = &v2.CouchbaseClusterLoggingConfigurationSpec{
		Enabled:             true,
		ManageConfiguration: &[]bool{false}[0],          // Optional *bool so fun to set false
		ConfigurationName:   "custom-fluent-bit-config", // Use custom name for config map too
	}

	createValidCustomLogConfig(t, targetKube, testCouchbase)
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Confirm that config is correct
	e2eutil.MustCheckLoggingConfig(t, targetKube, testCouchbase)
	e2eutil.MustCheckAuditConfiguration(t, targetKube, testCouchbase)

	// Wait for logging to be ready on each pod, then check that each pod is exporting some logs
	e2eutil.MustWaitForLoggingSidecarReady(t, targetKube, testCouchbase, 5*time.Minute)
	// General check for some logs
	e2eutil.MustCheckLogging(t, targetKube, testCouchbase, 5*time.Minute)
	// Specific check for logs we have specified in the custom config that are not in the default one
	e2eutil.MustCheckLogsForString(t, targetKube, testCouchbase, 5*time.Minute, "couchbase.log.lookforme")

	// Ensure no audit cleanup
	e2eutil.MustCheckLoggingSidecarsCount(t, targetKube, testCouchbase, 1)

	// Ensure we can mess up the custom config and it is not reconciled and pods are not restarted
	createInvalidLogConfig(t, targetKube, testCouchbase)
	e2eutil.MustFailLoggingConfig(t, targetKube, testCouchbase)

	// Check that the user can not see the cluster settings being edited as they're custom.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(1),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestChangeLogShipperImage(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	testCouchbase := clusterOptions().WithPersisitentTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.Servers[0].VolumeMounts = &v2.VolumeMounts{
		DefaultClaim: pvcName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []v2.PersistentVolumeClaimTemplate{
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 1),
	}
	testCouchbase.Spec.Logging.Server = enableLogging(f)
	testCouchbase.Spec.Logging.Server.Sidecar.Image = "fluent/fluent-bit:1.6.10"

	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Confirm that config is now correct
	e2eutil.MustCheckLoggingConfig(t, targetKube, testCouchbase)

	// Wait for logging to be ready on each pod, then check that each pod is exporting some logs
	e2eutil.MustWaitForLoggingSidecarReady(t, targetKube, testCouchbase, 5*time.Minute)

	// Check for the specific version of Fluent-bit in the logs
	e2eutil.MustCheckLogsForString(t, targetKube, testCouchbase, time.Minute, "Fluent Bit v1.6.10")
}
