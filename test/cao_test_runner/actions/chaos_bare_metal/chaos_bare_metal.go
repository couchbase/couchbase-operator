package chaosbaremetal

import (
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	baremetal "github.com/couchbase/couchbase-operator/test/cao_test_runner/bare_metal_sdks"
	cbbaremetalfilter "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cb_bare_metal_filter"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/triggers"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/validations"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

var (
	ErrChaosBareMetalConfigNotFound = errors.New("chaos bare metal config not found")
	ErrChaosBareMetalConfigDecode   = errors.New("unable to decode BareMetalChaosConfig")
)

type BareMetalChaosConfig struct {
	Description   []string                `yaml:"description"`
	CloudProvider baremetal.CloudProvider `yaml:"cloudProvider" caoCli:"required"`
	CloudRegion   string                  `yaml:"cloudRegion" caoCli:"required"`
	InstanceDNS   []string                `yaml:"instanceDNS" caoCli:"required"`
	ChaosList     []ChaosList             `yaml:"chaosList" caoCli:"required"`
	Validators    []map[string]any        `yaml:"validators,omitempty"`
}

type ChaosList struct {
	// ChaosIterations determines how many times shall the chaos action be performed
	ChaosIterations int64 `yaml:"chaosIterations"`
	// CBBareMetalFilter is used to find and filter the bare metal CB nodes on which we have to perform chaos actions.
	CBBareMetalFilter cbbaremetalfilter.CBBareMetalFilter `yaml:"cbBareMetalFilter" caoCli:"required"`
	// ChaosActions is a list of all the actions to be performed for all the filtered CB nodes.
	ChaosActions []ActionName `yaml:"chaosActions" caoCli:"required"`
	// TODO CBServiceChaos is a list of CBService which determines the CB service chaos (if ChaosAction = "CBServiceChaos") for all the filtered CB nodes.
	// CBServiceChaos []chaos.CBService `yaml:"cbServiceChaos"`
	// Trigger is a list of TriggerConfig which determines the triggers for all the filtered CB nodes.
	Trigger []triggers.TriggerConfig `yaml:"triggerConfig" caoCli:"required"`

	// Couchbase Cluster Info Parameters

	// CBUpgradeVersion is the CB version to which the cluster is being upgraded to. Used for upgrade trigger checks.
	CBUpgradeVersion string `yaml:"cbUpgradeVersion"`
	// CBNodesAfterScaling is the number of nodes the cluster shall have after scaling op.
	CBNodesAfterScaling int `yaml:"cbNodesAfterScaling"`
}

type ChaosBareMetal struct {
	desc       string
	yamlConfig interface{}
}

func NewChaosBareMetalConfig(config interface{}) (actions.Action, error) {
	if config == nil {
		return nil, fmt.Errorf("new bare metal chaos config: %w", ErrChaosBareMetalConfigNotFound)
	}

	chaosConfig, ok := config.(*BareMetalChaosConfig)
	if !ok {
		return nil, fmt.Errorf("new bare metal chaos config: %w", ErrChaosBareMetalConfigDecode)
	}

	return &ChaosBareMetal{
		desc:       fmt.Sprintf("perform chaos action desc: %v", chaosConfig.Description),
		yamlConfig: chaosConfig,
	}, nil
}

func (c *ChaosBareMetal) RunValidators(ctx *context.Context, state string, testAssets assets.TestAssetGetterSetter) error {
	if c.yamlConfig == nil {
		return ErrChaosBareMetalConfigNotFound
	}

	chaosConfig, ok := c.yamlConfig.(*BareMetalChaosConfig)
	if !ok {
		return ErrChaosBareMetalConfigDecode
	}

	logrus.Infof("%s validators running for the bare metal chaos action desc `%v`", state, chaosConfig.Description)

	if ok, err := validations.RunValidator(ctx, chaosConfig.Validators, state); !ok {
		return fmt.Errorf("run %s validations for the bare metal chaos action desc `%v`: %w", state, chaosConfig.Description, err)
	}

	logrus.Infof("%s validators successful for the bare metal chaos action desc `%v`", state, chaosConfig.Description)

	return nil
}

func (c *ChaosBareMetal) CheckConfig() error {
	if c.yamlConfig == nil {
		return ErrChaosBareMetalConfigNotFound
	}

	chaosConfig, ok := c.yamlConfig.(*BareMetalChaosConfig)
	if !ok {
		return ErrChaosBareMetalConfigDecode
	}

	// Validating the ChaosList.
	for i := range chaosConfig.ChaosList {
		err := validateChaosList(&chaosConfig.ChaosList[i])
		if err != nil {
			return fmt.Errorf("new bare metal chaos config: %w", err)
		}
	}

	return nil
}

func (c *ChaosBareMetal) Do(ctx *context.Context, testAssets assets.TestAssetGetter) error {
	if c.yamlConfig == nil {
		return ErrChaosBareMetalConfigNotFound
	}

	chaosConfig, ok := c.yamlConfig.(*BareMetalChaosConfig)
	if !ok {
		return ErrChaosBareMetalConfigDecode
	}

	logrus.Infof("Starting chaos action desc: %v", chaosConfig.Description)

	// Based on the number chaos actions in BareMetalChaosConfig.ChaosList. Spawn separate go routines for each chaos action
	// This is required as each chaos action will wait for some trigger. All the triggers must be run concurrently in
	// order to not miss any cluster condition.
	eg := &errgroup.Group{}

	for _, chaosAction := range chaosConfig.ChaosList {
		filteredCBNodes, err := chaosAction.CBBareMetalFilter.FilterCBNodes()
		if err != nil {
			return fmt.Errorf("perform chaos action: %w", err)
		}

		// Using BareMetalChaosConfig and the ChaosList we populate the CBNodeChaosConfig which contains chaos action config for each CB node.
		cbNodeChaosSlice := populateCBNodeChaos(chaosConfig, &chaosAction, filteredCBNodes)

		for i := range cbNodeChaosSlice {
			eg.Go(func() error {
				return ExecuteChaosAction(ctx, cbNodeChaosSlice[i])
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

func (c *ChaosBareMetal) Describe() string {
	return c.desc
}

func (c *ChaosBareMetal) Config() interface{} {
	return c.yamlConfig
}
