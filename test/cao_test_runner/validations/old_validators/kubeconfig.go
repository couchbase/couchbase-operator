package oldvalidators

import (
	"errors"
	"fmt"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	"github.com/sirupsen/logrus"
)

var (
	ErrNotMatching = errors.New("does not match the expected context")
)

const (
	defaultCheckDuration = 120 * time.Second
	defaultCheckInterval = 15 * time.Second
)

type KubeConfigValidator struct {
	Name           string `yaml:"name" caoCli:"required"`
	State          string `yaml:"state" caoCli:"required"`
	CurrentContext string `yaml:"k8sContext" caoCli:"required"`
	DurationInSecs int64  `yaml:"durationInSecs"`
	IntervalInSecs int64  `yaml:"intervalInSecs"`
}

func (validator *KubeConfigValidator) GetState() string {
	return validator.State
}
func (validator *KubeConfigValidator) Run(ctx *context.Context, testAssets assets.TestAssetGetterSetter) error {
	defer handlePanic()
	logrus.Info("Kubeconfig context validator check started")

	funcCheckKubeConfigContext := func() error {
		currentContext, err := kubectl.GetCurrentContext().Output()
		if err != nil {
			return fmt.Errorf("kubectl cannot fetch current context: %w", err)
		}

		if currentContext != validator.CurrentContext {
			return fmt.Errorf("current context: %s and expected context %s: %w", currentContext, validator.CurrentContext, ErrNotMatching)
		}

		return nil
	}

	checkDuration := defaultCheckDuration
	checkInterval := defaultCheckInterval

	if validator.DurationInSecs != 0 {
		checkDuration = time.Duration(validator.DurationInSecs) * time.Second
	}

	if validator.IntervalInSecs != 0 {
		checkInterval = time.Duration(validator.IntervalInSecs) * time.Second
	}

	err := util.RetryFunctionTillTimeout(funcCheckKubeConfigContext, checkDuration, checkInterval)
	if err != nil {
		return fmt.Errorf("retry function: %w", err)
	}

	logrus.Info("Kubeconfig context validator check successful")

	return nil
}
