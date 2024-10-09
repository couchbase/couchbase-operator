package nodefilter

import (
	"fmt"
	"math"
	"slices"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/nodes"
)

type NodeFilterEKS struct {
}

func ConfigNodeFilterEKS() *NodeFilterEKS {
	return &NodeFilterEKS{}
}

// SortNodesUsingSortByStrategy sorts the nodes based on NodeSortByStrategy.
func (n *NodeFilterEKS) SortNodesUsingSortByStrategy(nodeFilter *NodeFilter) ([]string, error) {
	nodeNames, err := sortNodesUsingSortByStrategy(nodeFilter.SortByStrategy)
	if err != nil {
		return nil, fmt.Errorf("sort node `eks`: %w", err)
	}

	return nodeNames, nil
}

// FilterOutNodes first runs SortNodesUsingSortByStrategy() and then filters out nodes based on
// NodeFilter.SkipOperator, NodeFilter.SkipAdmission and NodeFilter.AvoidLabels.
func (n *NodeFilterEKS) FilterOutNodes(nodeFilter *NodeFilter) ([]string, error) {
	nodeNames, err := sortNodesUsingSortByStrategy(nodeFilter.SortByStrategy)
	if err != nil {
		return nil, fmt.Errorf("filter out nodes `eks`: %w", err)
	}

	operatorNodeName, admissionNodeNames, err := GetOperatorAdmissionNodeNames("default")
	if err != nil {
		return nil, fmt.Errorf("filter out nodes `eks`: %w", err)
	}

	for i := range nodeNames {
		if nodeFilter.SkipOperator && nodeNames[i] == operatorNodeName {
			nodeNames[i] = ""
			continue
		}

		if nodeFilter.SkipAdmission && slices.Contains(admissionNodeNames, nodeNames[i]) {
			nodeNames[i] = ""
			continue
		}

		// If one of the labels in AvoidLabels is present on the node then we reject the node.
		for _, avoidLabel := range nodeFilter.AvoidLabels {
			node, err := nodes.GetNode(nodeNames[i])
			if err != nil {
				return nil, fmt.Errorf("filter out nodes `eks`: %w", err)
			}

			if LabelExists(node, avoidLabel[0], avoidLabel[1]) {
				nodeNames[i] = ""
				break
			}
		}
	}

	nodeNames = RemoveEmptyFromSlice(nodeNames)

	return nodeNames, nil
}

// FilterNodesUsingStrategy first runs FilterOutNodes() and then selects nodes based on NodeSelectionStrategy strategy.
func (n *NodeFilterEKS) FilterNodesUsingStrategy(nodeFilter *NodeFilter) ([]string, error) {
	nodeNames, err := n.FilterOutNodes(nodeFilter)
	if err != nil {
		return nil, fmt.Errorf("filter nodes `eks`: %w", err)
	}

	switch nodeFilter.SelectStrategy {
	case SelectAny:
		return filterAny(nodeFilter, nodeNames)
	case SelectRoundRobinAZ:
		return eksFilterRoundRobinAZ(nodeFilter, nodeNames)
	default:
		return nil, fmt.Errorf("filter out nodes `eks` using `%s`: %w", nodeFilter.SelectStrategy, ErrInvalidSelectStrategy)
	}
}

// filterAny filters the first NodeFilter.Count number of nodes from the slice of nodeNames.
func filterAny(nodeFilter *NodeFilter, nodeNames []string) ([]string, error) {
	if len(nodeNames) < nodeFilter.Count {
		return nil, fmt.Errorf("filter any `eks` with count=%d and filtered nodes=%d: %w", nodeFilter.Count, len(nodeNames), ErrInsufficientNodes)
	}

	return nodeNames[0:nodeFilter.Count], nil
}

// eksFilterRoundRobinAZ filters the nodes on round-robin basis of the AZs.
/*
 * If the AZs are not provided, we find out all the AZs present, add to azList and sort them.
 * E.g. NodeFilter.AZList: [us-east-2a, us-east-2b, us-east-2c] and NodeFilter.Count=11 then distribution will be [4, 4, 3] in same order.
 * E.g. NodeFilter.AZList: [us-east-2c, us-east-2a, us-east-2b] and NodeFilter.Count=11 then distribution will be [4, 4, 3] in same order.
 * E.g. NodeFilter.AZList: nil and NodeFilter.Count=8 then distribution will be [2a=3, 2b=3, 2c=2]. All AZs were found then sorted and nodes were distributed.
 */
func eksFilterRoundRobinAZ(nodeFilter *NodeFilter, nodeNames []string) ([]string, error) {
	if len(nodeNames) < nodeFilter.Count {
		return nil, fmt.Errorf("filter any with count=%d and filtered nodes=%d: %w", nodeFilter.Count, len(nodeNames), ErrInsufficientNodes)
	}

	var azList []string // If nodeFilter.AZList != nil then it stores them, else it stores all the AZs present.

	currAZMap := make(map[string]int) // Maps the AZ name to the total number of k8s node present in that AZ.
	reqAZMap := make(map[string]int)  // We calculate the number of nodes to have in each AZ (round-robin based) and store in this map.
	numAZs := len(nodeFilter.AZList)

	nodeList, err := nodes.GetNodes(nodeNames)
	if err != nil {
		return nil, err
	}

	// If the AZs are not provided, we find out all the AZs present.
	if numAZs == 0 {
		for _, node := range nodeList.Nodes {
			tempAZ := node.Metadata.Labels["topology.kubernetes.io/zone"]
			currAZMap[tempAZ]++

			reqAZMap[tempAZ] = 0
		}

		for key := range reqAZMap {
			azList = append(azList, key)
			numAZs++
		}

		slices.Sort(azList)
	} else {
		azList = nodeFilter.AZList

		for i := range azList {
			reqAZMap[azList[i]] = 0
		}

		numAZs = len(reqAZMap)
	}

	// Populating reqAZMap with the number of nodes from each AZ that shall be filtered based on Round Robin selection.
	tempCount := nodeFilter.Count

	for _, az := range azList {
		reqAZMap[az] = int(math.Ceil(float64(tempCount) / float64(numAZs)))
		tempCount -= reqAZMap[az]
		numAZs--
	}

	// Validating if we have enough nodes in the required AZs
	for key, val := range reqAZMap {
		if currAZMap[key] < val {
			return nil, fmt.Errorf("az `%s`=%d is not enough required=%d: %w", key, currAZMap[key], val, ErrInsufficientNodes)
		}
	}

	var filteredNodes []string

	// Getting the list of nodes
	for _, node := range nodeList.Nodes {
		tempAZ := node.Metadata.Labels["topology.kubernetes.io/zone"]
		if _, ok := reqAZMap[tempAZ]; ok && reqAZMap[tempAZ] > 0 {
			filteredNodes = append(filteredNodes, node.Metadata.Name)

			reqAZMap[tempAZ]--
		}
	}

	return filteredNodes, nil
}
