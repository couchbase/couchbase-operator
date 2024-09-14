package chaos

import (
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	managedsvcs "github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	cbpodfilter "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/cb_pod_filter"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/triggers"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/validations"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

var (
	ErrChaosConfigDecode   = errors.New("unable to decode config into ChaosConfig")
	ErrChaosConfigNotFound = errors.New("no config found for chaos action")
)

type ChaosConfig struct {
	Description    []string                           `yaml:"description"`
	ManagedSvcName managedsvcs.ManagedServiceProvider `yaml:"managedService" caoCli:"required"` // TODO to be taken from context
	ClusterName    string                             `yaml:"clusterName" caoCli:"required"`    // TODO to be taken from context
	ChaosList      []ChaosList                        `yaml:"chaosList" caoCli:"required"`
	Validators     []map[string]any                   `yaml:"validators,omitempty"`
}

type ChaosList struct {
	// ChaosIterations determines how many times shall the chaos action be performed
	ChaosIterations int64 `yaml:"chaosIterations"`
	// CBPodFilter is used to find and filter the CB pods on which we have to perform chaos actions.
	CBPodFilter cbpodfilter.CBPodFilter `yaml:"cbPodFilter" caoCli:"required"`
	// ChaosActions is a list of all the actions to be performed for all the filtered pods.
	ChaosActions []ActionName `yaml:"chaosActions" caoCli:"required"`
	// CBServiceChaos is a list of CBService which determines the CB service chaos (if ChaosAction = "CBServiceChaos") for all the filtered pods.
	CBServiceChaos []CBService `yaml:"cbServiceChaos"`
	// Trigger is a list of TriggerConfig which determines the triggers for all the filtered pods.
	Trigger []triggers.TriggerConfig `yaml:"triggerConfig" caoCli:"required"`

	// Couchbase Cluster Info Parameters

	// CBUpgradeVersion is the CB version to which the cluster is being upgraded to. Used for upgrade trigger checks.
	CBUpgradeVersion string `yaml:"cbUpgradeVersion"`
	// CBPodsAfterScaling is the number of nodes the cluster shall have after scaling op.
	CBPodsAfterScaling int `yaml:"cbPodsAfterScaling"`
}

type Chaos struct {
	desc       string
	yamlConfig interface{}
}

func NewChaosConfig(config interface{}) (actions.Action, error) {
	if config == nil {
		return nil, fmt.Errorf("new chaos config: %w", ErrChaosConfigNotFound)
	}

	chaosConfig, ok := config.(*ChaosConfig)
	if !ok {
		return nil, fmt.Errorf("new chaos config: %w", ErrChaosConfigDecode)
	}

	if !managedsvcs.ValidateManagedServices(chaosConfig.ManagedSvcName) {
		return nil, fmt.Errorf("new chaos config: %w", managedsvcs.ErrManagedServiceNotFound)
	}

	// Validating the ChaosList.
	for i := range chaosConfig.ChaosList {
		err := validateChaosList(&chaosConfig.ChaosList[i])
		if err != nil {
			return nil, fmt.Errorf("new chaos config: %w", err)
		}
	}

	return &Chaos{
		desc:       fmt.Sprintf("perform chaos action desc: %v", chaosConfig.Description),
		yamlConfig: chaosConfig,
	}, nil
}

func (c *Chaos) Checks(ctx *context.Context, _ interface{}, state string) error {
	chaosConfig, _ := c.yamlConfig.(*ChaosConfig)

	logrus.Infof("%s validators running for the chaos action desc `%v`", state, chaosConfig.Description)

	if ok, err := validations.RunValidator(ctx, chaosConfig.Validators, state); !ok {
		return fmt.Errorf("run %s validations for the chaos action desc `%v`: %w", state, chaosConfig.Description, err)
	}

	logrus.Infof("%s validators successful for the chaos action desc `%v`", state, chaosConfig.Description)

	return nil
}

func (c *Chaos) Do(ctx *context.Context, _ interface{}) error {
	chaosConfig, _ := c.yamlConfig.(*ChaosConfig)

	logrus.Infof("Starting chaos action desc: %v", chaosConfig.Description)

	// Based on the number chaos actions in ChaosConfig.ChaosList. Spawn separate go routines for each chaos action
	// This is required as each chaos action will wait for some trigger. All the triggers must be run concurrently in
	// order to not miss any cluster condition.
	eg := &errgroup.Group{}

	for _, chaosAction := range chaosConfig.ChaosList {
		filteredCBPods, err := chaosAction.CBPodFilter.FilterPods()
		if err != nil {
			return fmt.Errorf("perform chaos action: %w", err)
		}

		// Using ChaosConfig and the ChaosList we populate the CBPodChaosConfig which contains chaos action config for each pod.
		cbPodChaosSlice := populateCBPodChaos(chaosConfig, &chaosAction, filteredCBPods)

		for _, cbPodChaos := range cbPodChaosSlice {
			eg.Go(func() error {
				return ExecuteChaosAction(ctx, cbPodChaos)
			})
		}
	}

	// Wait for the goroutines to complete and if error occurs in one of the go routines return it.
	err := eg.Wait()
	if err != nil {
		return fmt.Errorf("perform chaos action: %w", err)
	}

	logrus.Infof("Successfully executed chaos action desc: %s", chaosConfig.Description)

	return nil
}

func (c *Chaos) Describe() string {
	return c.desc
}

func (c *Chaos) Config() interface{} {
	return c.yamlConfig
}
