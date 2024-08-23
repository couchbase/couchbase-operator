package pods

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

var (
	ErrPodNameNotProvided   = errors.New("pod name is not provided")
	ErrNamespaceNotProvided = errors.New("namespace is not provided")
)

func GetPodNames(namespace string) ([]string, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get pod names: %w", ErrNamespaceNotProvided)
	}

	podNamesOutput, err := kubectl.Get("pods").FormatOutput("name").InNamespace(namespace).Output()
	if err != nil {
		return nil, fmt.Errorf("get pod names: %w", err)
	}

	podNames := strings.Split(podNamesOutput, "\n")
	for i := range podNames {
		// kubectl returns pod names as pod/pod-name. We remove the prefix "pod/"
		podNames[i] = strings.TrimPrefix(podNames[i], "pod/")
	}

	return podNames, nil
}

// GetPod gets the pod information of the pod in the given namespace and returns *Pod.
func GetPod(podName string, namespace string) (*Pod, error) {
	if podName == "" {
		return nil, fmt.Errorf("get pod: %w", ErrPodNameNotProvided)
	}

	if namespace == "" {
		return nil, fmt.Errorf("get pod: %w", ErrNamespaceNotProvided)
	}

	podJSON, err := kubectl.GetByTypeAndName("pods", podName).FormatOutput("json").InNamespace(namespace).Output()
	if err != nil {
		return nil, fmt.Errorf("get pod: %w", err)
	}

	var pod Pod

	err = json.Unmarshal([]byte(podJSON), &pod)
	if err != nil {
		return nil, fmt.Errorf("get pod: %w", err)
	}

	return &pod, nil
}

// GetPods gets the pod information and returns the *PodList containing the list of Pods.
// If podNames = nil, then all the pods in the namespace are taken into account.
func GetPods(podNames []string, namespace string) (*PodList, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get pods: %w", ErrNamespaceNotProvided)
	}

	if podNames == nil {
		podNamesList, err := GetPodNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get pods: %w", err)
		}

		podNames = podNamesList
	}

	var podList PodList

	// When we execute `get pods <single-pod>`, then we receive a single Pod JSON instead of list of Pod JSONs.
	if len(podNames) == 1 {
		pod, err := GetPod(podNames[0], namespace)
		if err != nil {
			return nil, fmt.Errorf("get pods: %w", err)
		}

		podList.Pods = append(podList.Pods, *pod)

		return &podList, nil
	}

	podsJSON, err := kubectl.GetByTypeAndName("pods", podNames...).FormatOutput("json").InNamespace(namespace).Output()
	if err != nil {
		return nil, fmt.Errorf("get pods: %w", err)
	}

	err = json.Unmarshal([]byte(podsJSON), &podList)
	if err != nil {
		return nil, fmt.Errorf("get pods: json unmarshal: %w", err)
	}

	return &podList, nil
}

// GetPodsMap gets the pod information and returns the map[string]*Pod which has the *Pod for each pod names in given list.
// If podNames = nil, then all the pods in the namespace are taken into account.
func GetPodsMap(podNames []string, namespace string) (map[string]*Pod, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get pods map: %w", ErrNamespaceNotProvided)
	}

	if podNames == nil {
		podNamesList, err := GetPodNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get pods map: %w", err)
		}

		podNames = podNamesList
	}

	podMap := make(map[string]*Pod)

	for _, podName := range podNames {
		pod, err := GetPod(podName, namespace)
		if err != nil {
			return nil, fmt.Errorf("get pods map: %w", err)
		}

		podMap[podName] = pod
	}

	return podMap, nil
}

func GetNodeNameForPod(podName, namespace string) (string, error) {
	pod, err := GetPod(podName, namespace)
	if err != nil {
		return "", fmt.Errorf("get node name for pod: %w", err)
	}

	return pod.Spec.NodeName, nil
}
