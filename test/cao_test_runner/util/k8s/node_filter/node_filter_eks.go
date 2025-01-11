package nodefilter

import (
	"fmt"
	"math"
	"slices"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/nodes"
)

type NodeFilterEKS struct {
	// NodeMap stores the information of the nodes. Maps node names to nodes.Node.
	NodeMap map[string]*nodes.Node
	// NodeNames stores the node names in sorted order (sorted using NodeSortByStrategy).
	// NodeNames slice is consistent with the NodeMap map.
	NodeNames []string
}

func ConfigNodeFilterEKS() *NodeFilterEKS {
	return &NodeFilterEKS{
		NodeMap:   make(map[string]*nodes.Node),
		NodeNames: make([]string, 0),
	}
}

// FilterNodesUsingStrategy filters and selects the nodes and returns the node names in sorted order.
/*
 * First we filter out the nodes based on the conditional parameters using filterNodesOnConditionalParameters().
 * Next, we sort the nodes using NodeSortByStrategy.
 * Finally, we select the nodes based on NodeSelectionStrategy strategy.
 */
func (n *NodeFilterEKS) FilterNodesUsingStrategy(nodeFilter *NodeFilter) ([]string, error) {
	var err error

	n.NodeMap, err = nodes.GetNodesMap(nil)
	if err != nil {
		return nil, fmt.Errorf("filter nodes `eks`: %w", err)
	}

	err = filterNodesOnConditionalParameters(nodeFilter, n.NodeMap)
	if err != nil {
		return nil, fmt.Errorf("filter nodes `eks`: %w", err)
	}

	n.NodeNames, err = sortNodesUsingSortByStrategy(nodeFilter.SortByStrategy, n.NodeMap)
	if err != nil {
		return nil, fmt.Errorf("filter nodes `eks`: %w", err)
	}

	if len(n.NodeNames) < nodeFilter.Count {
		return nil, fmt.Errorf("filter nodes `eks` with count=%d and filtered nodes=%d: %w", nodeFilter.Count, len(n.NodeNames), ErrInsufficientNodes)
	}

	switch nodeFilter.SelectStrategy {
	case SelectAny:
		return n.filterAny(nodeFilter)
	case SelectRoundRobinAZ:
		return n.eksFilterRoundRobinAZ(nodeFilter)
	default:
		return nil, fmt.Errorf("filter nodes `eks` using `%s`: %w", nodeFilter.SelectStrategy, ErrInvalidSelectStrategy)
	}
}

// filterAny filters the first NodeFilter.Count number of nodes from the slice of nodeNames.
func (n *NodeFilterEKS) filterAny(nodeFilter *NodeFilter) ([]string, error) {
	return n.NodeNames[0:nodeFilter.Count], nil
}

// eksFilterRoundRobinAZ filters the nodes on round-robin basis of the AZs.
/*
 * If the AZs are not provided, we find out all the AZs present, add to azList and sort them.
 * E.g. NodeFilter.AZList: [us-east-2a, us-east-2b, us-east-2c] and NodeFilter.Count=11 then distribution will be [4, 4, 3] in same order.
 * E.g. NodeFilter.AZList: [us-east-2c, us-east-2a, us-east-2b] and NodeFilter.Count=11 then distribution will be [4, 4, 3] in same order.
 * E.g. NodeFilter.AZList: nil and NodeFilter.Count=8 then distribution will be [2a=3, 2b=3, 2c=2]. All AZs were found then sorted and nodes were distributed.
 */
func (n *NodeFilterEKS) eksFilterRoundRobinAZ(nodeFilter *NodeFilter) ([]string, error) {
	var azList []string // If nodeFilter.AZList != nil then it stores them, else it stores all the AZs present.

	currAZMap := make(map[string]int) // Maps the AZ name to the total number of k8s node present in that AZ.
	reqAZMap := make(map[string]int)  // We calculate the number of nodes to have in each AZ (round-robin based) and store in this map.
	numAZs := len(nodeFilter.AZList)

	// If the AZs are not provided, we find out all the AZs present.
	if numAZs == 0 {
		for _, node := range n.NodeMap {
			tempAZ := node.Metadata.Labels[NodeZoneLabelKey]
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
	for _, node := range n.NodeMap {
		tempAZ := node.Metadata.Labels[NodeZoneLabelKey]
		if _, ok := reqAZMap[tempAZ]; ok && reqAZMap[tempAZ] > 0 {
			filteredNodes = append(filteredNodes, node.Metadata.Name)

			reqAZMap[tempAZ]--
		}
	}

	return filteredNodes, nil
}
