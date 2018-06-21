package e2eutil

import (
	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func GetCouchbaseCluster(crClient versioned.Interface, name, namespace string) (*api.CouchbaseCluster, error) {
	return crClient.Couchbase().CouchbaseClusters(namespace).Get(name, metav1.GetOptions{})
}

// Gets events for a CouchbaseCluster and returns them sorted by time (oldest to newest)
func GetCouchbaseEvents(kubeCli kubernetes.Interface, name, namespace string) (EventList, error) {
	list, err := kubeCli.Core().Events(namespace).List(metav1.ListOptions{FieldSelector: "involvedObject.name=" + name})
	if err != nil {
		return nil, err
	}

	events := EventList{}
	for _, item := range list.Items {
		events.AddEvent(item)
	}

	return events, nil
}

// returns quota for any node unless name is specified
// quota value is in MB
func GetK8SAllocatableMemory(kubeCli kubernetes.Interface, namespace string, name *string) (int, error) {

	// list or get node
	var node *v1.Node
	if name == nil {
		nodeList, err := kubeCli.CoreV1().Nodes().List(metav1.ListOptions{})
		if err != nil {
			return -1, err
		}
		nodeItems := nodeList.Items
		if len(nodeItems) == 0 {
			return -1, NewErrEmptyNodeList()
		}
		node = &nodeItems[0]
	} else {
		var err error
		node, err = kubeCli.CoreV1().Nodes().Get(*name, metav1.GetOptions{})
		if err != nil {
			return -1, err
		}
	}

	// get allocatable memory
	memQuantity := node.Status.Allocatable[v1.ResourceMemory]
	memQuantityMB := int(memQuantity.Value() >> 20)
	return memQuantityMB, nil
}

// Updates K8S nodes with given Unschedulable and Taint values
func SetNodeTaintAndSchedulableProperty(kubeClient kubernetes.Interface, isUnschedulable bool, podTaintList []v1.Taint, nodeIndex int) (err error) {
	for retryCount := 0; retryCount < 3; retryCount++ {
		k8sNodeList, err := kubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
		if err != nil {
			return err
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
