package task

import (
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	changekubeconfig "github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/change_config/kubeconfig"
	changenamespace "github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/change_config/namespace"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/chaos"
	chaosbaremetal "github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/chaos_bare_metal"
	destroyadmissioncontroller "github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/destroy/admission_controller"
	destroycrd "github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/destroy/crd"
	destroykubernetes "github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/destroy/kubernetes"
	destoryoperator "github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/destroy/operator"
	labeltaintnodes "github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/k8s/label_taint_nodes"
	admissioncontrollersetup "github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/setup/admission_controller"
	caocrdsetup "github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/setup/cao_crd"
	couchbasesetup "github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/setup/couchbase"
	setupkubernetes "github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/setup/kubernetes"
	namespacesetup "github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/setup/namespace"
	operatorsetup "github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/setup/operator"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/upgrade"
	upgradekubernetes "github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/upgrade/kubernetes"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/workloads"
	dataworkloads "github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/workloads/data_workloads"
	indexworkloads "github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/workloads/index_workloads"
	queryworkloads "github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/workloads/query_workloads"
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
		"Setup Kubernetes Cluster":         {newAction: setupkubernetes.NewKubernetesConfig, config: &setupkubernetes.KubernetesSetupConfig{}},
		"Setup Operator":                   {newAction: operatorsetup.NewSetupOperatorConfig, config: &operatorsetup.OperatorConfig{}},
		"Setup Admission Controller":       {newAction: admissioncontrollersetup.NewSetupAdmissionControllerConfig, config: &admissioncontrollersetup.AdmissionControllerConfig{}},
		"Setup CAO Binary and Deploy CRDs": {newAction: caocrdsetup.NewCaoCrdSetupConfig, config: &caocrdsetup.CaoCrdSetupConfig{}},
		"Create Namespace":                 {newAction: namespacesetup.NewSetupNamespaceConfig, config: &namespacesetup.SetupNamespaceConfig{}},
		"Deploy Couchbase":                 {newAction: couchbasesetup.NewCouchbaseConfig, config: &couchbasesetup.CouchbaseConfig{}},
		"Sleep":                            {newAction: workloads.NewSleepActionConfig, config: &workloads.SleepActionConfig{}},
		"Generic Workload":                 {newAction: workloads.NewGenericWorkloadConfig, config: &workloads.GenericWorkloadConfig{}},
		"Change Kubeconfig Context":        {newAction: changekubeconfig.NewKubeConfigContextChangeConfig, config: &changekubeconfig.KubeConfigContextChangeConfig{}},
		"Change Current Namespace":         {newAction: changenamespace.NewNamespaceChangeConfig, config: &changenamespace.NamespaceChangeConfig{}},
		"Destroy Operator":                 {newAction: destoryoperator.NewDeleteOperatorConfig, config: &destoryoperator.OperatorConfig{}},
		"Destroy Admission Controller":     {newAction: destroyadmissioncontroller.NewDestroyAdmissionControllerConfig, config: &destroyadmissioncontroller.AdmissionControllerConfig{}},
		"Delete CRDs":                      {newAction: destroycrd.NewCRDDestroyConfig, config: &destroycrd.CRDDestroyConfig{}},
		"Destroy Kubernetes Cluster":       {newAction: destroykubernetes.NewKubernetesConfig, config: &destroykubernetes.KubernetesDestroyConfig{}},
		"Data Workload":                    {newAction: dataworkloads.NewDataWorkloadConfig, config: &dataworkloads.DataWorkloadConfig{}},
		"Chaos":                            {newAction: chaos.NewChaosConfig, config: &chaos.ChaosConfig{}},
		"Upgrade Kubernetes Cluster":       {newAction: upgradekubernetes.NewKubernetesUpgradeConfig, config: &upgradekubernetes.KubernetesUpgradeConfig{}},
		"Bare Metal Chaos":                 {newAction: chaosbaremetal.NewChaosBareMetalConfig, config: &chaosbaremetal.BareMetalChaosConfig{}},
		"Index Workload":                   {newAction: indexworkloads.NewIndexWorkloadConfig, config: &indexworkloads.IndexWorkloadConfig{}},
		"Query Workload":                   {newAction: queryworkloads.NewQueryWorkloadConfig, config: &queryworkloads.QueryWorkloadConfig{}},
		"Label Taint Nodes":                {newAction: labeltaintnodes.NewLabelTaintNodeConfig, config: &labeltaintnodes.LabelTaintNodeConfig{}},
	}
}
