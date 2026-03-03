/*
Copyright 2024-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	v1 "k8s.io/api/core/v1"
)

func TestMigrateCluster(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	clusterSize := 3

	// Create the source cluster.
	srcCluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Pause the source cluster.
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Test("/status/controlPaused", true), time.Minute)

	dstCluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	dstCluster.Spec.Migration = &couchbasev2.ClusterAssimilationSpec{
		UnmanagedClusterHost: fmt.Sprintf("%s.%s.svc.cluster.local", srcCluster.Name, srcCluster.Namespace),
	}

	dstCluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, dstCluster)

	// Check that the cluster goes to healthy
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, dstCluster, 3*time.Minute)

	// Check that all nodes from the initial cluster have been ejected
	e2eutil.MustBeUninitializedCluster(t, kubernetes, srcCluster)

	// Check that we've removed the migrating condition from the cluster
	e2eutil.MustWaitForClusterConditionsRemoved(t, kubernetes, dstCluster, 5*time.Minute, couchbasev2.ClusterConditionMigrating)
}

func TestMigrateLeaveUnmanagedCluster(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	clusterSize := 3
	unmanagedNodes := 1
	// Create the source cluster.
	srcCluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Pause the source cluster.
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Test("/status/controlPaused", true), time.Minute)

	dstCluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	dstCluster.Spec.Migration = &couchbasev2.ClusterAssimilationSpec{
		UnmanagedClusterHost: fmt.Sprintf("%s.%s.svc.cluster.local", srcCluster.Name, srcCluster.Namespace),
		NumUnmanagedNodes:    1,
	}

	dstCluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, dstCluster)

	MustValidateNumManagedNodes(t, kubernetes, dstCluster, clusterSize-unmanagedNodes)

	MustValidateClusterSize(t, kubernetes, dstCluster, clusterSize)
}

func TestMigratePremigrationNodes(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	clusterSize := 3
	preMigrationSize := 2

	// Create the source cluster.
	srcCluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Pause the source cluster.
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Test("/status/controlPaused", true), time.Minute)

	dstCluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	dstCluster.Spec.Migration = &couchbasev2.ClusterAssimilationSpec{
		UnmanagedClusterHost: fmt.Sprintf("%s.%s.svc.cluster.local", srcCluster.Name, srcCluster.Namespace),
	}

	dstCluster.Spec.Servers = append(dstCluster.Spec.Servers, couchbasev2.ServerConfig{
		Name:     "premigration",
		Size:     preMigrationSize,
		Services: []couchbasev2.Service{couchbasev2.EventingService},
	})

	dstCluster = e2eutil.CreateNewClusterFromSpec(t, kubernetes, dstCluster, -1)

	e2eutil.MustWaitForClusterEvent(t, kubernetes, dstCluster, e2eutil.NewMemberAddedEvent(dstCluster, preMigrationSize-1), 10*time.Minute)

	MustValidateClusterSize(t, kubernetes, dstCluster, clusterSize+preMigrationSize)
}

func TestMigrateStabilizationPeriod(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	clusterSize := 3
	stabilizationPeriodS := int64(60)

	// Create the source cluster.
	srcCluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Pause the source cluster.
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Test("/status/controlPaused", true), time.Minute)

	dstCluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	dstCluster.Spec.Migration = &couchbasev2.ClusterAssimilationSpec{
		UnmanagedClusterHost: fmt.Sprintf("%s.%s.svc.cluster.local", srcCluster.Name, srcCluster.Namespace),
		StabilizationPeriod:  e2espec.NewDurationS(stabilizationPeriodS),
	}

	dstCluster = e2eutil.CreateNewClusterFromSpec(t, kubernetes, dstCluster, -1)

	// Check that the cluster goes into the waiting state the right number of times
	for i := 0; i < clusterSize-1; i++ {
		e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionWaitingBetweenMigrations, v1.ConditionTrue, dstCluster, 10*time.Minute)

		// Validate that it stays in the waiting state for the right amount of time (minus 10 seconds so not too flakey)
		e2eutil.AssertClusterConditionFor(t, kubernetes, couchbasev2.ClusterConditionWaitingBetweenMigrations, v1.ConditionTrue, dstCluster, time.Duration(stabilizationPeriodS-10)*time.Second)

		if i < clusterSize-2 {
			e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionWaitingBetweenMigrations, v1.ConditionFalse, dstCluster, 10*time.Minute)
		}
	}

	// Check that the cluster goes to healthy
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, dstCluster, 3*time.Minute)

	// Check that all nodes from the initial cluster have been ejected
	e2eutil.MustBeUninitializedCluster(t, kubernetes, srcCluster)

	// Check that we've removed the migrating and stabilization period condition from the cluster
	e2eutil.MustWaitForClusterConditionsRemoved(t, kubernetes, dstCluster, 5*time.Minute, couchbasev2.ClusterConditionMigrating, couchbasev2.ClusterConditionWaitingBetweenMigrations)
}

func TestMigrateMaxConcurrency(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	clusterSize := 3

	// Create the source cluster.
	srcCluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Pause the source cluster.
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Test("/status/controlPaused", true), time.Minute)

	dstCluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	dstCluster.Spec.Migration = &couchbasev2.ClusterAssimilationSpec{
		UnmanagedClusterHost:    fmt.Sprintf("%s.%s.svc.cluster.local", srcCluster.Name, srcCluster.Namespace),
		MaxConcurrentMigrations: 2,
	}

	dstCluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, dstCluster)

	// Check that all nodes from the initial cluster have been ejected
	e2eutil.MustBeUninitializedCluster(t, kubernetes, srcCluster)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.MultiNodeSwapRebalanceSequence(2),
		e2eutil.MultiNodeSwapRebalanceSequence(1),
	}

	ValidateEvents(t, kubernetes, dstCluster, expectedEvents)
}

// TestMigrateClusterWithMultipleServerGroups checks that migration will take several server groups/zones into account and correctly schedule pods onto the correct nodes.
// This test will also check that we set the correct server groups on couchbase server.
func TestMigrateClusterWithMultipleServerGroups(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).ServerGroups(2)

	serverGroups := getAvailabilityZones(t, kubernetes)
	clusterSize := len(serverGroups)

	// Create the source cluster.
	srcCluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	srcCluster.Spec.ServerGroups = serverGroups
	srcCluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, srcCluster)

	// Pause the source cluster.
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Test("/status/controlPaused", true), time.Minute)

	// Create the destination cluster
	dstCluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	dstCluster.Spec.ServerGroups = serverGroups
	dstCluster.Spec.Migration = &couchbasev2.ClusterAssimilationSpec{
		UnmanagedClusterHost: fmt.Sprintf("%s.%s.svc.cluster.local", srcCluster.Name, srcCluster.Namespace),
	}
	dstCluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, dstCluster)

	// Check that all nodes from the initial cluster have been ejected
	e2eutil.MustBeUninitializedCluster(t, kubernetes, srcCluster)

	// Check that we've removed the migrating condition from the cluster
	e2eutil.MustWaitForClusterConditionsRemoved(t, kubernetes, dstCluster, 5*time.Minute, couchbasev2.ClusterConditionMigrating)

	// Check that the pods have been scheduled correctly
	expected := getExpectedRzaResultMap(clusterSize, serverGroups)
	expected.mustValidateRzaMap(t, kubernetes, dstCluster)

	// Check that we have the correct number of server groups on couchbase server
	e2eutil.MustWaitUntilCouchbaseServerGroupsExist(t, kubernetes, dstCluster, serverGroups)
}

func TestMigrationByServerClass(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	serverClassSize := 2
	clusterSize := serverClassSize * 2
	unmanagedNodes := serverClassSize
	// Create the source cluster.
	srcCluster := clusterOptions().WithMixedEphemeralTopology(serverClassSize).MustCreate(t, kubernetes)

	// Pause the source cluster.
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Test("/status/controlPaused", true), time.Minute)

	dstCluster := clusterOptions().WithMixedEphemeralTopology(serverClassSize).Generate(kubernetes)
	dstCluster.Spec.Migration = &couchbasev2.ClusterAssimilationSpec{
		UnmanagedClusterHost: fmt.Sprintf("%s.%s.svc.cluster.local", srcCluster.Name, srcCluster.Namespace),
		NumUnmanagedNodes:    unmanagedNodes,
		MigrationOrderOverride: &couchbasev2.MigrationOrderOverrideSpec{
			MigrationOrderOverrideStrategy: couchbasev2.ByServerClass,
			ServerClassOrder:               []string{srcCluster.Spec.Servers[1].Name, srcCluster.Spec.Servers[0].Name},
		},
	}

	dstCluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, dstCluster)

	MustValidateNumManagedNodes(t, kubernetes, dstCluster, clusterSize-unmanagedNodes)

	MustValidateClusterSize(t, kubernetes, dstCluster, clusterSize)

	expectedRemovedNodes := []int{2, 3}

	for _, nodeID := range expectedRemovedNodes {
		e2eutil.MustObserveClusterEvent(t, kubernetes, dstCluster, e2eutil.NewMemberRemovedEvent(srcCluster, nodeID), 3*time.Minute)
	}

	unmanagedNodes = 0
	dstCluster = e2eutil.MustPatchCluster(t, kubernetes, dstCluster, jsonpatch.NewPatchSet().Replace("/spec/migration/numUnmanagedNodes", unmanagedNodes), time.Minute)

	e2eutil.MustWaitUntilClusterUninitialized(t, kubernetes, srcCluster, 10*time.Minute)

	MustValidateClusterSize(t, kubernetes, dstCluster, clusterSize)
}

func TestMigrationByNode(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	clusterSize := 4
	// Create the source cluster.
	srcCluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Pause the source cluster.
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Test("/status/controlPaused", true), time.Minute)

	migrationOrderIds := []int{3, 1, 0, 2}
	nodeOrder := []string{}

	for _, id := range migrationOrderIds {
		nodeOrder = append(nodeOrder, GetMemberHostname(srcCluster, id))
	}

	dstCluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	dstCluster.Spec.Migration = &couchbasev2.ClusterAssimilationSpec{
		UnmanagedClusterHost: fmt.Sprintf("%s.%s.svc.cluster.local", srcCluster.Name, srcCluster.Namespace),
		MigrationOrderOverride: &couchbasev2.MigrationOrderOverrideSpec{
			MigrationOrderOverrideStrategy: couchbasev2.ByNode,
			NodeOrder:                      nodeOrder,
		},
	}

	dstCluster = e2eutil.CreateNewClusterFromSpec(t, kubernetes, dstCluster, -1)

	for _, id := range migrationOrderIds {
		e2eutil.MustWaitForClusterEvent(t, kubernetes, dstCluster, e2eutil.NewMemberRemovedEvent(srcCluster, id), 3*time.Minute)
	}

	// Check that the cluster goes to healthy
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, dstCluster, 3*time.Minute)

	// Check that all nodes from the initial cluster have been ejected
	e2eutil.MustBeUninitializedCluster(t, kubernetes, srcCluster)
}

func TestMigrationByServerGroup(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).ServerGroups(2)
	availableServerGroups := getAvailabilityZones(t, kubernetes)

	clusterSize := 4

	// Create the source cluster.
	srcCluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	srcCluster.Spec.ServerGroups = availableServerGroups[:2]

	srcCluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, srcCluster)

	// Pause the source cluster.
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Test("/status/controlPaused", true), time.Minute)

	dstCluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	dstCluster.Spec.ServerGroups = availableServerGroups[:2]

	unmanagedNodes := 1
	dstCluster.Spec.Migration = &couchbasev2.ClusterAssimilationSpec{
		UnmanagedClusterHost: fmt.Sprintf("%s.%s.svc.cluster.local", srcCluster.Name, srcCluster.Namespace),
		NumUnmanagedNodes:    unmanagedNodes,
		MigrationOrderOverride: &couchbasev2.MigrationOrderOverrideSpec{
			MigrationOrderOverrideStrategy: couchbasev2.ByServerGroup,
			ServerGroupOrder:               []string{availableServerGroups[1], availableServerGroups[0]},
		},
	}

	dstCluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, dstCluster)

	MustValidateNumManagedNodes(t, kubernetes, dstCluster, clusterSize-unmanagedNodes)

	MustValidateClusterSize(t, kubernetes, dstCluster, clusterSize)

	expectedRemovedNodes := MustGetPodIDsInServerGroup(t, kubernetes, srcCluster, availableServerGroups[1])

	for _, nodeID := range expectedRemovedNodes {
		e2eutil.MustObserveClusterEvent(t, kubernetes, dstCluster, e2eutil.NewMemberRemovedEvent(srcCluster, nodeID), 3*time.Minute)
	}

	unmanagedNodes = 0
	dstCluster = e2eutil.MustPatchCluster(t, kubernetes, dstCluster, jsonpatch.NewPatchSet().Replace("/spec/migration/numUnmanagedNodes", unmanagedNodes), time.Minute)

	e2eutil.MustWaitUntilClusterUninitialized(t, kubernetes, srcCluster, 10*time.Minute)

	MustValidateClusterSize(t, kubernetes, dstCluster, clusterSize)
}

func TestMigrationRemovingServerClassInMigrationMode(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	serverClassSize := 2
	clusterSize := serverClassSize * 2
	// Create the source cluster.
	srcCluster := clusterOptions().WithMixedEphemeralTopology(serverClassSize).MustCreate(t, kubernetes)

	// Pause the source cluster.
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Test("/status/controlPaused", true), time.Minute)

	dstCluster := clusterOptions().WithMixedEphemeralTopology(serverClassSize).Generate(kubernetes)
	dstCluster.Spec.Migration = &couchbasev2.ClusterAssimilationSpec{
		UnmanagedClusterHost: fmt.Sprintf("%s.%s.svc.cluster.local", srcCluster.Name, srcCluster.Namespace),
	}

	dstCluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, dstCluster)

	MustValidateClusterSize(t, kubernetes, dstCluster, clusterSize)

	// Remove a server class.
	dstCluster = e2eutil.MustPatchCluster(t, kubernetes, dstCluster, jsonpatch.NewPatchSet().Remove("/spec/servers/1"), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, dstCluster, e2eutil.RebalanceStartedEvent(dstCluster), 5*time.Minute)

	// Check that the cluster enteres a healthy state.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, dstCluster, 5*time.Minute)

	// Validate that there are now the correct number of nodes in the cluster.
	MustValidateClusterSize(t, kubernetes, dstCluster, clusterSize-serverClassSize)
}

func TestMigrationWaitsForIndexStorageModeToMatch(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	clusterSize := 1

	// Create the source cluster.
	srcCluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Pause the source cluster.
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Test("/status/controlPaused", true), time.Minute)

	// Initialise the destination cluster.
	dstCluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	dstCluster.Spec.Migration = &couchbasev2.ClusterAssimilationSpec{
		UnmanagedClusterHost: fmt.Sprintf("%s.%s.svc.cluster.local", srcCluster.Name, srcCluster.Namespace),
	}

	// Set the index storage mode of the destination cluster to a non-default value.
	dstCluster.Spec.ClusterSettings.Indexer = &couchbasev2.CouchbaseClusterIndexerSettings{
		StorageMode: couchbasev2.CouchbaseClusterIndexStorageSettingStandard,
	}

	dstCluster = e2eutil.CreateNewClusterFromSpec(t, kubernetes, dstCluster, -1)

	// Expect the cluster to enter an error state with the index storage mismatch message.
	e2eutil.MustWaitForClusterWithErrorMessage(t, kubernetes, "index storage mode mismatch", dstCluster, 5*time.Minute)

	// Update the index storage mode of the destination cluster to the default value which matches the source cluster. Ordinarily this would
	// be rejected by the DAC.
	dstCluster = e2eutil.MustPatchCluster(t, kubernetes, dstCluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/storageMode", couchbasev2.CouchbaseClusterIndexStorageSettingMemoryOptimized), time.Minute)

	// Check that the cluster is able to migrate successfully now that the index storage mode matches.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, dstCluster, 5*time.Minute)
	e2eutil.MustWaitForClusterConditionsRemoved(t, kubernetes, dstCluster, 5*time.Minute, couchbasev2.ClusterConditionError)
	e2eutil.MustBeUninitializedCluster(t, kubernetes, srcCluster)

	// Check that we've removed the migrating condition from the cluster.
	e2eutil.MustWaitForClusterConditionsRemoved(t, kubernetes, dstCluster, 5*time.Minute, couchbasev2.ClusterConditionMigrating)
}

func TestMigrationNotAllowedWithRollbackVersion(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	clusterSize := 3

	// Create the source cluster.
	srcCluster := clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Check that the cluster is healthy then start an upgrade.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, srcCluster, 2*time.Minute)
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)

	// When the upgrade starts, pause the source cluster, leaving it in mixed mode.
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, srcCluster, 5*time.Minute)
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Test("/status/controlPaused", true), 5*time.Minute)
	// Create the destination cluster with the original server version of the source cluster before it was upgraded.
	dstCluster := clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	dstCluster.Spec.Migration = &couchbasev2.ClusterAssimilationSpec{
		UnmanagedClusterHost: fmt.Sprintf("%s.%s.svc.cluster.local", srcCluster.Name, srcCluster.Namespace),
	}
	// Create the destination cluster.
	dstCluster = e2eutil.CreateNewClusterFromSpec(t, kubernetes, dstCluster, -1)

	// Check that the destination cluster enters an error state with a rollback version error message.
	e2eutil.MustWaitForClusterWithErrorMessage(t, kubernetes, "upgrades cannot be rolled back during migration", dstCluster, 5*time.Minute)
}

func GetMemberHostname(cluster *couchbasev2.CouchbaseCluster, nodeID int) string {
	return fmt.Sprintf("%s.%s.%s.svc", couchbaseutil.CreateMemberName(cluster.Name, nodeID), cluster.Name, cluster.Namespace)
}

func MustGetNumManagedNodes(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster) int {
	selector := labels.SelectorFromSet(labels.Set(k8sutil.LabelsForCluster(cluster)))

	pods, err := kubernetes.KubeClient.CoreV1().Pods(kubernetes.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector.String()})

	if err != nil {
		e2eutil.Die(t, err)
	}

	return len(pods.Items)
}

func MustValidateNumManagedNodes(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, expected int) {
	if actual := MustGetNumManagedNodes(t, kubernetes, cluster); actual != expected {
		e2eutil.Die(t, fmt.Errorf("expected %d managed nodes in the cluster, got %d", expected, actual))
	}
}

func MustValidateClusterSize(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, expected int) {
	if actual := e2eutil.MustGetClusterSize(t, kubernetes, cluster); actual != expected {
		e2eutil.Die(t, fmt.Errorf("expected %d nodes in the cluster, got %d", expected, actual))
	}
}

func MustGetPodIDsInServerGroup(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, serverGroup string) []int {
	selector := labels.SelectorFromSet(labels.Set(k8sutil.LabelsForCluster(cluster)))

	pods, err := kubernetes.KubeClient.CoreV1().Pods(kubernetes.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector.String()})

	if err != nil {
		e2eutil.Die(t, err)
	}

	nodeIds := []int{}

	for _, pod := range pods.Items {
		if sg, ok := pod.Spec.NodeSelector["topology.kubernetes.io/zone"]; ok && sg == serverGroup {
			id, err := couchbaseutil.GetIndexFromMemberName(pod.Name)
			if err != nil {
				e2eutil.Die(t, err)
			}

			nodeIds = append(nodeIds, id)
		}
	}

	return nodeIds
}
