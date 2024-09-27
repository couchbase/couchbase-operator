package nodes

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

var (
	ErrNodeNameNotProvided = errors.New("node name is not provided")
	ErrNodeNotFound        = errors.New("node not found")
	ErrNoNodesFound        = errors.New("no nodes found")
)

func GetNodeNames() ([]string, error) {
	nodeNamesOutput, err := kubectl.Get("nodes").FormatOutput("name").Output()
	if err != nil {
		return nil, fmt.Errorf("get node names: %w", err)
	}

	if nodeNamesOutput == "" {
		return nil, fmt.Errorf("get node names: %w", ErrNoNodesFound)
	}

	nodeNames := strings.Split(nodeNamesOutput, "\n")
	for i := range nodeNames {
		// kubectl returns node names as node/node-name. We remove the prefix "node/"
		nodeNames[i] = strings.TrimPrefix(nodeNames[i], "node/")
	}

	return nodeNames, nil
}

// GetNode gets the node information of the node and returns *Node.
// Defined errors returned: ErrNodeNotFound.
func GetNode(nodeName string) (*Node, error) {
	if nodeName == "" {
		return nil, fmt.Errorf("get node: %w", ErrNodeNameNotProvided)
	}

	nodeJSON, stderr, err := kubectl.GetByTypeAndName("nodes", nodeName).FormatOutput("json").Exec(true, false)
	if err != nil {
		if strings.TrimSpace(stderr) == fmt.Sprintf("Error from server (NotFound): nodes \"%s\" not found", nodeName) {
			return nil, fmt.Errorf("get node: %w", ErrNodeNotFound)
		}

		return nil, fmt.Errorf("get node: %w", err)
	}

	var node Node

	err = json.Unmarshal([]byte(nodeJSON), &node)
	if err != nil {
		return nil, fmt.Errorf("get node: %w", err)
	}

	return &node, nil
}

// GetNodes gets the node information and returns the *NodeList containing the list of Node.
// If nodeNames = nil, then all the nodes are taken into account.
// Defined errors returned: ErrNodeNotFound.
func GetNodes(nodeNames []string) (*NodeList, error) {
	if nodeNames == nil {
		nodeList, err := GetNodeNames()
		if err != nil {
			return nil, fmt.Errorf("get nodes: %w", err)
		}

		nodeNames = nodeList
	}

	var nodeList NodeList

	// When we execute `get nodes <single-node>`, then we receive a single Node JSON instead of list of Node JSONs.
	if len(nodeNames) == 1 {
		node, err := GetNode(nodeNames[0])
		if err != nil {
			return nil, fmt.Errorf("get nodes: %w", err)
		}

		nodeList.Nodes = append(nodeList.Nodes, *node)

		return &nodeList, nil
	}

	nodesJSON, stderr, err := kubectl.GetByTypeAndName("nodes", nodeNames...).FormatOutput("json").Exec(true, false)
	if err != nil {
		if strings.Contains(stderr, "Error from server (NotFound)") {
			return nil, fmt.Errorf("get nodes: %w", ErrNodeNotFound)
		}

		return nil, fmt.Errorf("get nodes: %w", err)
	}

	err = json.Unmarshal([]byte(nodesJSON), &nodeList)
	if err != nil {
		return nil, fmt.Errorf("get nodes: json unmarshal: %w", err)
	}

	return &nodeList, nil
}

// GetNodesMap gets the node information and returns the map[string]*Node which has the *Node for each node names in given list.
// If nodeNames = nil, then all the nodes are taken into account.
func GetNodesMap(nodeNames []string) (map[string]*Node, error) {
	if nodeNames == nil {
		nodeList, err := GetNodeNames()
		if err != nil {
			return nil, fmt.Errorf("get nodes map: %w", err)
		}

		nodeNames = nodeList
	}

	nodeMap := make(map[string]*Node)

	nodesList, err := GetNodes(nodeNames)
	if err != nil {
		return nil, fmt.Errorf("get nodes map: %w", err)
	}

	for i := range nodesList.Nodes {
		nodeMap[nodeNames[i]] = &nodesList.Nodes[i]
	}

	return nodeMap, nil
}
