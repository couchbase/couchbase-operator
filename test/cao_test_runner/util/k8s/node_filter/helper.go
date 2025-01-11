package nodefilter

import (
	"fmt"
	"slices"

	caopods "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/cao_pods"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/nodes"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/pods"
)

// filterNodesOnConditionalParameters filters out the nodes based on all the conditional parameters.
/*
 * Conditional parameters: NodeFilter.SkipOperator, NodeFilter.SkipAdmission, NodeFilter.NodegroupNames and NodeFilter.AvoidLabels.
 * After this, NodeFilterEKS.NodeMap and NodeFilterEKS.NodeNames have eligible nodes - ready to be sorted using NodeSortByStrategy followed by selection using NodeSelectionStrategy.
 */
func filterNodesOnConditionalParameters(nodeFilter *NodeFilter, nodeMap map[string]*nodes.Node) error {
	var err error
	var operatorNodeName string
	var admissionNodeNames []string

	if nodeFilter.SkipOperator || nodeFilter.SkipAdmission {
		operatorNodeName, admissionNodeNames, err = GetOperatorAdmissionNodeNames(nodeFilter.Namespace)
		if err != nil {
			return fmt.Errorf("filter nodes on condition: %w", err)
		}
	}

	for nodeName := range nodeMap {
		if nodeFilter.SkipOperator && nodeName == operatorNodeName {
			delete(nodeMap, nodeName)
			continue
		}

		if nodeFilter.SkipAdmission && slices.Contains(admissionNodeNames, nodeName) {
			delete(nodeMap, nodeName)
			continue
		}

		// Checking if the node is of the required Nodegroup.
		if nodeFilter.NodegroupNames != nil && !slices.Contains(nodeFilter.NodegroupNames, nodeMap[nodeName].Metadata.Labels[EKSNodegroupLabelKey]) {
			delete(nodeMap, nodeName)
			continue
		}

		// If one of the labels in AvoidLabels is present on the node then we reject the node.
		for _, avoidLabel := range nodeFilter.AvoidLabels {
			if LabelExists(nodeMap[nodeName], avoidLabel[0], avoidLabel[1]) {
				delete(nodeMap, nodeName)
				break
			}
		}
	}

	return nil
}

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
