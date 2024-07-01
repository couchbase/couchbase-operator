package setup

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
	ErrCouchbaseConfigDecode = errors.New("unable to decode CouchbaseConfig")
	ErrMissingYaml           = errors.New("missing yaml spec for couchbase deployment")
	ErrConfigCouchbase       = errors.New("no config found for couchbase deployment")
)

type CouchbaseConfig struct {
	CBClusterSpecPath string         `yaml:"cbClusterSpecPath" caoCli:"required"`
	CBBucketsSpecPath string         `yaml:"cbBucketsSpecPath"`
	CBSecretsSpecPath string         `yaml:"cbSecretsSpecPath"`
	Validators        map[string]any `yaml:"validators,omitempty"`
}

func NewCouchbaseConfig(config interface{}) (actions.Action, error) {
	if config != nil {
		c, ok := config.(*CouchbaseConfig)
		if !ok {
			return nil, ErrCouchbaseConfigDecode
		}

		if c.CBClusterSpecPath == "" {
			return nil, ErrMissingYaml
		}

		return &Couchbase{
			desc:       "setup couchbase using yaml specification",
			yamlConfig: c,
		}, nil
	}

	return nil, ErrConfigCouchbase
}

type Couchbase struct {
	desc       string
	yamlConfig interface{}
}

func (s *Couchbase) Checks(ctx *context.Context, _ interface{}, state string) error {
	c, _ := s.yamlConfig.(*CouchbaseConfig)
	ctx.WithID(context.CouchbaseSpecPathIDKey, c.CBClusterSpecPath)

	if ok, err := validations.RunValidator(ctx, c.Validators, state); !ok {
		return fmt.Errorf("run %s validations: %w", state, err)
	}

	return nil
}

func (s *Couchbase) Describe() string {
	return s.desc
}

func (s *Couchbase) Do(ctx *context.Context, _ interface{}) error {
	c, _ := s.yamlConfig.(*CouchbaseConfig)
	ctx.WithID(context.CouchbaseSpecPathIDKey, c.CBClusterSpecPath)

	logrus.Infof("Deploy Couchbase started")

	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	err = kubectl.ApplyFiles(path.Join(dir, c.CBClusterSpecPath)).InNamespace("default").ExecWithoutOutputCapture()
	if err != nil {
		logrus.Error("kubectl apply couchbase cluster yaml:", err)
		return fmt.Errorf("kubectl apply couchbase cluster yaml: %w", err)
	}

	if c.CBBucketsSpecPath != "" {
		err = kubectl.ApplyFiles(path.Join(dir, c.CBBucketsSpecPath)).InNamespace("default").ExecWithoutOutputCapture()
		if err != nil {
			logrus.Error("kubectl apply couchbase buckets yaml:", err)
			return fmt.Errorf("kubectl apply couchbase buckets yaml: %w", err)
		}
	}

	if c.CBSecretsSpecPath != "" {
		err = kubectl.ApplyFiles(path.Join(dir, c.CBSecretsSpecPath)).InNamespace("default").ExecWithoutOutputCapture()
		if err != nil {
			logrus.Error("kubectl apply couchbase secrets yaml:", err)
			return fmt.Errorf("kubectl apply couchbase secrets yaml: %w", err)
		}
	}

	return nil
}

func (s *Couchbase) Config() interface{} {
	return s.yamlConfig
}
