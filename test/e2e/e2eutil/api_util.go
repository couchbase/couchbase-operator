/*
Copyright 2017-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2eutil

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

func GetCouchbaseCluster(crClient versioned.Interface, cl *couchbasev2.CouchbaseCluster) (*couchbasev2.CouchbaseCluster, error) {
	return crClient.CouchbaseV2().CouchbaseClusters(cl.Namespace).Get(context.Background(), cl.Name, metav1.GetOptions{})
}

func CreateCouchbaseCluster(crClient versioned.Interface, cl *couchbasev2.CouchbaseCluster) (*couchbasev2.CouchbaseCluster, error) {
	return crClient.CouchbaseV2().CouchbaseClusters(cl.Namespace).Create(context.Background(), cl, metav1.CreateOptions{})
}

func DeleteCouchbaseCluster(crClient versioned.Interface, cl *couchbasev2.CouchbaseCluster) error {
	return crClient.CouchbaseV2().CouchbaseClusters(cl.Namespace).Delete(context.Background(), cl.Name, metav1.DeleteOptions{})
}

func UpdateCouchbaseCluster(crClient versioned.Interface, cl *couchbasev2.CouchbaseCluster) (*couchbasev2.CouchbaseCluster, error) {
	return crClient.CouchbaseV2().CouchbaseClusters(cl.Namespace).Update(context.Background(), cl, metav1.UpdateOptions{})
}

// Gets events for a CouchbaseCluster and returns them sorted by time (oldest to newest).
func GetCouchbaseEvents(kubeCli kubernetes.Interface, couchbase *couchbasev2.CouchbaseCluster) (EventList, error) {
	selector := map[string]string{
		"involvedObject.apiVersion": "couchbase.com/v2",
		"involvedObject.kind":       "CouchbaseCluster",
		"involvedObject.name":       couchbase.Name,
	}

	list, err := kubeCli.CoreV1().Events(couchbase.Namespace).List(context.Background(), metav1.ListOptions{FieldSelector: labels.FormatLabels(selector)})
	if err != nil {
		return nil, err
	}

	events := EventList{}

	for _, item := range list.Items {
		events = append(events, item)
	}

	sort.Sort(events)

	return events, nil
}

func UntaintAll(k8s *types.Cluster) error {
	callback := func() error {
		nodes, err := k8s.KubeClient.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("failed to get node list: %w", err)
		}

		for i := range nodes.Items {
			node := &nodes.Items[i]

			node.Spec.Unschedulable = false
			node.Spec.Taints = nil

			if _, err := k8s.KubeClient.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{}); err != nil {
				return err
			}
		}

		return nil
	}

	return retryutil.RetryFor(time.Minute, callback)
}

func MustUntaintAll(t *testing.T, k8s *types.Cluster) {
	if err := UntaintAll(k8s); err != nil {
		Die(t, err)
	}
}

// MustTaintZoneNoSchedule taints a zone with a NoSchedule taint.
func MustTaintZoneNoSchedule(t *testing.T, k8s *types.Cluster, zone string) {
	MustTaintZone(t, k8s, zone, v1.TaintEffectNoSchedule)
}

// MustEvacuateZone cleans out an availability zone.
func MustEvacuateZone(t *testing.T, k8s *types.Cluster, zone string) {
	MustTaintZone(t, k8s, zone, v1.TaintEffectNoExecute)
}

// MustTaintZone taints a zone with a given effect.
func MustTaintZone(t *testing.T, k8s *types.Cluster, zone string, taintEffect v1.TaintEffect) {
	nodes, err := k8s.KubeClient.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		Die(t, err)
	}

	for _, n := range nodes.Items {
		if n.Labels[constants.FailureDomainZoneLabel] != zone {
			continue
		}

		// Reload the node, the statuses are liable to change as we kick stuff off.
		node, err := k8s.KubeClient.CoreV1().Nodes().Get(context.Background(), n.Name, metav1.GetOptions{})
		if err != nil {
			Die(t, err)
		}

		node.Spec.Taints = []v1.Taint{
			{
				Key:    "couchbase-qe",
				Value:  "rocks",
				Effect: taintEffect,
			},
		}

		if _, err = k8s.KubeClient.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{}); err != nil {
			Die(t, err)
		}
	}
}

// MustRollingUpgrade simulates a Kubernetes rolling upgrade.
func MustRollingUpgrade(t *testing.T, k8s *types.Cluster, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	nodes, err := k8s.KubeClient.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		Die(t, err)
	}

	for _, n := range nodes.Items {
		// Kick everything off the node immediately.
		node, err := k8s.KubeClient.CoreV1().Nodes().Get(context.Background(), n.Name, metav1.GetOptions{})
		if err != nil {
			Die(t, err)
		}

		node.Spec.Taints = []v1.Taint{
			{
				Key:    "couchbase-qe",
				Value:  "rocks",
				Effect: v1.TaintEffectNoExecute,
			},
		}

		if _, err = k8s.KubeClient.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{}); err != nil {
			Die(t, err)
		}

		// Wait for application controllers to recover.
		time.Sleep(30 * time.Second)

		// Wait for PDBs to allow eviction before scheduling the next death.
		callback := func() error {
			pdbs, err := k8s.KubeClient.PolicyV1().PodDisruptionBudgets(k8s.Namespace).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return err
			}

			for _, pdb := range pdbs.Items {
				if pdb.Status.CurrentHealthy <= pdb.Status.DesiredHealthy {
					return fmt.Errorf("unable to evict any pods, current %v <= desired %v", pdb.Status.CurrentHealthy, pdb.Status.DesiredHealthy)
				}
			}

			return nil
		}

		if err := retryutil.Retry(ctx, 10*time.Second, callback); err != nil {
			Die(t, err)
		}

		// Untaint the node.
		node, err = k8s.KubeClient.CoreV1().Nodes().Get(context.Background(), node.Name, metav1.GetOptions{})
		if err != nil {
			Die(t, err)
		}

		node.Spec.Taints = nil

		if _, err := k8s.KubeClient.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{}); err != nil {
			Die(t, err)
		}
	}
}

// validatePodReadiness checks a single pod carries the expected readiness condition.
func validatePodReadiness(pod *v1.Pod, status v1.ConditionStatus) error {
	for _, condition := range pod.Status.Conditions {
		if condition.Type != v1.PodReady {
			continue
		}

		if condition.Status != status {
			return fmt.Errorf("%s ready status %v not as expected %v", pod.Name, condition.Status, status)
		}

		return nil
	}

	return fmt.Errorf("%s ready status not set", pod.Name)
}

// MustValidatePodReadiness checks a pod has the the correct readiness condition.
func MustValidatePodReadiness(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, index int, status v1.ConditionStatus, timeout time.Duration) {
	name := couchbaseutil.CreateMemberName(cluster.Name, index)

	callback := func() error {
		pod, err := k8s.KubeClient.CoreV1().Pods(k8s.Namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		return validatePodReadiness(pod, status)
	}

	if err := retryutil.RetryFor(timeout, callback); err != nil {
		Die(t, err)
	}
}

// MustValidateAllPodReadiness checks the cluster has the expected number of pods, and
// that all of them are ready.
// We use this instead of calling MustValidatePodReadiness for each index from 0 to size-1.
// Member indexes are not always contiguous. If a member fails to come up, the Operator
// replaces it with one using the next index, so a three node cluster can end up with
// members 0000, 0002 and 0003. Looping over indexes then waits for pod 0001, which does
// not exist, until the timeout expires and the test fails for an unrelated reason.
func MustValidateAllPodReadiness(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, expected int, status v1.ConditionStatus, timeout time.Duration) {
	callback := func() error {
		pods, err := k8s.KubeClient.CoreV1().Pods(k8s.Namespace).List(context.Background(), ClusterListOpt(cluster))
		if err != nil {
			return err
		}

		if len(pods.Items) != expected {
			return fmt.Errorf("expected %d pods, got %d", expected, len(pods.Items))
		}

		for i := range pods.Items {
			if err := validatePodReadiness(&pods.Items[i], status); err != nil {
				return err
			}
		}

		return nil
	}

	if err := retryutil.RetryFor(timeout, callback); err != nil {
		Die(t, err)
	}
}

// GetNodeForPod returns a reference to the node a pod runs on.
func GetNodeForPod(k8s *types.Cluster, name string) (*v1.Node, error) {
	pod, err := k8s.KubeClient.CoreV1().Pods(k8s.Namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	nodes, err := k8s.KubeClient.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, node := range nodes.Items {
		if node.Status.Addresses[0].Address == pod.Status.HostIP {
			return &node, nil
		}
	}

	return nil, fmt.Errorf("node for pod not found")
}

func MustGetNodeForPod(t *testing.T, k8s *types.Cluster, name string) *v1.Node {
	node, err := GetNodeForPod(k8s, name)
	if err != nil {
		Die(t, err)
	}

	return node
}

func MustWaitForPodIPNotInServiceEndpointSlice(t *testing.T, k8s *types.Cluster, podName, svc string, timeout time.Duration) {
	if err := waitForPodServiceEndpointSliceReadyCondition(k8s, podName, svc, false, timeout); err != nil {
		Die(t, err)
	}
}

func MustWaitForPodIPInServiceEndpointSlice(t *testing.T, k8s *types.Cluster, podName, svc string, timeout time.Duration) {
	if err := waitForPodServiceEndpointSliceReadyCondition(k8s, podName, svc, true, timeout); err != nil {
		Die(t, err)
	}
}

func waitForPodServiceEndpointSliceReadyCondition(k8s *types.Cluster, podName, svc string, expectReady bool, timeout time.Duration) error {
	// Helper to clean up the inner address loop
	containsAddress := func(addresses []string, target string) bool {
		for _, a := range addresses {
			if a == target {
				return true
			}
		}
		return false
	}

	callback := func() error {
		pod, err := k8s.KubeClient.CoreV1().Pods(k8s.Namespace).Get(context.Background(), podName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		podIP := pod.Status.PodIP

		slices, err := k8s.KubeClient.DiscoveryV1().EndpointSlices(k8s.Namespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("kubernetes.io/service-name=%s", svc),
		})

		if err != nil {
			return fmt.Errorf("failed to list endpoint slices for DNS service: %w", err)
		}

		for _, slice := range slices.Items {
			for _, ep := range slice.Endpoints {
				if !containsAddress(ep.Addresses, podIP) {
					continue
				}

				if ep.Conditions.Ready == nil {
					return fmt.Errorf("pod %s found but Ready condition is nil", podIP)
				}

				if *ep.Conditions.Ready != expectReady {
					return fmt.Errorf("pod %s Ready condition is %t, want %t", podIP, *ep.Conditions.Ready, expectReady)
				}

				return nil // Pod found and is in the expected state.
			}
		}

		return fmt.Errorf("pod IP %s not found in any endpoint slice for service %s", podIP, svc)
	}

	return retryutil.RetryFor(timeout, callback)
}
