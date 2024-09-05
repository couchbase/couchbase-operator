package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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

	e2eutil.MustNewClusterFromSpec(t, kubernetes, dstCluster)

	// Check that all nodes from the initial cluster have been ejected
	e2eutil.MustBeUnitializedCluster(t, kubernetes, srcCluster)
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

	if managedNodes := MustGetNumManagedNodes(t, kubernetes, dstCluster); managedNodes+unmanagedNodes != clusterSize {
		e2eutil.Die(t, fmt.Errorf("expected %d managed nodes in the cluster, got %d", clusterSize-unmanagedNodes, managedNodes))
	}

	if actualSize := e2eutil.MustGetClusterSize(t, kubernetes, dstCluster); actualSize != clusterSize {
		e2eutil.Die(t, fmt.Errorf("expected %d nodes in the cluster, got %d", clusterSize, actualSize))
	}
}

func TestPremigrationNodes(t *testing.T) {
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

	if actualSize := e2eutil.MustGetClusterSize(t, kubernetes, dstCluster); actualSize != clusterSize+preMigrationSize {
		e2eutil.Die(t, fmt.Errorf("expected %d nodes in the cluster, got %d", clusterSize+preMigrationSize, actualSize))
	}
}

func MustGetNumManagedNodes(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster) int {
	selector := labels.SelectorFromSet(labels.Set(k8sutil.LabelsForCluster(cluster)))

	pods, err := kubernetes.KubeClient.CoreV1().Pods(kubernetes.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector.String()})

	if err != nil {
		e2eutil.Die(t, err)
	}

	return len(pods.Items)
}
