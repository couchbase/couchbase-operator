package labeltaintnodes

import (
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	nodefilter "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/node_filter"
	corev1 "k8s.io/api/core/v1"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"

	"github.com/sirupsen/logrus"
)

var (
	ErrLabelTaintNodeConfig = errors.New("LabelTaintNodeConfig is nil")
	ErrDecodeLabelTaintNode = errors.New("unable to decode LabelTaintNodeConfig")
)

type LabelTaintNodeConfig struct {
	Name        string   `yaml:"name" caoCli:"required"`
	Description []string `yaml:"description"`

	// NodeFilter helps to filter the k8s nodes on which apply / remove the labels or taints.
	NodeFilter *nodefilter.NodeFilter `yaml:"nodeFilter" caoCli:"required"`

	// Parameters for Labels

	ApplyLabel  bool     `yaml:"applyLabel"`
	RemoveLabel bool     `yaml:"removeLabel"`
	Labels      []Labels `yaml:"labels"`

	// Parameters for Taints

	ApplyTaint  bool     `yaml:"applyTaint"`
	RemoveTaint bool     `yaml:"removeTaint"`
	Taints      []Taints `yaml:"taints"`
}

type Labels struct {
	LabelKey   string `yaml:"labelKey"`
	LabelValue string `yaml:"labelValue"`
}

type Taints struct {
	TaintKey    string             `yaml:"taintKey"`
	TaintValue  string             `yaml:"taintValue"`
	TaintEffect corev1.TaintEffect `yaml:"taintEffect"`
}

func NewLabelTaintNodeConfig(config interface{}) (actions.Action, error) {
	if config == nil {
		return nil, fmt.Errorf("new label taint node config: %w", ErrLabelTaintNodeConfig)
	}

	ltnConfig, ok := config.(*LabelTaintNodeConfig)
	if !ok {
		return nil, fmt.Errorf("new label taint node config: %w", ErrDecodeLabelTaintNode)
	}

	return &LabelTaintNode{
		desc:       "apply or remove labels and taints to k8s nodes",
		yamlConfig: ltnConfig,
	}, nil
}

type LabelTaintNode struct {
	desc       string
	yamlConfig interface{}
}

func (ltn *LabelTaintNode) Do(_ *context.Context, testAssets assets.TestAssetGetter) error {
	ltnConfig, _ := ltn.yamlConfig.(*LabelTaintNodeConfig)

	filterNodes, err := nodefilter.NewFilterNodesInterface(&ltnConfig.NodeFilter.ManagedSvcProvider)
	if err != nil {
		return fmt.Errorf("label taint nodes: %w", err)
	}

	filteredNodes, err := filterNodes.FilterNodesUsingStrategy(ltnConfig.NodeFilter)
	if err != nil {
		return fmt.Errorf("label taint nodes: %w", err)
	}

	if ltnConfig.ApplyLabel {
		logrus.Info("Apply label to k8s nodes started")
		logrus.Infof("Filtered nodes: %v", filteredNodes)

		for _, label := range ltnConfig.Labels {
			err = kubectl.Label(filteredNodes, "node", label.LabelKey, label.LabelValue).ExecWithoutOutputCapture()
			if err != nil {
				return fmt.Errorf("apply label to couchbase node: %w", err)
			}
		}

		logrus.Info("Apply label to K8s nodes successful")
	}

	if ltnConfig.ApplyTaint {
		logrus.Info("Apply taint to K8s nodes started")
		logrus.Infof("Filtered nodes: %v", filteredNodes)

		for _, taint := range ltnConfig.Taints {
			err = kubectl.Taint(filteredNodes, taint.TaintKey, taint.TaintValue, string(taint.TaintEffect)).ExecWithoutOutputCapture()
			if err != nil {
				return fmt.Errorf("apply taint to couchbase node: %w", err)
			}
		}

		logrus.Info("Apply taint to K8s nodes successful")
	}

	if ltnConfig.RemoveLabel {
		logrus.Info("Remove label from K8s nodes started")
		logrus.Infof("Filtered nodes: %v", filteredNodes)

		for _, label := range ltnConfig.Labels {
			err = kubectl.Unlabel(filteredNodes, "node", label.LabelKey).ExecWithoutOutputCapture()
			if err != nil {
				return fmt.Errorf("remove label of couchbase node: %w", err)
			}
		}

		logrus.Info("Remove label from K8s nodes successful")
	}

	if ltnConfig.RemoveTaint {
		logrus.Info("Remove taint from K8s nodes started")
		logrus.Infof("Filtered nodes: %v", filteredNodes)

		for _, taint := range ltnConfig.Taints {
			err = kubectl.RemoveTaint(filteredNodes, taint.TaintKey, taint.TaintValue, string(taint.TaintEffect)).ExecWithoutOutputCapture()
			if err != nil {
				return fmt.Errorf("remove taint of couchbase node: %w", err)
			}
		}

		logrus.Info("Remove taint from K8s nodes successful")
	}

	return nil
}

func (ltn *LabelTaintNode) Describe() string {
	return ltn.desc
}

func (ltn *LabelTaintNode) Config() interface{} {
	return ltn.yamlConfig
}

func (ltn *LabelTaintNode) CheckConfig() error {
	ltnConfig, ok := ltn.yamlConfig.(*LabelTaintNodeConfig)
	if !ok {
		return fmt.Errorf("new label taint node config: %w", ErrDecodeLabelTaintNode)
	}

	err := ValidateLabelTaintNodeConfig(ltnConfig)
	if err != nil {
		return fmt.Errorf("new label taint node config: %w", err)
	}

	return nil
}

func (ltn *LabelTaintNode) RunValidators(_ *context.Context, _ string, _ assets.TestAssetGetterSetter) error {
	return nil
}
