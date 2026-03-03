/*
Copyright 2017-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2e

import (
	"fmt"
	"testing"
	"time"

	pkgconstants "github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
)

const (
	// version of couchbase server in relation to redhat catalog.
	defaultOlmServerVersion = "couchbase-7.0.4-1"
	// version of backup image in relation to redhat catalog.
	defaultOlmBackupVersion = "backup-1.3.0-2"
	// version of exporter sidecar in relation to redhat catalog.
	defaultOlmMetricVersion = "metrics-1.0.6-2"
)

// createRelatedVersionEnvVars is a helper function for creating
// environment variables used to specify overriding sidecar versions.
// Within the context of OLM (operator lifecycle manager) these vars
// are called 'Related Image Versions' as they allow the operator to
// pre-define related images (sidecars) for pre-installation in a
// disconnected deployment.
func createRelatedVersionEnvVars(serverVersion string, backupVersion string, exporterVersion string) []corev1.EnvVar {
	// perform reverse lookup to get sha256 digest of version string
	for digest, value := range pkgconstants.ImageDigests {
		switch value {
		case serverVersion:
			serverVersion = "registry.connect.redhat.com/couchbase/server@sha256:" + digest
		case backupVersion:
			backupVersion = "registry.connect.redhat.com/couchbase/operator-backup@sha256:" + digest
		case exporterVersion:
			exporterVersion = "registry.connect.redhat.com/couchbase/exporter@sha256:" + digest
		}
	}

	return []corev1.EnvVar{
		{
			Name:  pkgconstants.EnvCouchbaseImageName,
			Value: serverVersion,
		},
		{
			Name:  pkgconstants.EnvBackupImageName,
			Value: backupVersion,
		},
		{
			Name:  pkgconstants.EnvMetricsImageName,
			Value: exporterVersion,
		},
	}
}

// TestPauseControl tests the user can pause the operator from controlling
// an couchbase cluster.
func TestPauseOperator(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Pause the operator, kill a pod, ensure nothing comes back from the dead, then reenable the operator.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/status/controlPaused", true), time.Minute)
	e2eutil.KillPods(t, kubernetes, cluster, 1)
	e2eutil.MustAssertFor(t, time.Minute, e2eutil.ResourceCondition(kubernetes, cluster, "Available", "True"))
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/paused", false), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Member failed over
	// * Member replaced
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		e2eutil.PodDownFailoverRecoverySequence(),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestKillOperator(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Kill the operator, wait for recovery and make sure nothing bad happened to the cluster.
	e2eutil.MustKillOperatorAndWaitForRecovery(t, kubernetes)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestKillOperatorAndUpdateClusterConfig ensures that manual changes to bucket
// configuration are reverted by the operator during a restart.
func TestKillOperatorAndUpdateClusterConfig(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size1

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// When the cluster is ready, pause the operator, and await aknowledgement.  Manually update the bucket, then kill
	// and unpause the operator.  Verify the bucket was updated and wait for it to revert as the operator regains mastership.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/status/controlPaused", true), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Replace("/EnableFlush", false), time.Minute)
	e2eutil.MustDeleteCouchbaseOperator(t, kubernetes)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/paused", false), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, k8sutil.BucketEditEvent(bucket.GetName(), cluster), 2*time.Minute)

	// Check the events match what we expect:
	// * Admin console service created
	// * Cluster created
	// * Bucket edited (reverted)
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBucketEdited},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestImageVersionPrecedence checks that spec.image has default precedence
// over image version provided through the operators environment variable.
func TestImageVersionDefaultPrecedence(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	e2eutil.MustDeleteOperatorDeployment(t, kubernetes, time.Minute)

	envVars := createRelatedVersionEnvVars(defaultOlmServerVersion, defaultOlmBackupVersion, defaultOlmMetricVersion)
	e2eutil.MustCreateOperatorDeploymentWithEnvVars(t, kubernetes, envVars)

	// Create the cluster.
	clusterSize := 1
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Verify that the version displayed in cluster.status matches cluster.spec
	// as this indicates we did not give precedence to the environment variable.
	expectedServerVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage)
	// (assuming that no one is providing redhat style server versions to begin with)
	if expectedServerVersion == defaultOlmServerVersion {
		e2eutil.Die(t, fmt.Errorf("initial server version %s already matches version provided to EnvVar", expectedServerVersion))
	}

	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, expectedServerVersion, time.Minute)

	// Since this test isn't going to be running in OpenShift then we cannot expect
	// upgrade to succeed because the images refer to the redhat registry.
	// The point is simply to demonstrate that the env var has taken precedence.
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestImageVersionPrecedence checks that spec.image has default precedence
// over image version provided through the operators environment variable.
func TestImageVersionEnvPrecedence(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	e2eutil.MustDeleteOperatorDeployment(t, kubernetes, time.Minute)

	envVars := createRelatedVersionEnvVars(defaultOlmServerVersion, defaultOlmBackupVersion, defaultOlmMetricVersion)
	e2eutil.MustCreateOperatorDeploymentWithEnvVars(t, kubernetes, envVars)

	// Static configuration.
	clusterSize := 1

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Patching the clustr to give precedence to Env Var should result in an upgrade.
	_ = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/envImagePrecedence", true), time.Minute)

	// Since this test isn't going to be running in OpenShift then we cannot expect
	// upgrade to succeed because the images refer to the redhat registry.
	// The point is simply to demonstrate that the env var has taken precedence.
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestNoPodDeleteDelayRespected(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// static configuration
	clusterSize := constants.Size2

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	_, err := e2eutil.ResizeCluster(0, constants.Size1, kubernetes, cluster, 5*time.Minute)

	if err != nil {
		t.Errorf("TestNoPodDeleteDelayRespected should not timeout.")
	}
}

func TestPodDeletedAfterExpectedDelay(t *testing.T) {
	f := framework.Global
	f.PodDeleteDelay = 2 * time.Minute

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// static configuration
	clusterSize := constants.Size3

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	_, err := e2eutil.ResizeCluster(0, constants.Size2, kubernetes, cluster, 1*time.Minute)
	if err != nil {
		t.Errorf("TestPodDeletedAfterExpectedDelay failed")
	}

	time.Sleep(2 * time.Minute)

	_, err = e2eutil.ResizeCluster(0, constants.Size1, kubernetes, cluster, 5*time.Minute)
	if err != nil {
		t.Errorf("TestPodDeletedAfterExpectedDelay should not timeout" + err.Error())
	}
}

func TestPodDeleteDelayRespected(t *testing.T) {
	f := framework.Global
	f.PodDeleteDelay = 3 * time.Minute

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// static configuration
	clusterSize := constants.Size2

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	_, err := e2eutil.ResizeCluster(0, constants.Size1, kubernetes, cluster, 1*time.Minute)
	if err != nil {
		t.Errorf("TestPodDeleteDelayRespected failed: " + err.Error())
	}
}
