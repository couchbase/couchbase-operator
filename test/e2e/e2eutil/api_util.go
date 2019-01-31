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

// Updates K8S nodes with given Unschedulable and Taint values
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

// Takes in pod name and returns the node index on which the pod is scheduled on
func GetTargetNodeIndexForPod(kubeClient kubernetes.Interface, namespace, podName string) (nodeIndex int, err error) {
	pod, err := kubeClient.CoreV1().Pods(namespace).Get(podName, metav1.GetOptions{})
	if err != nil {
		return
	}

	podSchedulledOnNode := pod.Status.HostIP
	nodeList, err := kubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return
	}
	for index, node := range nodeList.Items {
		if node.Status.Addresses[0].Address == podSchedulledOnNode {
			nodeIndex = index
			return
		}
	}
	return
}
