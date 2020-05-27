package e2eutil

import (
	"fmt"
	"sort"
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func GetCouchbaseCluster(crClient versioned.Interface, cl *couchbasev2.CouchbaseCluster) (*couchbasev2.CouchbaseCluster, error) {
	return crClient.CouchbaseV2().CouchbaseClusters(cl.Namespace).Get(cl.Name, metav1.GetOptions{})
}

// Gets events for a CouchbaseCluster and returns them sorted by time (oldest to newest).
func GetCouchbaseEvents(kubeCli kubernetes.Interface, couchbase *couchbasev2.CouchbaseCluster) (EventList, error) {
	list, err := kubeCli.CoreV1().Events(couchbase.Namespace).List(metav1.ListOptions{FieldSelector: "involvedObject.name=" + couchbase.Name})
	if err != nil {
		return nil, err
	}

	events := EventList{}

	for _, item := range list.Items {
		// Filter out events we have no control over
		if item.Reason == "FailedToUpdateEndpoint" {
			continue
		}

		events = append(events, item)
	}

	sort.Sort(events)

	return events, nil
}

// Updates K8S nodes with given Unschedulable and Taint values.
func SetNodeTaintAndSchedulableProperty(kubeClient kubernetes.Interface, isUnschedulable bool, podTaintList []v1.Taint, nodeIndex int) (err error) {
	for retryCount := 0; retryCount < 3; retryCount++ {
		k8sNodeList, err := kubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
		if err != nil {
			continue
		}

		nodeToTaint := k8sNodeList.Items[nodeIndex]
		nodeToTaint.Spec.Unschedulable = isUnschedulable
		nodeToTaint.Spec.Taints = podTaintList

		if _, err = kubeClient.CoreV1().Nodes().Update(&nodeToTaint); err == nil {
			break
		}
	}

	return err
}

// GetNodeForPod returns a reference to the node a pod runs on.
func GetNodeForPod(k8s *types.Cluster, name string) (*v1.Node, error) {
	pod, err := k8s.KubeClient.CoreV1().Pods(k8s.Namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	nodes, err := k8s.KubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
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

// MustGetAvailabiltyZoneForPod returns the availability zone a pod runs on.
func MustGetAvailabiltyZoneForPod(t *testing.T, k8s *types.Cluster, name string) string {
	node := MustGetNodeForPod(t, k8s, name)

	availaibiltyZone, ok := node.Labels[constants.FailureDomainZoneLabel]
	if !ok {
		Die(t, fmt.Errorf("node missing availability zone label"))
	}

	return availaibiltyZone
}

func MustEvacuateAvailabilityZone(t *testing.T, k8s *types.Cluster, zone string) func() {
	nodes, err := k8s.KubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		Die(t, err)
	}

	zoneNodes := []*v1.Node{}

	for index, node := range nodes.Items {
		nodeZone, ok := node.Labels[constants.FailureDomainZoneLabel]
		if !ok {
			Die(t, fmt.Errorf("node missing availability zone label"))
		}

		if nodeZone == zone {
			zoneNodes = append(zoneNodes, &nodes.Items[index])
		}
	}

	for _, node := range zoneNodes {
		node.Spec.Taints = []v1.Taint{
			{
				Key:    "NoExecute",
				Value:  "NoExecute",
				Effect: v1.TaintEffectNoExecute,
			},
		}

		node.Spec.Unschedulable = true

		if _, err := k8s.KubeClient.CoreV1().Nodes().Update(node); err != nil {
			Die(t, err)
		}
	}

	return func() {
		for _, node := range zoneNodes {
			newNode, err := k8s.KubeClient.CoreV1().Nodes().Get(node.Name, metav1.GetOptions{})
			if err != nil {
				Die(t, err)
			}

			newNode.Spec.Taints = []v1.Taint{}
			if _, err := k8s.KubeClient.CoreV1().Nodes().Update(newNode); err != nil {
				Die(t, err)
			}
		}
	}
}
