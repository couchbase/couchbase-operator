package cbpods

import (
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/pods"
)

var (
	ErrNamespaceNotProvided = errors.New("namespace not provided")
	ErrNoCBPodsFound        = errors.New("zero cb pods found in the namespace")
)

// GetCBPodNames retrieves the names of all the cb pods in a given namespace.
func GetCBPodNames(namespace string) ([]string, error) {
	var cbPodNames []string

	if namespace == "" {
		return nil, fmt.Errorf("get cb pods: %w", ErrNamespaceNotProvided)
	}

	podList, err := pods.GetPods(nil, namespace)
	if err != nil {
		return nil, fmt.Errorf("get cb pods: %w", err)
	}

	for _, pod := range podList.Pods {
		if value, ok := pod.Metadata.Labels["couchbase_server"]; ok && value == "true" {
			cbPodNames = append(cbPodNames, pod.Metadata.Name)
		}
	}

	if len(cbPodNames) == 0 {
		return nil, fmt.Errorf("get cb pod names in namespace %s: %w", namespace, ErrNoCBPodsFound)
	}

	return cbPodNames, nil
}

// GetCBPods retrieves kubectl pod information of all the cb pods in a given namespace.
func GetCBPods(namespace string) ([]*pods.Pod, error) {
	var cbPods []*pods.Pod

	if namespace == "" {
		return nil, fmt.Errorf("get cb pods: %w", ErrNamespaceNotProvided)
	}

	podList, err := pods.GetPods(nil, namespace)
	if err != nil {
		return nil, fmt.Errorf("get cb pods: %w", err)
	}

	for i, pod := range podList.Pods {
		if value, ok := pod.Metadata.Labels["couchbase_server"]; ok && value == "true" {
			cbPods = append(cbPods, &podList.Pods[i])
		}
	}

	if len(cbPods) == 0 {
		return nil, fmt.Errorf("get cb pods in namespace %s: %w", namespace, ErrNoCBPodsFound)
	}

	return cbPods, nil
}

// GetCBPodsMap retrieves kubectl pod information of all the cb pods in a given namespace and returns a map with key as pod name.
func GetCBPodsMap(namespace string) (map[string]*pods.Pod, error) {
	cbPodsMap := make(map[string]*pods.Pod)

	if namespace == "" {
		return nil, fmt.Errorf("get cb pods map: %w", ErrNamespaceNotProvided)
	}

	podList, err := pods.GetPods(nil, namespace)
	if err != nil {
		return nil, fmt.Errorf("get cb pods map: %w", err)
	}

	for i, pod := range podList.Pods {
		if value, ok := pod.Metadata.Labels["couchbase_server"]; ok && value == "true" {
			cbPodsMap[pod.Metadata.Name] = &podList.Pods[i]
		}
	}

	if len(cbPodsMap) == 0 {
		return nil, fmt.Errorf("get cb pods map in namespace %s: %w", namespace, ErrNoCBPodsFound)
	}

	return cbPodsMap, nil
}

// GetCBPodHostname returns the k8s hostname for the CB pod.
func GetCBPodHostname(cbPodName, namespace string) string {
	// pod=cb-example-0001 in namespace=default and cluster=cb-example has hostname=cb-example-0001.cb-example.default.svc.
	return fmt.Sprintf("%s.%s.%s.svc", cbPodName, cbPodName[0:len(cbPodName)-5], namespace)
}
