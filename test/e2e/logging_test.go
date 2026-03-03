/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2e

import (
	"fmt"
	"strconv"
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
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	clusterSize int = 1
)

// Audit configuration reconciled (CRD = disabled, manually enabled and then reset back to disabled).
func TestNoLogOrAuditConfig(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Check no audit enabled
	e2eutil.MustCheckAuditConfiguration(t, kubernetes, cluster)
	// Check no extra sidecars
	e2eutil.MustCheckLoggingSidecarsCount(t, kubernetes, cluster, 0)

	// Check we can manually enable, i.e. existing behaviour
	e2eutil.MustEnableAuditLogging(t, kubernetes, cluster)

	// Apply the new configuration and set explicitly false
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/logging/audit", &v2.CouchbaseClusterAuditLoggingSpec{Enabled: false}), time.Minute)

	// We only have one of these events so no need to mess with race conditions & async waits
	e2eutil.MustObserveClusterEvent(t, kubernetes, cluster, k8sutil.ClusterSettingsEditedEvent("audit", cluster), 5*time.Minute)

	// Check we have seen the event before continuing to check the audit configuration has been applied correctly now
	e2eutil.MustCheckAuditConfiguration(t, kubernetes, cluster)

	// Check that the user can see the cluster being edited.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(1),
		eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Audit log appears on standard output when enabled.
// Audit logs are removed automatically by the GC - confirm with short rotation interval and check output of the sidecar includes log names.
func TestLoggingAndAuditingDefaults(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	cluster := clusterOptions().WithPersistentTopology(clusterSize).WithAuditing(true).WithDefaultLogStreaming().Generate(kubernetes)

	// Create a dodgy log config to prove it is reconciled correctly later
	e2eutil.SetInvalidLogStreamingConfig(t, kubernetes, cluster.Spec.Logging.Server.ConfigurationName)
	e2eutil.MustFailLoggingConfig(t, kubernetes, cluster)

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Confirm that config is now correct
	// Special case as we create the Secret first so the ownerRef should not be the cluster
	e2eutil.MustCheckLoggingConfigNotOwned(t, kubernetes, cluster)
	e2eutil.MustCheckAuditConfiguration(t, kubernetes, cluster)

	// Wait for logging to be ready on each pod, then check that each pod is exporting some logs
	e2eutil.MustWaitForLoggingSidecarReady(t, kubernetes, cluster, 5*time.Minute)
	// Note that the sidecars start fast but have to wait for logs to appear from the slower server container
	e2eutil.MustCheckLogging(t, kubernetes, cluster, 5*time.Minute)
	// Checks for extra information we have started adding, e.g.:
	// "pod"=>{"namespace"=>"test-qmf56", "name"=>"test-couchbase-rjbbr-0000", "uid"=>"66c769fd-74b1-47bf-90a9-364eb649dcb9"}, "couchbase"=>{"cluster"=>"test-couchbase-rjbbr", "operator.version"=>"2.2.0", "server.version"=>"6.6.2", "node"=>"test-couchbase-rjbbr-0000", "node-config"=>"default", "server"=>"true", "data"=>"enabled", "index"=>"enabled"}
	// We do not check for specific values, only that they're being provided.
	e2eutil.MustCheckLogsForString(t, kubernetes, cluster, 10*time.Second, "\"pod\"=>{")
	e2eutil.MustCheckLogsForString(t, kubernetes, cluster, 10*time.Second, "\"couchbase\"=>{")
	e2eutil.MustCheckLogsForString(t, kubernetes, cluster, 10*time.Second, "operator.version")
	e2eutil.MustCheckLogsForString(t, kubernetes, cluster, 10*time.Second, "server.version")

	// Check that the user can see the cluster settings being edited but only once.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(1),
		eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// If no GC enabled then sidecar is not running.
// Can enable auditing without logging.
// Audit configuration passed through to REST API - verify with some custom updates to the CRD and check the audit REST API response.
func TestAuditingNoLogging(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// GC is not supported without logging - tested by validation - so disable here.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithAuditing(false).MustCreate(t, kubernetes)

	e2eutil.MustCheckAuditConfiguration(t, kubernetes, cluster)
	// Check no extra sidecars
	e2eutil.MustCheckLoggingSidecarsCount(t, kubernetes, cluster, 0)

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
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/logging/audit", newConfig), time.Minute)

	// Check that the user can see the cluster settings being edited twice.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(1),
		eventschema.Repeat{
			Times:     2,
			Validator: eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Additional logs can be streamed to standard output with a custom log configuration. Custom configuration is ignored for reconcile.
func TestCustomLogging(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Do not enable auditing so no logging will be enabled unless we have a custom configuration
	cluster := clusterOptions().WithPersistentTopology(clusterSize).WithCustomLogStreaming().Generate(kubernetes)

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

	e2eutil.SetLogStreamingConfig(t, kubernetes, cluster, config, false)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Confirm that config is correct
	e2eutil.MustCheckLoggingConfigNotOwned(t, kubernetes, cluster)
	e2eutil.MustCheckAuditConfiguration(t, kubernetes, cluster)

	// Wait for logging to be ready on each pod, then check that each pod is exporting some logs
	e2eutil.MustWaitForLoggingSidecarReady(t, kubernetes, cluster, 5*time.Minute)
	// General check for some logs
	e2eutil.MustCheckLogging(t, kubernetes, cluster, 5*time.Minute)
	// Specific check for logs we have specified in the custom config that are not in the default one
	e2eutil.MustCheckLogsForString(t, kubernetes, cluster, 5*time.Minute, "couchbase.log.lookforme")

	// Ensure no audit cleanup
	e2eutil.MustCheckLoggingSidecarsCount(t, kubernetes, cluster, 1)

	// Ensure we can mess up the custom config and it is not reconciled and pods are not restarted
	e2eutil.SetInvalidLogStreamingConfig(t, kubernetes, cluster.Spec.Logging.Server.ConfigurationName)
	e2eutil.MustFailLoggingConfig(t, kubernetes, cluster)

	// Check that the user can not see the cluster settings being edited as they're custom.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(1),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestChangeLogShipperImage(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	options := clusterOptions()
	options.Options.LoggingImage = "fluent/fluent-bit:1.6.10"

	cluster := options.WithPersistentTopology(clusterSize).WithDefaultLogStreaming().MustCreate(t, kubernetes)

	// Confirm that config is now correct
	e2eutil.MustCheckLoggingConfig(t, kubernetes, cluster)

	// Wait for logging to be ready on each pod, then check that each pod is exporting some logs
	e2eutil.MustWaitForLoggingSidecarReady(t, kubernetes, cluster, 5*time.Minute)

	// Check for the specific version of Fluent-bit in the logs
	e2eutil.MustCheckLogsForString(t, kubernetes, cluster, time.Minute, "Fluent Bit v1.6.10")
}

func TestInflightLogRedaction(t *testing.T) {
	if strings.HasPrefix(clusterOptions().Options.LoggingImage, "fluent/fluent-bit") {
		t.Skip("Official Fluent Bit image does not support redaction out of the box")
	}

	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	cluster := clusterOptions().WithPersistentTopology(clusterSize).WithCustomLogStreaming().Generate(kubernetes)

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

	e2eutil.SetLogStreamingConfig(t, kubernetes, cluster, config, false)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Wait for logging to be ready on each pod, then check that each pod is exporting some logs
	e2eutil.MustWaitForLoggingSidecarReady(t, kubernetes, cluster, 5*time.Minute)

	// Specific checks for redacted information being replaced (and not output)
	e2eutil.MustCheckLogsForString(t, kubernetes, cluster, 5*time.Minute, "{\"message\"=>\"Cats are <ud>00b335216f27c1e7d35149b5bbfe19d4eb2d6af1</ud> than dogs, and <ud>888f807d45ff6ce47240c7ed4e884a6f9dc7b4fb</ud>")
	// We need to make sure we look for the specific log output string rather than the string in the configuration to generate it
	e2eutil.MustCheckLogsAreMissingString(t, kubernetes, cluster, time.Minute, "{\"message\"=>\""+unredactedInputString)
}

func TestRebalanceLogProcessing(t *testing.T) {
	if strings.HasPrefix(clusterOptions().Options.LoggingImage, "fluent/fluent-bit") {
		t.Skip("Official Fluent Bit image does not support redaction out of the box")
	}

	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	cluster := clusterOptions().WithPersistentTopology(clusterSize).WithDefaultLogStreaming().MustCreate(t, kubernetes)

	cluster = e2eutil.MustResizeClusterNoWait(t, 0, clusterSize+2, kubernetes, cluster)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckLogsForString(t, kubernetes, cluster, 5*time.Minute, "couchbase.log.rebalance")
}

func TestLoggingDynamicConfigReload(t *testing.T) {
	if strings.HasPrefix(clusterOptions().Options.LoggingImage, "fluent/fluent-bit") {
		t.Skip("Official Fluent Bit image does not support redaction out of the box")
	}

	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	cluster := clusterOptions().WithPersistentTopology(clusterSize).WithCustomLogStreaming().Generate(kubernetes)

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

	e2eutil.SetLogStreamingConfig(t, kubernetes, cluster, config1, false)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Wait for logging to be ready on each pod, then check that each pod is exporting some logs
	e2eutil.MustWaitForLoggingSidecarReady(t, kubernetes, cluster, 5*time.Minute)

	e2eutil.MustCheckLogsForString(t, kubernetes, cluster, time.Minute, tag1)
	e2eutil.MustCheckLogsAreMissingString(t, kubernetes, cluster, time.Minute, tag2)

	// Switch config - can take a while to be applied
	e2eutil.SetLogStreamingConfig(t, kubernetes, cluster, config2, true)

	// Invert the check to confirm the new stuff is present
	// Note the old tag may still be in older parts of the log so do not search for it
	// e2eutil.MustCheckLogsAreMissingString(t, kubernetes, cluster, time.Minute, tag1)
	e2eutil.MustCheckLogsForString(t, kubernetes, cluster, 5*time.Minute, tag2)
}

// TestLoggingUpgrade starts the logging sidecar with the logging upgrade image,
// waits for the cluster to be healthy, then updates to the logging image, and
// checks the cluster has successfully upgraded.
func TestLoggingUpgrade(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).LoggingUpgradable()

	// Static configuration.
	clusterSize := 1

	// Create the cluster.
	cluster := clusterOptionsUpgradeLogging().WithPersistentTopology(clusterSize).WithDefaultLogStreaming().MustCreate(t, kubernetes)

	// Wait for the cluster to be ready.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)
	e2eutil.MustWaitForLoggingSidecarReady(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckLogging(t, kubernetes, cluster, 5*time.Minute)

	// Upgrade the cluster with new logging image and check again if logging is enabled.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/logging/server/sidecar/image", f.CouchbaseLoggingImage), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, v2.ClusterConditionUpgrading, corev1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	// Wait for Fluent Bit to be ready on each pod, then check that each pod is exporting the expected Couchbase logs.
	e2eutil.MustWaitForLoggingSidecarReady(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckLogging(t, kubernetes, cluster, 5*time.Minute)

	// Check that the image is the new version. This won't work if using the 'latest' tag.
	targetVersion := strings.Split(f.CouchbaseLoggingImage, ":")[1]
	targetVersion = strings.Split(targetVersion, "-")[0]
	searchString := fmt.Sprintf(`"version":"%s (build `, targetVersion)
	e2eutil.MustCheckLogsForString(t, kubernetes, cluster, 5*time.Minute, searchString)

	// Check the events match what we expect:
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestLoggingMemoryBufferLimits starts the server with a pod annotations
// which enables memory buffer limits in the logging sidecar.
// Checks the sidecar logs to test that these limits are set.
func TestLoggingMemoryBufferLimits(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastLoggingVersion("1.2.0")

	maxMem := e2eutil.MustGetMaxNodeMem(t, kubernetes)
	memLimit := strconv.Itoa(int(0.5*maxMem)) + "M"

	newAnnotations := make(map[string]string)
	newAnnotations["fluentbit.couchbase.com/mem.buf.limits.enabled"] = "true"

	cluster := clusterOptions().WithPersistentTopology(clusterSize).WithDefaultLogStreaming().WithPodAnnotations(newAnnotations).Generate(kubernetes)
	cluster.Spec.Logging.Server.Sidecar.Resources = &corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse(memLimit),
		},
	}

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	e2eutil.MustAddCustomAnnotationAndLabels(t, kubernetes, cluster, newAnnotations, nil)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Check no audit enabled
	e2eutil.MustCheckAuditConfiguration(t, kubernetes, cluster)

	// Wait for logging to be ready on each pod, then check that each pod is exporting some logs
	e2eutil.MustWaitForLoggingSidecarReady(t, kubernetes, cluster, 5*time.Minute)
	// General check for some logs
	e2eutil.MustCheckLogging(t, kubernetes, cluster, 5*time.Minute)

	e2eutil.MustCheckLogsForString(t, kubernetes, cluster, 5*time.Minute, "Setting new memory buffer limits")

	// Ensure no audit cleanup
	e2eutil.MustCheckLoggingSidecarsCount(t, kubernetes, cluster, 1)

	// Check that the user can not see the cluster settings being edited as they're custom.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(1),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
