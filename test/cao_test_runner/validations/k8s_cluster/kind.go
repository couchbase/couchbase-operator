package k8sclustervalidator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kind"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/nodes"
	"github.com/sirupsen/logrus"
)

var (
	ErrInvalidKindConfig            = errors.New("kind config is nil, testAssets does not hold cluster details. Validator does not know what to validate")
	ErrClusterDoesntExists          = errors.New("cluster doesn't exists")
	ErrNumNodesMismatch             = errors.New("number of nodes mismatch")
	ErrNumControlPlaneNodesMismatch = errors.New("number of control plane nodes mismatch")
	ErrNumWorkerNodesMismatch       = errors.New("number of worker nodes mismatch")
	ErrServiceProviderMismatch      = errors.New("service provider mismatch")
	ErrTestAssetsNotPopulated       = errors.New("test assets not populated properly")
)

const (
	kindPlatform    = assets.Kubernetes
	kindEnvironment = assets.Kind
	kindProvider    = ""
)

type KindConfig struct {
	NumControlPlane *int `yaml:"numControlPlane"`
	NumWorkers      *int `yaml:"numWorkers"`
}

type ValidateKindCluster struct {
	ClusterName string
	KindConfig  *KindConfig
}

// TODO : Also check if all the nodes are healthy and ready
// TODO : Validate all values of KindConfig before validating with cluster. Reduces kind calls and makes sure the yaml is correct.
func (c *ValidateKindCluster) ValidateCluster(_ context.Context, testAssets assets.TestAssetGetterSetter) error {
	/*
		If KindConfig is nil and testAssets does not hold kindClusterDetail for the cluster
			The validator does not know what to validate and will error out

		If KindConfig is nil and testAssets does hold kindClusterDetail for the cluster
			The validator checks and validates if the k8s environment is as per state held in testAssets and kindClusterDetail

		If KindConfig is not nil and testAssets does not hold kindClusterDetail for the cluster
			The kind cluster is a new cluster and did not exist previously. Validator should validate the new cluster according to
			the config

		If KindConfig is not nil and testAssets does hold kindClusterDetail for the cluster
			There are changes to the cluster. Validator should validate the new cluster according to the config.
	*/

	/*
		Validator validates the following
		 - If the cluster exists
		 - If there are right number of control plane nodes
		 - If there are right number of worker nodes
		 - If the kind environment is as per the testAssets state
	*/

	cluster, err := testAssets.GetK8SClustersGetterSetter().GetKindClusterDetailGetterSetter(c.ClusterName)
	if err != nil {
		if checkKindConfigNil(c.KindConfig) {
			return fmt.Errorf("validate kind cluster: %w", ErrInvalidKindConfig)
		}
	}

	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		if checkKindConfigNil(c.KindConfig) {
			return fmt.Errorf("validate kind cluster: %w", ErrInvalidKindConfig)
		}
	}

	if (cluster == nil && k8sCluster != nil) || (cluster != nil && k8sCluster == nil) {
		return fmt.Errorf("validate kind cluster: %w", ErrTestAssetsNotPopulated)
	}

	// When KindConfig is nil and testAssets does hold kindClusterDetail for the cluster
	if checkKindConfigNil(c.KindConfig) && k8sCluster != nil {
		logrus.Infof("Validating Previous State of Kind Cluster %s", c.ClusterName)
		if err := c.ValidatePrevState(testAssets); err != nil {
			return fmt.Errorf("validate kind cluster: %w", err)
		}
	}

	// When KindConfig is not nil and testAssets does not hold kindClusterDetail for the cluster
	if !checkKindConfigNil(c.KindConfig) && cluster == nil {
		logrus.Infof("Validating New Kind Cluster %s", c.ClusterName)
		if err := c.ValidateNewCluster(testAssets); err != nil {
			return fmt.Errorf("validate kind cluster: %w", err)
		}
	}

	// When KindConfig is not nil and testAssets does hold kindClusterDetail for the cluster
	if !checkKindConfigNil(c.KindConfig) && cluster != nil {
		logrus.Infof("Validating Updated State of Kind Cluster %s", c.ClusterName)
		if err := c.ValidateUpdateCluster(testAssets); err != nil {
			return fmt.Errorf("validate kind cluster: %w", err)
		}
	}

	logrus.Infof("Cluster %s successfully validated in Kind", c.ClusterName)

	return nil
}

func (c *ValidateKindCluster) ValidatePrevState(testAssets assets.TestAssetGetterSetter) error {
	out, _, err := kind.GetClusters().ExecWithOutputCapture()
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	allClusters := strings.Split(out, "\n")

	if !contains(allClusters, c.ClusterName) {
		return fmt.Errorf("validate prev state: %w", ErrClusterDoesntExists)
	}

	if err := checkIfKindClusterExistsInKubeconfig(c.ClusterName); err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	cluster, err := testAssets.GetK8SClustersGetterSetter().GetKindClusterDetailGetterSetter(c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	if k8sCluster.GetServiceProvider().GetPlatform() != kindPlatform || k8sCluster.GetServiceProvider().GetEnvironment() != kindEnvironment {
		return fmt.Errorf("validate prev state: %w", ErrServiceProviderMismatch)
	}

	allNodes, err := nodes.GetNodeNames()
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	var controlPlaneNodes, workerNodes []*string
	for _, node := range allNodes {
		if strings.Contains(node, "control-plane") {
			controlPlaneNodes = append(controlPlaneNodes, &node)
		} else {
			workerNodes = append(workerNodes, &node)
		}
	}

	if len(controlPlaneNodes) != len(cluster.GetAllControlPlaneNodes()) {
		return fmt.Errorf("validate prev state: %w", ErrNumControlPlaneNodesMismatch)
	}

	if len(workerNodes) != len(cluster.GetAllWorkerNodes()) {
		return fmt.Errorf("validate prev state: %w", ErrNumWorkerNodesMismatch)
	}

	if len(controlPlaneNodes)+len(workerNodes) != len(k8sCluster.GetNodes()) {
		return fmt.Errorf("validate prev state: %w", ErrNumNodesMismatch)
	}

	for _, node := range controlPlaneNodes {
		if !containsStrPtr(cluster.GetAllControlPlaneNodes(), node) {
			return fmt.Errorf("validate prev state: %w", ErrNumControlPlaneNodesMismatch)
		}

		if !containsStrPtr(k8sCluster.GetNodes(), node) {
			return fmt.Errorf("validate prev state: %w", ErrNumControlPlaneNodesMismatch)
		}
	}

	for _, node := range workerNodes {
		if !containsStrPtr(cluster.GetAllWorkerNodes(), node) {
			return fmt.Errorf("validate prev state: %w", ErrNumWorkerNodesMismatch)
		}

		if !containsStrPtr(k8sCluster.GetNodes(), node) {
			return fmt.Errorf("validate prev state: %w", ErrNumWorkerNodesMismatch)
		}
	}

	return nil
}

func (c *ValidateKindCluster) ValidateNewCluster(testAssets assets.TestAssetGetterSetter) error {
	out, _, err := kind.GetClusters().ExecWithOutputCapture()
	if err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	allClusters := strings.Split(out, "\n")

	if !contains(allClusters, c.ClusterName) {
		return fmt.Errorf("validate new cluster: %w", ErrClusterDoesntExists)
	}

	if err := checkIfKindClusterExistsInKubeconfig(c.ClusterName); err != nil {
		return fmt.Errorf("validate new state: %w", err)
	}

	allNodes, err := nodes.GetNodeNames()
	if err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	var controlPlaneNodes, workerNodes []*string
	for _, node := range allNodes {
		if strings.Contains(node, "control-plane") {
			controlPlaneNodes = append(controlPlaneNodes, &node)
		} else {
			workerNodes = append(workerNodes, &node)
		}
	}

	if len(controlPlaneNodes) != *c.KindConfig.NumControlPlane {
		return fmt.Errorf("validate new cluster: %w", ErrNumControlPlaneNodesMismatch)
	}

	if len(workerNodes) != *c.KindConfig.NumWorkers {
		return fmt.Errorf("validate new cluster: %w", ErrNumWorkerNodesMismatch)
	}

	if err := testAssets.GetK8SClustersGetterSetter().SetKindClusterDetail(
		assets.NewKindClusterDetail(c.ClusterName, controlPlaneNodes, workerNodes)); err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	serviceProvider := assets.NewManagedServiceProvider(kindPlatform, kindEnvironment, kindProvider)

	if err := testAssets.GetK8SClustersGetterSetter().SetK8SCluster(
		assets.NewK8SCluster(c.ClusterName, serviceProvider, append(controlPlaneNodes, workerNodes...))); err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	return nil
}

func (c *ValidateKindCluster) ValidateUpdateCluster(testAssets assets.TestAssetGetterSetter) error {
	out, _, err := kind.GetClusters().ExecWithOutputCapture()
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	allClusters := strings.Split(out, "\n")

	if !contains(allClusters, c.ClusterName) {
		return fmt.Errorf("validate update cluster: %w", ErrClusterDoesntExists)
	}

	if err := checkIfKindClusterExistsInKubeconfig(c.ClusterName); err != nil {
		return fmt.Errorf("validate update state: %w", err)
	}

	cluster, err := testAssets.GetK8SClustersGetterSetter().GetKindClusterDetailGetterSetter(c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	if k8sCluster.GetServiceProvider().GetPlatform() != kindPlatform || k8sCluster.GetServiceProvider().GetEnvironment() != kindEnvironment {
		return fmt.Errorf("validate update cluster: %w", ErrServiceProviderMismatch)
	}

	allNodes, err := nodes.GetNodeNames()
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	var controlPlaneNodes, workerNodes []*string
	for _, node := range allNodes {
		if strings.Contains(node, "control-plane") {
			controlPlaneNodes = append(controlPlaneNodes, &node)
		} else {
			workerNodes = append(workerNodes, &node)
		}
	}

	if len(controlPlaneNodes) != *c.KindConfig.NumControlPlane {
		return fmt.Errorf("validate update cluster: %w", ErrNumControlPlaneNodesMismatch)
	}

	if len(workerNodes) != *c.KindConfig.NumWorkers {
		return fmt.Errorf("validate update cluster: %w", ErrNumWorkerNodesMismatch)
	}

	if err := cluster.SetControlPlaneNodes(controlPlaneNodes); err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	if err := cluster.SetWorkerNodes(workerNodes); err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	if err := k8sCluster.SetNodes(append(controlPlaneNodes, workerNodes...)); err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	return nil
}

func checkIfKindClusterExistsInKubeconfig(clusterName string) error {
	out, _, err := kubectl.GetClusters().ExecWithOutputCapture()
	if err != nil {
		return fmt.Errorf("check if kind cluster exists in kubeconfig: %w", err)
	}

	allClusters := strings.Split(out, "\n")
	allClusters = allClusters[:len(allClusters)-1]

	if !strings.Contains(clusterName, "kind-") {
		clusterName = "kind-" + clusterName
	}

	if !contains(allClusters, clusterName) {
		return fmt.Errorf("check if kind cluster exists in kubeconfig: %w", ErrClusterKubeconfigDoesntExists)
	}

	return nil
}

func checkKindConfigNil(kindConfig *KindConfig) bool {
	if kindConfig == nil {
		return true
	}

	if kindConfig.NumControlPlane == nil || kindConfig.NumWorkers == nil {
		return true
	}

	return false
}
