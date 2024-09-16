package e2e

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	v2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPodDisruptionBudgets(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	clusterSize := 3

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	// Check the cluster has one correctly named PDB.
	pdbs, err := kubernetes.KubeClient.PolicyV1().PodDisruptionBudgets(cluster.Namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	if len(pdbs.Items) > 1 {
		e2eutil.Die(t, fmt.Errorf("cluster should only contain one pdb"))
	}

	for _, pdb := range pdbs.Items {
		if !strings.EqualFold(pdb.Name, cluster.Name+"-pdb") {
			e2eutil.Die(t, fmt.Errorf("pdb has incorrect name"))
		}
	}

	// Update the cluster to use per-serviceclass PDBs.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/perServiceClassPDB", true), 1*time.Minute)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	// Check the cluster has the correct PDBs.

	services := []v2.Service{}

	for _, serverClass := range cluster.Spec.Servers {
		for _, service := range serverClass.Services {
			if !slices.Contains(services, service) {
				services = append(services, service)
			}
		}
	}

	// Need to have a nap here as the PDBs take a while to spin up.
	// It's not recorded as a cluster event so e2eutil is a bit useless here.
	time.Sleep(5 * time.Minute)

	perServiceClassPdbs, err := kubernetes.KubeClient.PolicyV1().PodDisruptionBudgets(cluster.Namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	if len(perServiceClassPdbs.Items) != len(cluster.Spec.Servers) {
		e2eutil.Die(t, fmt.Errorf("incorrect number of pdbs"))
	}

	for _, pdb := range perServiceClassPdbs.Items {
		found := false

		for _, serverClass := range cluster.Spec.Servers {
			if strings.EqualFold(pdb.Name, cluster.Name+"-"+serverClass.Name+"-pdb") {
				found = true
				break
			}
		}

		if !found {
			e2eutil.Die(t, fmt.Errorf("expected pdb not found"))
		}
	}
}
