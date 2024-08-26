package validations

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/jsonpatch"
	"github.com/sirupsen/logrus"
)

const (
	defaultReadinessDuration = 10 * 60 * time.Second
	defaultReadinessInterval = 15 * time.Second
	cbNamespace              = "/items/0/metadata/namespace"
	cbClusterName            = "/items/0/metadata/name"
)

var (
	clusterStatus = map[string]string{
		"/items/0/status/conditions/0/type":   "Available",
		"/items/0/status/conditions/0/status": "True",
		"/items/0/status/conditions/1/type":   "Balanced",
		"/items/0/status/conditions/1/status": "True",
	}
)

type CouchbaseReadiness struct {
	Name           string `yaml:"name" caoCli:"required"`
	State          string `yaml:"state" caoCli:"required"`
	DurationInSecs int64  `yaml:"durationInSecs"`
	IntervalInSecs int64  `yaml:"intervalInSecs"`
}

func (c *CouchbaseReadiness) Run(_ *context.Context) error {
	defer handlePanic()
	logrus.Info("Couchbase readiness check started")

	// CHECK: State of the Cluster
	// namespace := context.ValueID(ctxt.Context(), context.NamespaceIDKey)
	// cbClusterName := context.ValueID(ctxt.Context(), context.CouchbaseClusterNameKey)

	funcGetStateApplyPatch := func() error {
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
			Test(cbNamespace, "default").
			Test(cbClusterName, "cb-example")

		for key, val := range clusterStatus {
			patchSet.Test(key, val)
		}

		err = jsonpatch.Apply(&jsonOutput, patchSet.Patches())
		if err != nil {
			return fmt.Errorf("apply patch: %w", err)
		}

		return nil
	}

	// If the DurationInMinutes and IntervalInMinutes is provided then it will be used, else default values to be used
	checkDuration := defaultReadinessDuration
	checkInterval := defaultReadinessInterval

	if c.DurationInSecs != 0 {
		checkDuration = time.Duration(c.DurationInSecs) * time.Second
	}

	if c.IntervalInSecs != 0 {
		checkInterval = time.Duration(c.IntervalInSecs) * time.Second
	}

	err := util.RetryFunctionTillTimeout(funcGetStateApplyPatch, checkDuration, checkInterval)
	if err != nil {
		return fmt.Errorf("retry function: %w", err)
	}

	logrus.Info("Couchbase readiness check successful")

	return nil
}

func (c *CouchbaseReadiness) GetState() string {
	return c.State
}
