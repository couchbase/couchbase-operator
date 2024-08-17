package destroycrd

import (
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/validations"
	"github.com/sirupsen/logrus"
)

var (
	ErrDecodeCRDSetupConfig = errors.New("unable to decode CRDSetupConfig")
	ErrNoConfigFound        = errors.New("no config found for setting up crds")
	ErrNoAvailableContexts  = errors.New("no such available contexts")
)

type CRDDestroyConfig struct {
	Description []string         `yaml:"description"`
	CRDPath     string           `yaml:"crdPath" caoCli:"required"`
	Validators  []map[string]any `yaml:"validators,omitempty"`
}

type DestroyCRD struct {
	desc       string
	yamlConfig interface{}
}

func NewCRDDestroyConfig(config interface{}) (actions.Action, error) {
	if config != nil {
		c, ok := config.(*CRDDestroyConfig)
		if !ok {
			return nil, ErrDecodeCRDSetupConfig
		}

		return &DestroyCRD{
			desc:       "delete the crds",
			yamlConfig: c,
		}, nil
	}

	return nil, ErrNoConfigFound
}

func (action *DestroyCRD) Describe() string {
	return action.desc
}

func (action *DestroyCRD) Do(ctx *context.Context, config interface{}) error {
	c, ok := action.yamlConfig.(*CRDDestroyConfig)
	if !ok {
		return ErrNoConfigFound
	}

	logrus.Infof("CRD delete action started")

	err := kubectl.DeleteFromFiles(c.CRDPath).ExecWithoutOutputCapture()
	if err != nil {
		return fmt.Errorf("kubectl delete crd yaml: %w", err)
	}

	return nil
}

func (action *DestroyCRD) Config() interface{} {
	return action.yamlConfig
}

func (action *DestroyCRD) Checks(ctx *context.Context, config interface{}, state string) error {
	c, ok := action.yamlConfig.(*CRDDestroyConfig)
	if !ok {
		return ErrNoConfigFound
	}

	if ok, err := validations.RunValidator(ctx, c.Validators, state); !ok {
		return fmt.Errorf("run %s validations: %w", state, err)
	}

	return nil
}
