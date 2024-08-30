package caopods

import (
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/pods"
)

var (
	ErrAdmissionPodDoesntExist = errors.New("admission controller pod does not exist")
	ErrOperatorPodDoesntExist  = errors.New("operator pod does not exist")
	ErrNamespaceNotProvided    = errors.New("namespace not provided")
	ErrPodsListEmpty           = errors.New("pods list empty")
)

// GetOperatorAdmissionPodNames retrieves the k8s pod names for operator and admission pod respectively.
func GetOperatorAdmissionPodNames(namespace string) (string, string, error) {
	if namespace == "" {
		return "", "", fmt.Errorf("get operator admission pod names: %w", ErrNamespaceNotProvided)
	}

	podNames, err := pods.GetPodNames(namespace)
	if err != nil {
		return "", "", fmt.Errorf("get operator admission pod names: %w", err)
	}

	var operatorPodName, admissionPodName string

	for _, podName := range podNames {
		if strings.Contains(podName, "couchbase-operator-admission") {
			admissionPodName = podName
			continue
		}

		if strings.Contains(podName, "couchbase-operator") {
			operatorPodName = podName
		}
	}

	if operatorPodName == "" {
		return "", "", fmt.Errorf("get operator admission pod names: %w", ErrOperatorPodDoesntExist)
	}

	if admissionPodName == "" {
		return "", "", fmt.Errorf("get operator admission pod names: %w", ErrAdmissionPodDoesntExist)
	}

	return operatorPodName, admissionPodName, nil
}

// GetOperatorPod retrieves kubectl pod information of the operator pod in a given namespace.
func GetOperatorPod(namespace string) (*pods.Pod, error) {
	var operatorPod *pods.Pod

	if namespace == "" {
		return nil, fmt.Errorf("get operator pod: %w", ErrNamespaceNotProvided)
	}

	operatorPodName, _, err := GetOperatorAdmissionPodNames(namespace)
	if err != nil {
		return nil, fmt.Errorf("get operator pod: %w", err)
	}

	podList, err := pods.GetPods([]string{operatorPodName}, namespace)
	if err != nil {
		return nil, fmt.Errorf("get operator pod: %w", err)
	}

	if len(podList.Pods) == 0 {
		return nil, fmt.Errorf("get operator pod: %w", ErrPodsListEmpty)
	}

	operatorPod = &podList.Pods[0]

	return operatorPod, nil
}

// GetAdmissionPod retrieves kubectl pod information of the admission controller pod in a given namespace.
func GetAdmissionPod(namespace string) (*pods.Pod, error) {
	var admissionPod *pods.Pod

	if namespace == "" {
		return nil, fmt.Errorf("get admission pod: %w", ErrNamespaceNotProvided)
	}

	_, admissionPodName, err := GetOperatorAdmissionPodNames(namespace)
	if err != nil {
		return nil, fmt.Errorf("get admission pod: %w", err)
	}

	podList, err := pods.GetPods([]string{admissionPodName}, namespace)
	if err != nil {
		return nil, fmt.Errorf("get admission pod: %w", err)
	}

	if len(podList.Pods) == 0 {
		return nil, fmt.Errorf("get admission pod: %w", ErrPodsListEmpty)
	}

	admissionPod = &podList.Pods[0]

	return admissionPod, nil
}

// GetCAOPodsMap retrieves kubectl pod information of all the cao pods in a given namespace and returns a map with key as pod name.
// TODO : Add operator-backup and all other operator pods into this.
func GetCAOPodsMap(namespace string) (map[string]*pods.Pod, error) {
	caoPodsMap := make(map[string]*pods.Pod)

	if namespace == "" {
		return nil, fmt.Errorf("get cao pods map: %w", ErrNamespaceNotProvided)
	}

	operatorPodName, admissionPodName, err := GetOperatorAdmissionPodNames(namespace)
	if err != nil {
		return nil, fmt.Errorf("get cao pods map: %w", err)
	}

	operatorPod, err := GetOperatorPod(namespace)
	if err != nil {
		return nil, fmt.Errorf("get cao pods map: %w", err)
	}

	admissionPod, err := GetAdmissionPod(namespace)
	if err != nil {
		return nil, fmt.Errorf("get cao pods map: %w", err)
	}

	// TODO : Add operator-backup and all other operator pods into this.

	caoPodsMap[operatorPodName] = operatorPod
	caoPodsMap[admissionPodName] = admissionPod

	return caoPodsMap, nil
}
