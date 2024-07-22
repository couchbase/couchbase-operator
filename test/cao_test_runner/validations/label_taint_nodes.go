package validations

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/jsonpatch"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8sinfo"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/kubectl"
	"github.com/sirupsen/logrus"
)

const (
	k8sNodeLabelForCBNode          = "couchbase-node"
	k8sNodeLabelForGenericWorkload = "workload-node"
	defaultLabelValue              = "true"
	taintNoSchedule                = "NoSchedule"
)

var (
	ErrDecodeError = errors.New("unable to decode")
	ErrNumOfNodes  = errors.New("number of nodes in k8s cluster is less than provided nodes")
)

type LabelTaintNodes struct {
	Name             string `yaml:"name" caoCli:"required"`
	State            string `yaml:"state" caoCli:"required"`
	NumCBNodes       int64  `yaml:"numCBNodes" caoCli:"required"`
	NumWorkloadNodes int64  `yaml:"numWorkloadNodes" caoCli:"required"`
	ApplyTaint       bool   `yaml:"applyTaint" caoCli:"required"`
	ApplyLabel       bool   `yaml:"applyLabel" caoCli:"required"`
	RemoveTaint      bool   `yaml:"removeTaint"`
	RemoveLabel      bool   `yaml:"removeLabel"`
}

func (ltn *LabelTaintNodes) Run(ctxt *context.Context) error {
	defer handlePanic()

	k8sNodesMap, k8sNodeNames, err := k8sinfo.GetK8sNodesInfo()
	if err != nil {
		return fmt.Errorf("get nodes information: %w", err)
	}

	totalNodes := int64(len(k8sNodeNames))

	k8sPodsMap, k8sPodNames, err := k8sinfo.GetK8sPodsInfo("default")
	if err != nil {
		return fmt.Errorf("get pods information: %w", err)
	}

	// Adding the pods and nodes map to context
	ctxt.WithIDInterface(context.K8sNodesMapKey, k8sNodesMap)
	ctxt.WithIDInterface(context.K8sPodsMapKey, k8sPodsMap)

	// Get the nodes containing the Operator and Operator-Admission
	var operatorNodeName, admissionNodeName string

	for _, podName := range k8sPodNames {
		if strings.Contains(podName, "couchbase-operator-admission") {
			operatorNodeName, err = k8sinfo.GetNodeNameForPod(k8sPodsMap[podName])
			if err != nil {
				return fmt.Errorf("get node name for pod: %w", err)
			}

			continue
		}

		if strings.Contains(podName, "couchbase-operator") {
			admissionNodeName, err = k8sinfo.GetNodeNameForPod(k8sPodsMap[podName])
			if err != nil {
				return fmt.Errorf("get node name for pod: %w", err)
			}
		}
	}

	// Apply Labels or Taints to K8S Nodes
	if ltn.ApplyTaint || ltn.ApplyLabel {
		logrus.Info("Apply label and taint to K8s nodes started")

		// Apply the couchbase label and taint to all the desired nodes
		cbNodes := ltn.NumCBNodes
		if ltn.NumCBNodes+2 < totalNodes {
			for i := int64(0); i < cbNodes; i++ {
				// Not applying taints or labels to nodes with Operator or Operator-Admission
				if k8sNodeNames[i] == operatorNodeName || k8sNodeNames[i] == admissionNodeName {
					cbNodes++
					continue
				}

				if ltn.ApplyLabel {
					err = kubectl.Label(k8sNodeNames[i], k8sNodeLabelForCBNode, defaultLabelValue).ExecWithoutOutputCapture()
					if err != nil {
						return fmt.Errorf("apply label to couchbase node: %w", err)
					}
				}

				if ltn.ApplyTaint {
					err = kubectl.Taint(k8sNodeNames[i], k8sNodeLabelForCBNode, defaultLabelValue, taintNoSchedule).ExecWithoutOutputCapture()
					if err != nil {
						return fmt.Errorf("apply taint to couchbase node: %w", err)
					}
				}
			}
		} else {
			return fmt.Errorf("unable to apply label and taint with total nodes = %d and cb nodes = %d: %w",
				totalNodes, ltn.NumCBNodes, ErrNumOfNodes)
		}

		// Apply the workload label and taint to all the desired nodes
		if ltn.NumWorkloadNodes <= totalNodes-ltn.NumCBNodes {
			for i := ltn.NumCBNodes; i < ltn.NumCBNodes+ltn.NumWorkloadNodes; i++ {
				if ltn.ApplyLabel {
					err = kubectl.Label(k8sNodeNames[i], k8sNodeLabelForGenericWorkload, defaultLabelValue).ExecWithoutOutputCapture()
					if err != nil {
						return fmt.Errorf("apply label to workload node: %w", err)
					}
				}

				if ltn.ApplyTaint {
					err = kubectl.Taint(k8sNodeNames[i], k8sNodeLabelForGenericWorkload, defaultLabelValue, taintNoSchedule).ExecWithoutOutputCapture()
					if err != nil {
						return fmt.Errorf("apply taint to workload node: %w", err)
					}
				}
			}
		} else {
			return fmt.Errorf("unable to apply label and taint with total nodes = %d and workload nodes = %d: %w",
				totalNodes, ltn.NumWorkloadNodes, ErrNumOfNodes)
		}

		logrus.Info("Apply label and taint to K8s nodes successful")
	}

	// Remove Labels or Taints from K8S Nodes
	if ltn.RemoveLabel || ltn.RemoveTaint {
		logrus.Info("Remove label and taint from K8s nodes started")

		cbNodes := ltn.NumCBNodes
		workloadNodes := ltn.NumWorkloadNodes

		for _, nodeName := range k8sNodeNames {
			exists, err := checkIfLabelExists(k8sNodesMap[nodeName], k8sNodeLabelForCBNode, defaultLabelValue)
			if err != nil {
				return err
			}

			if exists && cbNodes > 0 {
				if ltn.RemoveLabel {
					err = kubectl.Unlabel(nodeName, k8sNodeLabelForCBNode).ExecWithoutOutputCapture()
					if err != nil {
						return fmt.Errorf("remove label of couchbase node: %w", err)
					}
				}

				if ltn.RemoveTaint {
					err = kubectl.RemoveTaint(nodeName, k8sNodeLabelForCBNode, defaultLabelValue, taintNoSchedule).ExecWithoutOutputCapture()
					if err != nil {
						return fmt.Errorf("remove taint of couchbase node: %w", err)
					}
				}

				cbNodes--
			}

			exists, err = checkIfLabelExists(k8sNodesMap[nodeName], k8sNodeLabelForGenericWorkload, defaultLabelValue)
			if err != nil {
				return err
			}

			if exists && workloadNodes > 0 {
				if ltn.RemoveLabel {
					err = kubectl.Unlabel(nodeName, k8sNodeLabelForGenericWorkload).ExecWithoutOutputCapture()
					if err != nil {
						return fmt.Errorf("remove label of workload node: %w", err)
					}
				}

				if ltn.RemoveTaint {
					err = kubectl.RemoveTaint(nodeName, k8sNodeLabelForGenericWorkload, defaultLabelValue, taintNoSchedule).ExecWithoutOutputCapture()
					if err != nil {
						return fmt.Errorf("remove taint of workload node: %w", err)
					}
				}

				workloadNodes--
			}
		}

		logrus.Info("Remove label and taint from K8s nodes successful")
	}

	return nil
}

func (ltn *LabelTaintNodes) GetState() string {
	return ltn.State
}

func checkIfLabelExists(k8sNodeInfoJSON, labelKeyName, labelValue string) (bool, error) {
	var jsonOutput map[string]interface{}

	// Unmarshal the k8s node info json string into map[string]interface{}
	err := json.Unmarshal([]byte(k8sNodeInfoJSON), &jsonOutput)
	if err != nil {
		return false, fmt.Errorf("check if label exists: unmarshal k8s node info: %w", err)
	}

	val, err := jsonpatch.Get(&jsonOutput, "/metadata/labels/"+labelKeyName)
	if err != nil {
		if !errors.Is(err, jsonpatch.ErrPathNotFoundInJSON) {
			return false, fmt.Errorf("check if label exists: jsonpatch get: %w", err)
		}
	}

	value, ok := val.(string)
	if !ok {
		return false, fmt.Errorf("decode label name as string: %w", ErrDecodeError)
	}

	if value == labelValue {
		return true, nil
	}

	return false, nil
}
