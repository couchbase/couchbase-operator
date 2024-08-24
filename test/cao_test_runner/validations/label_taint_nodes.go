package validations

import (
	"errors"
	"fmt"

	nodefilter "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/node_filter"
	corev1 "k8s.io/api/core/v1"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"

	"github.com/sirupsen/logrus"
)

var (
	ErrDecodeError   = errors.New("unable to decode")
	ErrValueNotFound = errors.New("value is nil or empty")
)

type LabelTaintNodes struct {
	Name        string                `yaml:"name" caoCli:"required"`
	State       string                `yaml:"state" caoCli:"required"`
	NodeFilter  nodefilter.NodeFilter `yaml:"nodeFilter" caoCli:"required"`
	LabelKey    string                `yaml:"labelKey"`
	LabelValue  string                `yaml:"labelValue"`
	TaintKey    string                `yaml:"taintKey"`
	TaintValue  string                `yaml:"taintValue"`
	TaintEffect corev1.TaintEffect    `yaml:"taintEffect"`
	ApplyTaint  bool                  `yaml:"applyTaint" caoCli:"required"`
	ApplyLabel  bool                  `yaml:"applyLabel" caoCli:"required"`
	RemoveTaint bool                  `yaml:"removeTaint"`
	RemoveLabel bool                  `yaml:"removeLabel"`
}

func (ltn *LabelTaintNodes) Run(ctxt *context.Context) error {
	defer handlePanic()

	err := ltn.validate()
	if err != nil {
		return fmt.Errorf("label taint nodes: %w", err)
	}

	filterNodes, err := nodefilter.NewFilterNodesInterface(ltn.NodeFilter.ManagedSvcName)
	if err != nil {
		return fmt.Errorf("label taint nodes: %w", err)
	}

	filteredNodes, err := filterNodes.FilterNodesUsingStrategy(&ltn.NodeFilter)
	if err != nil {
		return fmt.Errorf("label taint nodes: %w", err)
	}

	if ltn.ApplyLabel {
		logrus.Info("Apply label to K8s nodes started")
		logrus.Info("Filtered nodes: %v", filteredNodes)

		for _, nodeName := range filteredNodes {
			err = kubectl.Label(nodeName, ltn.LabelKey, ltn.LabelValue).ExecWithoutOutputCapture()
			if err != nil {
				return fmt.Errorf("apply label to couchbase node: %w", err)
			}
		}
		logrus.Info("Apply label to K8s nodes successful")
	}

	if ltn.ApplyTaint {
		logrus.Info("Apply taint to K8s nodes started")
		logrus.Info("Filtered nodes: %v", filteredNodes)

		for _, nodeName := range filteredNodes {
			err = kubectl.Taint(nodeName, ltn.TaintKey, ltn.TaintValue, string(ltn.TaintEffect)).ExecWithoutOutputCapture()
			if err != nil {
				return fmt.Errorf("apply taint to couchbase node: %w", err)
			}
		}

		logrus.Info("Apply taint to K8s nodes successful")
	}

	if ltn.RemoveLabel {
		logrus.Info("Remove label from K8s nodes started")
		logrus.Info("Filtered nodes: %v", filteredNodes)

		for _, nodeName := range filteredNodes {
			err = kubectl.Unlabel(nodeName, ltn.LabelKey).ExecWithoutOutputCapture()
			if err != nil {
				return fmt.Errorf("remove label of couchbase node: %w", err)
			}
		}

		logrus.Info("Remove label from K8s nodes successful")
	}

	if ltn.RemoveTaint {
		logrus.Info("Remove taint from K8s nodes started")
		logrus.Info("Filtered nodes: %v", filteredNodes)

		for _, nodeName := range filteredNodes {
			err = kubectl.RemoveTaint(nodeName, ltn.TaintKey, ltn.TaintValue, string(ltn.TaintEffect)).ExecWithoutOutputCapture()
			if err != nil {
				return fmt.Errorf("remove taint of couchbase node: %w", err)
			}
		}

		logrus.Info("Remove taint from K8s nodes successful")
	}

	return nil
}

func (ltn *LabelTaintNodes) GetState() string {
	return ltn.State
}

func (ltn *LabelTaintNodes) validate() error {
	if ltn.ApplyLabel || ltn.RemoveLabel {
		if ltn.LabelKey == "" {
			return fmt.Errorf("validate label key: %w", ErrValueNotFound)
		}

		if ltn.ApplyLabel && ltn.LabelValue == "" {
			return fmt.Errorf("validate label value: %w", ErrValueNotFound)
		}
	}

	if ltn.ApplyTaint || ltn.RemoveTaint {
		if ltn.TaintKey == "" {
			return fmt.Errorf("validate taint key: %w", ErrValueNotFound)
		}

		if ltn.TaintValue == "" {
			return fmt.Errorf("validate taint value: %w", ErrValueNotFound)
		}

		switch ltn.TaintEffect {
		case corev1.TaintEffectNoSchedule, corev1.TaintEffectNoExecute, corev1.TaintEffectPreferNoSchedule:
			break
		default:
			return fmt.Errorf("validate taint effect: %w", ErrValueNotFound)
		}
	}

	return nil
}
