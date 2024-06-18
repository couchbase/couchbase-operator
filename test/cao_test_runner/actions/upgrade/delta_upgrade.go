package upgrade

import (
	"errors"
	"fmt"
	"os"
	"path"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/kubectl"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/validations"
	"github.com/sirupsen/logrus"
)

var (
	ErrDeltaUpgradeConfig       = errors.New("no config found for delta recovery upgrade")
	ErrDeltaUpgradeDecodeFailed = errors.New("unable to decode the Delta Recovery Upgrade config")
	WaitTime                    = 2
)

type DeltaRecoveryUpgradeConfig struct {
	SpecPath   string         `yaml:"specPath" caoCli:"required"`
	Validators map[string]any `yaml:"validators,omitempty"`
}

func NewDeltaRecoveryUpgrade(conf interface{}) (actions.Action, error) {
	if conf != nil {
		c, ok := conf.(*DeltaRecoveryUpgradeConfig)
		if !ok {
			return nil, ErrDeltaUpgradeDecodeFailed
		}

		return &DeltaRecoveryUpgrade{
			desc:       "simulate a delta recovery upgrade",
			yamlConfig: c,
		}, nil
	}

	return nil, ErrDeltaUpgradeConfig
}

type DeltaRecoveryUpgrade struct {
	desc       string
	yamlConfig interface{}
}

func (d *DeltaRecoveryUpgrade) Checks(ctx *context.Context, _ interface{}, state string) error {
	c, ok := d.yamlConfig.(*DeltaRecoveryUpgradeConfig)
	if !ok {
		return ErrDeltaUpgradeConfig
	}

	if ok, err := validations.RunValidator(ctx, c.Validators, state); !ok {
		return fmt.Errorf("run %s validations: %w", state, err)
	}

	return nil
}

func (d *DeltaRecoveryUpgrade) Describe() string {
	return d.desc
}

func (d *DeltaRecoveryUpgrade) Do(_ *context.Context, _ interface{}) error {
	c, ok := d.yamlConfig.(*DeltaRecoveryUpgradeConfig)
	if !ok {
		return ErrDeltaUpgradeConfig
	}

	// namespace := context.ValueID(ctxt.Context(), context.NamespaceIDKey)
	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	err = kubectl.ApplyFiles(path.Join(dir, c.SpecPath)).InNamespace("default").ExecWithoutOutputCapture()
	if err != nil {
		logrus.Error("kubectl apply:", err)
		return fmt.Errorf("kubectl apply: %w", err)
	}

	return nil
}

func (d *DeltaRecoveryUpgrade) Config() interface{} {
	return d.yamlConfig
}
