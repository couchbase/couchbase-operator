package namespacevalidator

import (
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/pods"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/services"
	"github.com/sirupsen/logrus"
)

var (
	ErrIllegalConfig         = errors.New("illegal config, no namespace to validate")
	ErrNamespaceNameMismatch = errors.New("namespace name mismatch")
	ErrPodsMismatch          = errors.New("pods mismatch in namespace")
	ErrServicesMismatch      = errors.New("services mismatch in namespace")
	ErrNamespaceMismatch     = errors.New("namespace mismatch")
)

type NamespaceValidator struct {
	Name        string    `yaml:"name" caoCli:"required"`
	ClusterName string    `yaml:"clusterName" caoCli:"required,context"`
	State       string    `yaml:"state" caoCli:"required"`
	Namespaces  []*string `yaml:"namespaces"`
}

func (c *NamespaceValidator) Run(ctx *context.Context, testAssets assets.TestAssetGetterSetter) error {
	/*
		If validator param namespaces is nil and testAssets does not hold any namespace for the cluster
			The validator does not know what to validate and will error out

		If validator param namespace is nil and testAssets does hold some namespaces for the cluster
			The validator checks and validates if the k8s environment namespaces is as per state held in testAssets

		If validator param namespace is not nil and testAssets does not hold those particular namespaces for the cluster
			It is a new namespace and did not exist previously. Validator should validate the new namespace according to
			the config

		If validator param namespace is not nil and testAssets does hold the same namespaces for the cluster
			Validator validates only these namespaces and not all the namespaces
	*/

	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		// Cluster does not exist
		return fmt.Errorf("validator run: %w", err)
	}

	namespaces := k8sCluster.GetAllNamespacesGetterSetters()

	// When testAssets does not know any namespaces and no namespaces were specified in yaml config
	if len(c.Namespaces) == 0 && len(namespaces) == 0 {
		// No namespace to validate
		return fmt.Errorf("validator run: %w", ErrIllegalConfig)
	}

	// The validator param namespace is nil, but testAssets does hold some namespaces for the cluster
	if len(c.Namespaces) == 0 {
		for _, namespace := range namespaces {
			if err := c.ValidatePreviousState(ctx, namespace.GetNamespaceName(), testAssets); err != nil {
				return fmt.Errorf("validator run: %w", err)
			}

			logrus.Infof("Namespace %s successfully validated", namespace.GetNamespaceName())
		}
	} else {
		// The validator param namespace is not nil, validator validates only these namespaces
		for _, namespace := range c.Namespaces {
			if _, err := k8sCluster.GetNamespaceGetterSetter(*namespace); err != nil {
				// Validator not in test assets, is a new namespace
				if err := c.ValidateNewNamespace(ctx, *namespace, testAssets); err != nil {
					return fmt.Errorf("validator run: %w", err)
				}

				logrus.Infof("Namespace %s successfully validated", *namespace)
			} else {
				// Validator in test assets, is a known namespace, validate old namespace
				if err := c.ValidatePreviousState(ctx, *namespace, testAssets); err != nil {
					return fmt.Errorf("validator run: %w", err)
				}

				logrus.Infof("Namespace %s successfully validated", *namespace)
			}
		}
	}

	return nil
}

func (c *NamespaceValidator) ValidatePreviousState(ctx *context.Context, namespaceName string, testAssets assets.TestAssetGetterSetter) error {
	logrus.Infof("Validating previous state for namespace %s", namespaceName)

	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		// Cluster not in testAssets
		return fmt.Errorf("validate previous state: %w", err)
	}

	namespace, err := k8sCluster.GetNamespaceGetterSetter(namespaceName)
	if err != nil {
		// Namespace not in testAssets
		return fmt.Errorf("validate previous state: %w", err)
	}

	if namespace.GetNamespaceName() != namespaceName {
		return fmt.Errorf("validate previous state: %w", ErrNamespaceNameMismatch)
	}

	out, _, err := kubectl.GetNamespaces().ExecWithOutputCapture()
	if err != nil {
		return fmt.Errorf("validate previous state: %w", err)
	}

	allNamespaces := strings.Split(out, "\n")
	// Remove the last empty string
	allNamespaces = allNamespaces[:len(allNamespaces)-1]

	for i, namespace := range allNamespaces {
		allNamespaces[i] = strings.TrimPrefix(namespace, "namespace/")
	}

	if !contains(allNamespaces, namespaceName) {
		return fmt.Errorf("validate previous state: %w", ErrNamespaceMismatch)
	}

	allPods, err := pods.GetPodNames(namespaceName)
	if err != nil && !errors.Is(err, pods.ErrNoPodsInNamespace) {
		return fmt.Errorf("validate previous state: %w", err)
	}

	allServices, err := services.GetServiceNames(namespaceName)
	if err != nil && !errors.Is(err, services.ErrNoServicesInNamespace) {
		return fmt.Errorf("validate previous state: %w", err)
	}

	assetPods := namespace.GetAllPods()

	assetServices := namespace.GetAllServices()

	if len(allPods) != len(assetPods) {
		return fmt.Errorf("validate previous state: %w", ErrPodsMismatch)
	}

	for _, pod := range assetPods {
		if !contains(allPods, *pod) {
			return fmt.Errorf("validate previous state: %w", ErrPodsMismatch)
		}
	}

	if len(allServices) != len(assetServices) {
		return fmt.Errorf("validate previous state: %w", ErrServicesMismatch)
	}

	for _, service := range assetServices {
		if !contains(allServices, *service) {
			return fmt.Errorf("validate previous state: %w", ErrServicesMismatch)
		}
	}

	return nil
}

func (c *NamespaceValidator) ValidateNewNamespace(ctx *context.Context, namespaceName string, testAssets assets.TestAssetGetterSetter) error {
	logrus.Infof("Validating new namespace %s", namespaceName)

	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		// Cluster not in testAssets
		return fmt.Errorf("validate new namespace: %w", err)
	}

	if _, err := k8sCluster.GetNamespaceGetterSetter(namespaceName); err == nil {
		// Namespace in testAssets
		return fmt.Errorf("validate new namespace: %w", ErrIllegalConfig)
	}

	out, _, err := kubectl.GetNamespaces().ExecWithOutputCapture()
	if err != nil {
		return fmt.Errorf("validate new namespace: %w", err)
	}

	allNamespaces := strings.Split(out, "\n")
	// Remove the last empty string
	allNamespaces = allNamespaces[:len(allNamespaces)-1]

	for i, namespace := range allNamespaces {
		allNamespaces[i] = strings.TrimPrefix(namespace, "namespace/")
	}

	if !contains(allNamespaces, namespaceName) {
		return fmt.Errorf("validate new namespace: %w", ErrNamespaceMismatch)
	}

	allPods, err := pods.GetPodNames(namespaceName)
	if err != nil && !errors.Is(err, pods.ErrNoPodsInNamespace) {
		return fmt.Errorf("validate new namespace: %w", err)
	}

	var pods []*string

	for _, pod := range allPods {
		pods = append(pods, &pod)
	}

	allServices, err := services.GetServiceNames(namespaceName)
	if err != nil && !errors.Is(err, services.ErrNoServicesInNamespace) {
		return fmt.Errorf("validate new namespace: %w", err)
	}

	var services []*string

	for _, service := range allServices {
		services = append(services, &service)
	}

	namespace := assets.NewNamespace(namespaceName, pods, services)

	if err := k8sCluster.SetNamespaces(namespace); err != nil {
		return fmt.Errorf("validate new namespace: %w", err)
	}

	return nil
}

func (c *NamespaceValidator) GetState() string {
	return c.State
}

func contains(array []string, str string) bool {
	for _, item := range array {
		if item == str {
			return true
		}
	}

	return false
}
