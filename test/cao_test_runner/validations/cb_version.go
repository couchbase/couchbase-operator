package validations

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	"github.com/sirupsen/logrus"
)

const (
	cbVersionCheck              = "/items/0/status/currentVersion"
	defaultVersionCheckDuration = 10 * 60 * time.Second
	defaultVersionCheckInterval = 10 * time.Second
)

type CBVersion struct {
	Name              string `yaml:"name" caoCli:"required"`
	State             string `yaml:"state" caoCli:"required"`
	CBVersion         string `yaml:"cbVersion" caoCli:"required"`
	DurationInMinutes int64  `yaml:"durationInMinutes"`
	IntervalInMinutes int64  `yaml:"intervalInMinutes"`
}

func (c *CBVersion) Run(_ *context.Context) error {
	defer handlePanic()
	logrus.Info("Couchbase version check started")

	// CHECK: If Cluster Upgraded to Desired Version
	funcCheckUpgrade := func() error {
		stdout, err := kubectl.Get("couchbasecluster").FormatOutput("json").InNamespace("default").Output()
		if err != nil {
			return fmt.Errorf("get couchbasecluster information: %w", err)
		}

		// Unmarshal JSON into the map variable
		var jsonOutput map[string]interface{}

		err = json.Unmarshal([]byte(stdout), &jsonOutput)
		if err != nil {
			return fmt.Errorf("parse json: %w", err)
		}

		// When the cluster is fully deployed then, these values are set with the following values
		patchSet := jsonpatch.NewPatchSet().
			Test(cbVersionCheck, c.CBVersion)

		err = jsonpatch.Apply(&jsonOutput, patchSet.Patches())
		if err != nil {
			return fmt.Errorf("apply patch: %w", err)
		}

		return nil
	}

	// If the DurationInMinutes and IntervalInMinutes is provided then it will be used, else default values to be used
	checkDuration := defaultVersionCheckDuration
	checkInterval := defaultVersionCheckInterval

	if c.DurationInMinutes != 0 {
		checkDuration = time.Duration(c.DurationInMinutes) * time.Minute
	}

	if c.IntervalInMinutes != 0 {
		checkInterval = time.Duration(c.IntervalInMinutes) * time.Minute
	}

	err := util.RetryFunctionTillTimeout(funcCheckUpgrade, checkDuration, checkInterval)
	if err != nil {
		return fmt.Errorf("retry function: %w", err)
	}

	logrus.Info("Couchbase version check successful: ", c.CBVersion)

	return nil
}

func (c *CBVersion) GetState() string {
	return c.State
}
