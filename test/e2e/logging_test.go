package e2e

import (
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
	corev1 "k8s.io/api/core/v1"
)

const (
	clusterSize int = 1
)

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

	// Check we can manually enable, i.e. existing behaviour
	e2eutil.MustEnableAuditLogging(t, targetKube, testCouchbase)

	// Apply the new configuration and set explicitly false
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/logging/audit", &v2.CouchbaseClusterAuditLoggingSpec{Enabled: false}), time.Minute)

	// We only have one of these events so no need to mess with race conditions & async waits
	e2eutil.MustObserveClusterEvent(t, targetKube, testCouchbase, k8sutil.ClusterSettingsEditedEvent("audit", testCouchbase), 5*time.Minute)

	// Check we have seen the event before continuing to check the audit configuration has been applied correctly now
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

	testCouchbase := clusterOptions().WithPersistentTopology(clusterSize).WithAuditing(true).WithDefaultLogStreaming().Generate(targetKube)

	// Create a dodgy log config to prove it is reconciled correctly later
	e2eutil.SetInvalidLogStreamingConfig(t, targetKube, testCouchbase.Spec.Logging.Server.ConfigurationName)
	e2eutil.MustFailLoggingConfig(t, targetKube, testCouchbase)

	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Confirm that config is now correct
	// Special case as we create the Secret first so the ownerRef should not be the cluster
	e2eutil.MustCheckLoggingConfigNotOwned(t, targetKube, testCouchbase)
	e2eutil.MustCheckAuditConfiguration(t, targetKube, testCouchbase)

	// Wait for logging to be ready on each pod, then check that each pod is exporting some logs
	e2eutil.MustWaitForLoggingSidecarReady(t, targetKube, testCouchbase, 5*time.Minute)
	// Note that the sidecars start fast but have to wait for logs to appear from the slower server container
	e2eutil.MustCheckLogging(t, targetKube, testCouchbase, 5*time.Minute)
	// Checks for extra information we have started adding, e.g.:
	// "pod"=>{"namespace"=>"test-qmf56", "name"=>"test-couchbase-rjbbr-0000", "uid"=>"66c769fd-74b1-47bf-90a9-364eb649dcb9"}, "couchbase"=>{"cluster"=>"test-couchbase-rjbbr", "operator.version"=>"2.2.0", "server.version"=>"6.6.2", "node"=>"test-couchbase-rjbbr-0000", "node-config"=>"default", "server"=>"true", "data"=>"enabled", "index"=>"enabled"}
	// We do not check for specific values, only that they're being provided.
	e2eutil.MustCheckLogsForString(t, targetKube, testCouchbase, 10*time.Second, "\"pod\"=>{")
	e2eutil.MustCheckLogsForString(t, targetKube, testCouchbase, 10*time.Second, "\"couchbase\"=>{")
	e2eutil.MustCheckLogsForString(t, targetKube, testCouchbase, 10*time.Second, "operator.version")
	e2eutil.MustCheckLogsForString(t, targetKube, testCouchbase, 10*time.Second, "server.version")

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

	// GC is not supported without logging - tested by validation - so disable here.
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).WithAuditing(false).MustCreate(t, targetKube)

	e2eutil.MustCheckAuditConfiguration(t, targetKube, testCouchbase)
	// Check no extra sidecars
	e2eutil.MustCheckLoggingSidecarsCount(t, targetKube, testCouchbase, 0)

	// Patch in some deltas to configuration and check it is in place now
	// The REST API for audit is particularly difficult/non-standard so testing each of the fields as a sanity check
	newConfig := &v2.CouchbaseClusterAuditLoggingSpec{
		Enabled: true,
		// Single element arrays seem troublesome for Erlang to parse so explicitly test
		DisabledEvents: []int{8243},
		DisabledUsers:  []v2.AuditDisabledUser{"@eventing/local", "@cbq-engine/local"},
		Rotation: &v2.CouchbaseClusterLogRotationSpec{
			Size: k8sutil.NewResourceQuantityMi(1),
		},
	}

	// Apply the new configuration
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/logging/audit", newConfig), time.Minute)

	// Check that the user can see the cluster settings being edited twice.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(1),
		eventschema.Repeat{
			Times:     2,
			Validator: eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Additional logs can be streamed to standard output with a custom log configuration. Custom configuration is ignored for reconcile.
func TestCustomLogging(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Do not enable auditing so no logging will be enabled unless we have a custom configuration
	testCouchbase := clusterOptions().WithPersistentTopology(clusterSize).WithCustomLogStreaming().Generate(targetKube)

	config := map[string]string{
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
			"    Tag couchbase.log.lookforme\n",

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
			"    Time_Format    %Y-%m-%dT%H:%M:%S.%L%z\n",
	}

	e2eutil.SetLogStreamingConfig(t, targetKube, testCouchbase, config, false)
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Confirm that config is correct
	e2eutil.MustCheckLoggingConfigNotOwned(t, targetKube, testCouchbase)
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
	e2eutil.SetInvalidLogStreamingConfig(t, targetKube, testCouchbase.Spec.Logging.Server.ConfigurationName)
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

	options := clusterOptions()
	options.Options.LoggingImage = "fluent/fluent-bit:1.6.10"

	testCouchbase := options.WithPersistentTopology(clusterSize).WithDefaultLogStreaming().MustCreate(t, targetKube)

	// Confirm that config is now correct
	e2eutil.MustCheckLoggingConfig(t, targetKube, testCouchbase)

	// Wait for logging to be ready on each pod, then check that each pod is exporting some logs
	e2eutil.MustWaitForLoggingSidecarReady(t, targetKube, testCouchbase, 5*time.Minute)

	// Check for the specific version of Fluent-bit in the logs
	e2eutil.MustCheckLogsForString(t, targetKube, testCouchbase, time.Minute, "Fluent Bit v1.6.10")
}

func TestInflightLogRedaction(t *testing.T) {
	if strings.HasPrefix(clusterOptions().Options.LoggingImage, "fluent/fluent-bit") {
		t.Skip("Official Fluent Bit image does not support redaction out of the box")
	}

	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	testCouchbase := clusterOptions().WithPersistentTopology(clusterSize).WithCustomLogStreaming().Generate(targetKube)

	unredactedInputString := "Cats are <ud>sma#@&*+-.!!!!!rter</ud> than dogs, and <UD>sheeps</UD>"

	config := map[string]string{
		"fluent-bit.conf": "[SERVICE]\n" +
			"    flush        	1\n" +
			"    daemon       	Off\n" +
			"    log_level    	warn\n" +
			"    parsers_file 	/fluent-bit/etc/parsers-couchbase.conf\n" +

			"# Simple test generator for redaction\n" +
			"[INPUT]\n" +
			"    Name dummy\n" +
			"    Tag couchbase.redact.test\n" +
			"    Dummy {\"message\": \"" + unredactedInputString + "\"}\n\n" +

			"# Redaction of fields\n" +
			"[FILTER]\n" +
			"    Name    lua\n" +
			"    Match   couchbase.redact.*\n" +
			"    script  /fluent-bit/etc/redaction.lua\n" +
			"    call    cb_sub_message\n\n" +

			"# Now rewrite the tags for redacted information\n" +
			"[FILTER]\n" +
			"    Name rewrite_tag\n" +
			"    Match couchbase.redact.*\n" +
			"    Rule message .* couchbase.log.$TAG[2] false\n\n" +

			"# Output all parsed logs by default\n" +
			"[OUTPUT]\n" +
			"    name  stdout\n" +
			"    match couchbase.log.*\n",

		// Provide an empty salt to allow for detection of actual hash
		"redaction.salt": "",
	}

	e2eutil.SetLogStreamingConfig(t, targetKube, testCouchbase, config, false)
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Wait for logging to be ready on each pod, then check that each pod is exporting some logs
	e2eutil.MustWaitForLoggingSidecarReady(t, targetKube, testCouchbase, 5*time.Minute)

	// Specific checks for redacted information being replaced (and not output)
	e2eutil.MustCheckLogsForString(t, targetKube, testCouchbase, 5*time.Minute, "Cats are <ud>00b335216f27c1e7d35149b5bbfe19d4eb2d6af1</ud> than dogs, and <ud>888f807d45ff6ce47240c7ed4e884a6f9dc7b4fb</ud>")
	e2eutil.MustCheckLogsAreMissingString(t, targetKube, testCouchbase, time.Minute, unredactedInputString)
}

func TestRebalanceLogProcessing(t *testing.T) {
	if strings.HasPrefix(clusterOptions().Options.LoggingImage, "fluent/fluent-bit") {
		t.Skip("Official Fluent Bit image does not support redaction out of the box")
	}

	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	testCouchbase := clusterOptions().WithPersistentTopology(clusterSize).WithDefaultLogStreaming().MustCreate(t, targetKube)

	testCouchbase = e2eutil.MustResizeClusterNoWait(t, 0, clusterSize+2, targetKube, testCouchbase)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustCheckLogsForString(t, targetKube, testCouchbase, 5*time.Minute, "couchbase.log.rebalance")
}

func TestLoggingDynamicConfigReload(t *testing.T) {
	if strings.HasPrefix(clusterOptions().Options.LoggingImage, "fluent/fluent-bit") {
		t.Skip("Official Fluent Bit image does not support redaction out of the box")
	}

	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	testCouchbase := clusterOptions().WithPersistentTopology(clusterSize).WithCustomLogStreaming().Generate(targetKube)

	// Start with logging with one tag then change to another and confirm the change
	tag1 := "couchbase.log.test.original"
	tag2 := "couchbase.log.test.updated"

	config1 := map[string]string{
		"fluent-bit.conf": "[SERVICE]\n" +
			"    flush        	1\n" +
			"    daemon       	Off\n" +
			"    log_level    	warn\n\n" +

			"# Simple test generator as we do not need actual logs\n" +
			"[INPUT]\n" +
			"    Name dummy\n" +
			"    Tag " + tag1 + "\n" +
			"    Dummy {\"message\": \"Testing dynamic reconfiguration - original\"}\n\n" +

			"[OUTPUT]\n" +
			"    name  stdout\n" +
			"    match couchbase.log.*\n",
	}

	// Defined here to make it easy to compare but used later
	config2 := map[string]string{
		"fluent-bit.conf": "[SERVICE]\n" +
			"    flush        	1\n" +
			"    daemon       	Off\n" +
			"    log_level    	warn\n\n" +

			"# Simple test generator as we do not need actual logs\n" +
			"[INPUT]\n" +
			"    Name dummy\n" +
			"    Tag " + tag2 + "\n" +
			"    Dummy {\"message\": \"Testing dynamic reconfiguration - updated\"}\n\n" +

			"[OUTPUT]\n" +
			"    name  stdout\n" +
			"    match couchbase.log.*\n",
	}

	e2eutil.SetLogStreamingConfig(t, targetKube, testCouchbase, config1, false)
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Wait for logging to be ready on each pod, then check that each pod is exporting some logs
	e2eutil.MustWaitForLoggingSidecarReady(t, targetKube, testCouchbase, 5*time.Minute)

	e2eutil.MustCheckLogsForString(t, targetKube, testCouchbase, time.Minute, tag1)
	e2eutil.MustCheckLogsAreMissingString(t, targetKube, testCouchbase, time.Minute, tag2)

	// Switch config - can take a while to be applied
	e2eutil.SetLogStreamingConfig(t, targetKube, testCouchbase, config2, true)

	// Invert the check to confirm the new stuff is present
	// Note the old tag may still be in older parts of the log so do not search for it
	// e2eutil.MustCheckLogsAreMissingString(t, targetKube, testCouchbase, time.Minute, tag1)
	e2eutil.MustCheckLogsForString(t, targetKube, testCouchbase, 5*time.Minute, tag2)
}

// TestLoggingUpgrade starts the logging sidecar with the logging upgrade image,
// waits for the cluster to be healthy, then updates to the logging image, and
// checks the cluster has successfully upgraded.
func TestLoggingUpgrade(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, targetKube).LoggingUpgradable()

	// Static configuration.
	clusterSize := 1

	// Create the cluster.
	testCouchbase := clusterOptionsUpgradeLogging().WithPersistentTopology(clusterSize).WithDefaultLogStreaming().MustCreate(t, targetKube)

	// Wait for the cluster to be ready.
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 10*time.Minute)
	e2eutil.MustWaitForLoggingSidecarReady(t, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustCheckLogging(t, targetKube, testCouchbase, 5*time.Minute)

	// Upgrade the cluster with new logging image and check again if logging is enabled.
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/logging/server/sidecar/image", f.CouchbaseLoggingImage), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, targetKube, v2.ClusterConditionUpgrading, corev1.ConditionTrue, testCouchbase, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 10*time.Minute)

	// Wait for Fluent Bit to be ready on each pod, then check that each pod is exporting the expected Couchbase logs.
	e2eutil.MustWaitForLoggingSidecarReady(t, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustCheckLogging(t, targetKube, testCouchbase, 5*time.Minute)

	// Check that the image is the new version. This won't work if using the 'latest' tag.
	targetVersion := strings.Split(f.CouchbaseLoggingImage, ":")[1]
	searchString := fmt.Sprintf(`"version":"%s (build `, targetVersion)
	e2eutil.MustCheckLogsForString(t, targetKube, testCouchbase, 5*time.Minute, searchString)

	// Check the events match what we expect:
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}
