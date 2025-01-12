package destroyadmissioncontroller

import (
	"errors"
	"fmt"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/cao"
	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/validations"
	"github.com/sirupsen/logrus"
)

var (
	ErrUnableToDecodeAdmissionConfig = errors.New("unable to decode AdmissionControllerConfig in Do()")
	ErrNoAdmissionConfigFound        = errors.New("no config found for deleting admission pod")
	ErrIllegalScope                  = errors.New("illegal scope")
	ErrCAOBinaryPathInvalid          = errors.New("cao binary path does not exist")
)

type ScopeType string

const (
	Namespace  ScopeType = "namespace"
	Cluster    ScopeType = "cluster"
	BlankScope ScopeType = ""
)

const (
	DefaultScope ScopeType = Cluster
)

type AdmissionControllerConfig struct {
	Description   []string         `yaml:"description"`
	CAOBinaryPath string           `yaml:"caoBinaryPath" caoCli:"required,context" env:"CAO_BINARY_PATH"`
	Scope         ScopeType        `yaml:"scope"`
	Validators    []map[string]any `yaml:"validators,omitempty"`
}

func NewDestroyAdmissionControllerConfig(config interface{}) (actions.Action, error) {
	if config != nil {
		c, ok := config.(*AdmissionControllerConfig)
		if !ok {
			return nil, ErrUnableToDecodeAdmissionConfig
		}

		return &DeleteAdmissionController{
			desc:       "Delete admission controller pod",
			yamlConfig: c,
		}, nil
	}

	return nil, ErrNoAdmissionConfigFound
}

type DeleteAdmissionController struct {
	desc       string
	yamlConfig interface{}
}

func (action *DeleteAdmissionController) Describe() string {
	return action.desc
}

func (action *DeleteAdmissionController) CheckConfig() error {
	if action.yamlConfig == nil {
		return ErrNoAdmissionConfigFound
	}

	c, ok := action.yamlConfig.(*AdmissionControllerConfig)
	if !ok {
		return ErrUnableToDecodeAdmissionConfig
	}

	switch c.Scope {
	case BlankScope:
		c.Scope = DefaultScope
	case Namespace, Cluster:
		// No-op
	default:
		return ErrIllegalScope
	}

	if ok = fileutils.NewFile(c.CAOBinaryPath).IsFileExists(); !ok {
		return fmt.Errorf("cao binary path %s does not exist: %w", c.CAOBinaryPath, ErrCAOBinaryPathInvalid)
	}

	return nil
}

func (action *DeleteAdmissionController) RunValidators(ctx *context.Context, state string, testAssets assets.TestAssetGetterSetter) error {
	if action.yamlConfig == nil {
		return ErrNoAdmissionConfigFound
	}

	c, ok := action.yamlConfig.(*AdmissionControllerConfig)
	if !ok {
		return ErrUnableToDecodeAdmissionConfig
	}

	if ok, err := validations.RunValidator(ctx, c.Validators, state, testAssets); !ok {
		return fmt.Errorf("run %s validations: %w", state, err)
	}

	return nil
}

func (action *DeleteAdmissionController) Do(ctx *context.Context, testAssets assets.TestAssetGetter) error {
	if action.yamlConfig == nil {
		return ErrNoAdmissionConfigFound
	}

	c, ok := action.yamlConfig.(*AdmissionControllerConfig)
	if !ok {
		return ErrUnableToDecodeAdmissionConfig
	}

	logrus.Infof("Admission Controller pod deletion started")

	logrus.Info("cao delete admission at :", time.Now().Format(time.RFC3339))

	cao.WithBinaryPath(c.CAOBinaryPath)

	if err := cao.DeleteAdmissionController(string(c.Scope)).ExecWithoutOutputCapture(); err != nil {
		return fmt.Errorf("failed to execute cao delete admission: %w", err)
	}

	return nil
}

func (action *DeleteAdmissionController) Config() interface{} {
	return action.yamlConfig
}
