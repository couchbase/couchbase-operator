package k8sinfo

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/kubectl"
)

var (
	ErrDecodeError = errors.New("unable to decode")
)

// GetK8sNodeInformation gets the current information in json of the k8s node and updates the nodesMap.
// nodesMap is stored in context, will be available to the child trees.
func GetK8sNodeInformation(nodeName string, nodesMap map[string]string) (map[string]string, error) {
	output, err := kubectl.Get("node", nodeName).InNamespace("default").FormatOutput("json").Output()
	if err != nil {
		return nodesMap, fmt.Errorf("get k8s node information: %w", err)
	}

	nodesMap[nodeName] = output

	return nodesMap, nil
}

// GetK8sPodInformation gets the current information in json of the k8s pod and updates the podsMap.
// podsMap is stored in context, will be available to the child trees.
func GetK8sPodInformation(podName string, podsMap map[string]string) (map[string]string, error) {
	output, err := kubectl.Get("pod", podName).InNamespace("default").FormatOutput("json").Output()
	if err != nil {
		return podsMap, fmt.Errorf("get k8s pod information: %w", err)
	}

	podsMap[podName] = output

	return podsMap, nil
}

// GetK8sNodesInfo gets information of all the nodes and returns map[nodeName]nodeInfoJsonString and list of node names.
// The nodeInfoJsonString is a string hence unmarshalling is required to use JSON Pointer.
func GetK8sNodesInfo() (map[string]string, []string, error) {
	output, err := kubectl.Get("nodes").InNamespace("default").FormatOutput("json").Output()
	if err != nil {
		return nil, nil, fmt.Errorf("get k8s nodes information: %w", err)
	}

	// Unmarshal JSON into the map variable
	var jsonOutput map[string]interface{}

	err = json.Unmarshal([]byte(output), &jsonOutput)
	if err != nil {
		return nil, nil, fmt.Errorf("parse json of k8s nodes information: %w", err)
	}

	var k8sNodeNames []string

	totalNodes := int64(0)
	k8sNodesMap := make(map[string]string)

	// Get all the node names present in the k8s cluster
	for {
		// Path to get the name of the k8s node
		path := fmt.Sprintf("/items/%d/metadata/name", totalNodes)

		value, err := jsonpatch.Get(&jsonOutput, path)
		if err != nil {
			if errors.Is(err, jsonpatch.ErrPathNotFoundInJSON) {
				break
			}

			return nil, nil, err
		}

		nodeName, ok := value.(string)
		if !ok {
			return nil, nil, fmt.Errorf("decode node name: %w", ErrDecodeError)
		}

		k8sNodesMap, err = GetK8sNodeInformation(nodeName, k8sNodesMap)
		if err != nil {
			return nil, nil, err
		}

		k8sNodeNames = append(k8sNodeNames, nodeName)
		totalNodes++
	}

	return k8sNodesMap, k8sNodeNames, nil
}

// GetK8sPodsInfo gets information of all the pods in a given namespace and returns map[podName]podInfoJsonString and
// list of pod names.
// The podInfoJsonString is a string hence unmarshalling is required to use JSON Pointer.
func GetK8sPodsInfo(namespace string) (map[string]string, []string, error) {
	output, err := kubectl.Get("pods").InNamespace(namespace).FormatOutput("json").Output()
	if err != nil {
		return nil, nil, fmt.Errorf("get k8s pods information: %w", err)
	}

	// Unmarshal JSON into the map variable
	var jsonOutput map[string]interface{}

	err = json.Unmarshal([]byte(output), &jsonOutput)
	if err != nil {
		return nil, nil, fmt.Errorf("parse json of k8s pods information: %w", err)
	}

	var k8sPodNames []string

	totalPods := int64(0)
	k8sPodsMap := make(map[string]string)

	// Get all the pod names present in the k8s cluster
	for {
		// Path to get the name of the k8s node
		path := fmt.Sprintf("/items/%d/metadata/name", totalPods)

		value, err := jsonpatch.Get(&jsonOutput, path)
		if err != nil {
			if errors.Is(err, jsonpatch.ErrPathNotFoundInJSON) {
				break
			}

			return nil, nil, fmt.Errorf("get pods json: %w", err)
		}

		podName, ok := value.(string)
		if !ok {
			return nil, nil, fmt.Errorf("decode pod name: %w", ErrDecodeError)
		}

		k8sPodNames = append(k8sPodNames, podName)
		totalPods++

		k8sPodsMap, err = GetK8sPodInformation(podName, k8sPodsMap)
		if err != nil {
			return nil, nil, err
		}
	}

	return k8sPodsMap, k8sPodNames, nil
}

func GetNodeNameForPod(podInfo string) (string, error) {
	var jsonOutput map[string]interface{}

	err := json.Unmarshal([]byte(podInfo), &jsonOutput)
	if err != nil {
		return "", fmt.Errorf("parse json of k8s pods information: %w", err)
	}

	value, err := jsonpatch.Get(&jsonOutput, "/spec/nodeName")
	if err != nil {
		if !errors.Is(err, jsonpatch.ErrPathNotFoundInJSON) {
			return "", fmt.Errorf("get pods json: %w", err)
		}
	}

	nodeName, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("decode pod name: %w", ErrDecodeError)
	}

	return nodeName, nil
}
