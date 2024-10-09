package nodefilter

import (
	"fmt"

	caopods "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/cao_pods"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/nodes"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/pods"
)

// GetOperatorAdmissionNodeNames retrieves the k8s node names which has the operator and admission pod respectively.
func GetOperatorAdmissionNodeNames(namespace string) (string, []string, error) {
	operatorPodName, admissionPodNames, err := caopods.GetOperatorAdmissionPodNames(namespace)
	if err != nil {
		return "", nil, fmt.Errorf("get operator admission node names: %w", err)
	}

	// Get the nodes containing the Operator and Operator-Admission
	var operatorNodeName string
	var admissionNodeNames []string

	operatorNodeName, err = pods.GetNodeNameForPod(operatorPodName, namespace)
	if err != nil {
		return "", nil, fmt.Errorf("get operator admission node names: %w", err)
	}

	for _, admissionPodName := range admissionPodNames {
		admissionNodeName, err := pods.GetNodeNameForPod(admissionPodName, namespace)
		if err != nil {
			return "", nil, fmt.Errorf("get operator admission node names: %w", err)
		}

		admissionNodeNames = append(admissionNodeNames, admissionNodeName)
	}

	return operatorNodeName, admissionNodeNames, nil
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
