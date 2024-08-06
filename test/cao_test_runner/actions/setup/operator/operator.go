package operatorsetup

import (
	"errors"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util"
)

var (
	ErrUnableToDecodeOperatorConfig = errors.New("unable to decode OperatorConfig in Do()")
)

const (
	length = 10
)

type OperatorConfig struct {
	Namespace                string `yaml:"namespace"`
	CRDS                     string `yaml:"CRDS"`
	OperatorImage            string `yaml:"operatorVersion"`
	AdmissionControllerImage string `yaml:"admissionControllerVersion"`
}

func NewSetupOperatorConfig(config interface{}) (actions.Action, error) {
	c := &OperatorConfig{}
	if config != nil {
		c, _ = config.(*OperatorConfig)
	}

	if c.Namespace == "" {
		c.Namespace = util.CreateNameSpaceName(length)
	}

	if c.OperatorImage == "" {
		c.OperatorImage = "docker.io/couchbase/operator:latest"
	}

	if c.AdmissionControllerImage == "" {
		c.AdmissionControllerImage = "docker.io/couchbase/admission-controller:latest"
	}

	return &Operator{
		desc:       "Setup [namespace, CRDS, operator, admission Controller]",
		yamlConfig: c,
	}, nil
}

type Operator struct {
	desc       string
	yamlConfig interface{}
}

func (s *Operator) Describe() string {
	return s.desc
}

func (s *Operator) Checks(_ *context.Context, _ interface{}, _ string) error {
	return nil
}

func (s *Operator) Do(ctx *context.Context, _ interface{}) error {
	c, ok := s.yamlConfig.(*OperatorConfig)

	if !ok {
		return ErrUnableToDecodeOperatorConfig
	}

	ctx.WithID(context.NamespaceIDKey, c.Namespace)

	ctx.WithID(context.OperatorIDKey, c.OperatorImage)

	ctx.WithID(context.AdmissionIDKey, c.AdmissionControllerImage)

	return nil
}

func (s *Operator) Config() interface{} {
	return s.yamlConfig
}
