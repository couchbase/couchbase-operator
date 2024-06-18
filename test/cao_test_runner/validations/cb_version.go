package validations

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/kubectl"
	"github.com/sirupsen/logrus"
)

const (
	cbVersionCheck       = "/items/0/status/currentVersion"
	upgradeTotalDuration = 10 * time.Minute
	upgradePollInterval  = 1 * time.Minute
)

type CBVersion struct {
	State     string `yaml:"state"`
	CBVersion string `yaml:"cbVersion"`
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

	err := util.RetryFunctionTillTimeout(funcCheckUpgrade, upgradeTotalDuration, upgradePollInterval)
	if err != nil {
		return fmt.Errorf("retry function: %w", err)
	}

	logrus.Info("Couchbase version check successful:", c.CBVersion)

	return nil
}

func (c *CBVersion) GetState() string {
	return c.State
}
