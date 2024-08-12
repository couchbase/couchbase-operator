package task

import (
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	admissioncontrollersetup "github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/setup/admission_controller"
	caocrdsetup "github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/setup/cao_crd"
	couchbasesetup "github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/setup/couchbase"
	kubeconfigsetup "github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/setup/kubeconfig"
	operatorsetup "github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/setup/operator"
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
		"Delta Upgrade":                    {newAction: upgrade.NewDeltaRecoveryUpgrade, config: &upgrade.DeltaRecoveryUpgradeConfig{}},
		"Setup Operator":                   {newAction: operatorsetup.NewSetupOperatorConfig, config: &operatorsetup.OperatorConfig{}},
		"Setup Admission Controller":       {newAction: admissioncontrollersetup.NewSetupAdmissionControllerConfig, config: &admissioncontrollersetup.AdmissionControllerConfig{}},
		"Setup CAO Binary and Deploy CRDs": {newAction: caocrdsetup.NewCaoCrdSetupConfig, config: &caocrdsetup.CaoCrdSetupConfig{}},
		"Deploy Couchbase":                 {newAction: couchbasesetup.NewCouchbaseConfig, config: &couchbasesetup.CouchbaseConfig{}},
		"Sleep":                            {newAction: workloads.NewSleepActionConfig, config: &workloads.SleepActionConfig{}},
		"Generic Workload":                 {newAction: workloads.NewGenericWorkloadConfig, config: &workloads.GenericWorkloadConfig{}},
		"Change Kubeconfig Context":        {newAction: kubeconfigsetup.NewKubernetesSetupConfig, config: &kubeconfigsetup.KubeConfigContextSetupConfig{}},
	}
}
