package nodefilter

import (
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/nodes"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/pods"
)

// GetOperatorAdmissionNodeNames retrieves the k8s node names which has the operator and admission pod respectively.
func GetOperatorAdmissionNodeNames(namespace string) (string, string, error) {
	podNames, err := pods.GetPodNames(namespace)
	if err != nil {
		return "", "", fmt.Errorf("get operator admission node names: %w", err)
	}

	// Get the nodes containing the Operator and Operator-Admission
	var operatorNodeName, admissionNodeName string

	for _, podName := range podNames {
		if strings.Contains(podName, "couchbase-operator-admission") {
			operatorNodeName, err = pods.GetNodeNameForPod(podName, namespace)
			if err != nil {
				return "", "", fmt.Errorf("get operator admission node names: %w", err)
			}

			continue
		}

		if strings.Contains(podName, "couchbase-operator") {
			admissionNodeName, err = pods.GetNodeNameForPod(podName, namespace)
			if err != nil {
				return "", "", fmt.Errorf("get operator admission node names: %w", err)
			}
		}
	}

	return operatorNodeName, admissionNodeName, nil
}

// LabelExists checks if the label (labelKeyName and labelValue both) is present on the node or not.
func LabelExists(node *nodes.Node, labelKeyName, labelValue string) bool {
	if value, ok := node.Metadata.Labels[labelKeyName]; ok {
		if value == labelValue {
			return true
		}
	}

	return false
}

// RemoveEmptyFromSlice removes empty strings from a string slice.
func RemoveEmptyFromSlice(oldSlice []string) []string {
	var newSlice []string

	for i := range oldSlice {
		if oldSlice[i] != "" {
			newSlice = append(newSlice, oldSlice[i])
		}
	}

	return newSlice
}

// GetNodesWithTimestamp returns the names of nodes with their creation timestamp.
// []{[]{"node-name", "creation-timestamp"}, ...}.
func GetNodesWithTimestamp() ([][]string, error) {
	var nodesWithTimestamp [][]string

	nodeList, err := nodes.GetNodes(nil)
	if err != nil {
		return nil, fmt.Errorf("get nodes with timestamp: %w", err)
	}

	for _, node := range nodeList.Nodes {
		temp := []string{node.Metadata.Name, node.Metadata.CreationTimestamp.String()}
		nodesWithTimestamp = append(nodesWithTimestamp, temp)
	}

	return nodesWithTimestamp, nil
}
