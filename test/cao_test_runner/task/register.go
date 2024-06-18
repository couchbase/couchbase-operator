package task

import (
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/setup"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/upgrade"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/workloads"
)

const (
	maxDepth = 120
)

type NewAction func(config interface{}) (actions.Action, error)

type ActionRegistration struct {
	newAction NewAction
	config    interface{}
}

func (r ActionRegistration) Config() interface{} {
	return r.config
}

type Register struct {
}

func (r Register) Actions() map[string]ActionRegistration {
	return map[string]ActionRegistration{
		"Delta Upgrade":    {newAction: upgrade.NewDeltaRecoveryUpgrade, config: &upgrade.DeltaRecoveryUpgradeConfig{}},
		"Setup Operator":   {newAction: setup.NewSetupOperatorConfig, config: &setup.OperatorConfig{}},
		"Deploy Couchbase": {newAction: setup.NewCouchbaseConfig, config: &setup.CouchbaseConfig{}},
		"Sleep":            {newAction: workloads.NewSleepActionConfig, config: &workloads.SleepActionConfig{}},
		"Generic Workload": {newAction: workloads.NewGenericWorkloadConfig, config: &workloads.GenericWorkloadConfig{}},
	}
}
