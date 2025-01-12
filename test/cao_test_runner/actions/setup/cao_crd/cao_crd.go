package caocrdsetup

import (
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	caoinstallutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/install_utils/cao_install_utils"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/validations"
	"github.com/sirupsen/logrus"
)

var (
	ErrDecodeCAOSetupConfig   = errors.New("unable to decode CaoCrdSetupConfig")
	ErrNoConfigFound          = errors.New("no config found for setting up cao binary and crd")
	ErrNoAvailableContexts    = errors.New("no such available contexts")
	ErrIllegalPlatform        = errors.New("illegal platform")
	ErrIllegalOperatingSystem = errors.New("illegal operating system")
	ErrIllegalArchitecture    = errors.New("illegal architecture")
)

type CaoCrdSetupConfig struct {
	Description     []string                            `yaml:"description"`
	OperatorVersion string                              `yaml:"operatorVersion" caoCli:"required,context" env:"OPERATOR_VERSION"`
	Platform        caoinstallutils.PlatformType        `yaml:"platform" caoCli:"required,context" env:"PLATFORM"`
	OperatingSystem caoinstallutils.OperatingSystemType `yaml:"operatingSystem" caoCli:"required,context" env:"OPERATING_SYSTEM"`
	Architecture    caoinstallutils.ArchitectureType    `yaml:"architecture" caoCli:"required,context" env:"ARCHITECTURE"`
	Validators      []map[string]any                    `yaml:"validators,omitempty"`
}

type SetupCaoCrd struct {
	desc       string
	yamlConfig interface{}
}

func NewCaoCrdSetupConfig(config interface{}) (actions.Action, error) {
	if config != nil {
		c, ok := config.(*CaoCrdSetupConfig)
		if !ok {
			return nil, ErrDecodeCAOSetupConfig
		}

		return &SetupCaoCrd{
			desc:       "setup the cao binary and crd",
			yamlConfig: c,
		}, nil
	}

	return nil, ErrNoConfigFound
}

func (action *SetupCaoCrd) Describe() string {
	return action.desc
}

func (action *SetupCaoCrd) Do(ctx *context.Context, testAssets assets.TestAssetGetter) error {
	if action.yamlConfig == nil {
		return ErrNoConfigFound
	}

	c, ok := action.yamlConfig.(*CaoCrdSetupConfig)
	if !ok {
		return ErrDecodeCAOSetupConfig
	}

	logrus.Infof("CAO CRD Setup started")

	resultsDir := testAssets.GetResultsDirectory()

	installParams, err := caoinstallutils.NewInstallParams(c.OperatorVersion, c.Platform, c.OperatingSystem, c.Architecture, resultsDir.DirectoryPath)
	if err != nil {
		return fmt.Errorf("error creating cao crd install setup: %w", err)
	}

	installClient := caoinstallutils.NewInstallClient()

	caoBinaryPath, crdPath, err := installClient.InstallCaoCrd(installParams)
	if err != nil {
		return fmt.Errorf("error installing cao crd: %w", err)
	}

	err = kubectl.ApplyFiles(crdPath).ExecWithoutOutputCapture()
	if err != nil {
		return fmt.Errorf("kubectl apply crd yaml: %w", err)
	}

	ctx.WithID(context.OperatingSystemKey, string(c.OperatingSystem))
	ctx.WithID(context.PlatformKey, string(c.Platform))
	ctx.WithID(context.ArchitectureKey, string(c.Architecture))
	ctx.WithID(context.OperatorVersionKey, c.OperatorVersion)
	ctx.WithID(context.CRDPathKey, crdPath)
	ctx.WithID(context.CAOBinaryPathKey, caoBinaryPath)

	return nil
}

func (action *SetupCaoCrd) Config() interface{} {
	return action.yamlConfig
}

func (action *SetupCaoCrd) CheckConfig() error {
	if action.yamlConfig == nil {
		return ErrNoConfigFound
	}

	c, ok := action.yamlConfig.(*CaoCrdSetupConfig)
	if !ok {
		return ErrDecodeCAOSetupConfig
	}

	switch c.Platform {
	case caoinstallutils.Kubernetes, caoinstallutils.Openshift:
		// No-op
	default:
		return ErrIllegalPlatform
	}

	switch c.OperatingSystem {
	case caoinstallutils.Linux, caoinstallutils.Windows, caoinstallutils.MacOs:
		// No-op
	default:
		return ErrIllegalOperatingSystem
	}

	switch c.Architecture {
	case caoinstallutils.Amd64, caoinstallutils.Arm64:
		// No-op
	default:
		return ErrIllegalArchitecture
	}

	return nil
}

func (action *SetupCaoCrd) RunValidators(ctx *context.Context,
	state string, testAssets assets.TestAssetGetterSetter) error {
	if action.yamlConfig == nil {
		return ErrNoConfigFound
	}

	c, ok := action.yamlConfig.(*CaoCrdSetupConfig)
	if !ok {
		return ErrDecodeCAOSetupConfig
	}

	if ok, err := validations.RunValidator(ctx, c.Validators, state, testAssets); !ok {
		return fmt.Errorf("run %s validations: %w", state, err)
	}

	return nil
}
