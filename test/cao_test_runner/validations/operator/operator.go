package operatorvalidator

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	caopods "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/cao_pods"
)

var (
	ErrIllegalConfig         = errors.New("illegal config, no operator pods to validate")
	ErrNotImplemented        = errors.New("not implemented")
	ErrOperatorImageMismatch = errors.New("operator image mismatch")
	ErrOperatorMismatch      = errors.New("operator mismatch")
	ErrValueNotStruct        = errors.New("value is not a struct")
	ErrFieldNotNil           = errors.New("field is not nil")
)

type OperatorConfig struct {
	OperatorImage *string `yaml:"operatorImage" env:"OPERATOR_IMAGE"`
	Scope         *string `yaml:"scope"`
}

type OperatorValidator struct {
	Name           string          `yaml:"name" caoCli:"required"`
	ClusterName    string          `yaml:"clusterName" caoCli:"required,context"`
	Namespace      string          `yaml:"namespace" caoCli:"required,context"`
	State          string          `yaml:"state" caoCli:"required"`
	OperatorConfig *OperatorConfig `yaml:"operatorConfig"`
}

func (c *OperatorValidator) Run(ctx *context.Context, testAssets assets.TestAssetGetterSetter) error {
	/*
		If validator param operatorConfig is nil and testAssets does not hold any operator
			The validator does not know what to validate and will error out

		If validator param operatorConfig is nil and testAssets does hold some operator
			The validator checks and validates if the operator is as per state held in testAssets

		If validator param operatorConfig is not nil and testAssets does not hold any operator
			It is a new operator and did not exist previously. Validator should validate the new operator pod according to
			the config

		If validator param operatorConfig is not nil and testAssets does hold operator
			Validator validates the operator pod according to the new config (Could be an update)

		No support for operators scoped to cluster
	*/

	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		// Cluster does not exist
		return fmt.Errorf("validator run: %w", err)
	}

	operatorPods := k8sCluster.GetAllOperatorPodsGetterSetter()

	var operatorPod assets.OperatorPodGetterSetter

	// Fetching the operator pod in current namespace
	for _, pod := range operatorPods {
		if pod.GetNamespace() == c.Namespace {
			operatorPod = pod
		}
	}

	configNil, _ := checkConfigIsNil(c.OperatorConfig)

	// No operator pods in testAssets and no info in operatorConfig
	if operatorPod == nil && configNil {
		return fmt.Errorf("validator run: %w", ErrIllegalConfig)
	}

	// New operator pod
	if operatorPod == nil && !configNil {
		if err := c.ValidateNewOperator(testAssets); err != nil {
			return fmt.Errorf("validator run: %w", err)
		}
	}

	// Check previous state of these operator pods
	if operatorPod != nil && configNil {
		if err := c.ValidateOperatorPreviousState(testAssets); err != nil {
			return fmt.Errorf("validator run: %w", err)
		}
	}

	// Update operator pod
	if operatorPod != nil && !configNil {
		if err := c.ValidateOperatorUpdate(testAssets); err != nil {
			return fmt.Errorf("validator run: %w", err)
		}
	}

	return nil
}

func (c *OperatorValidator) ValidateNewOperator(testAssets assets.TestAssetGetterSetter) error {
	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		// Cluster does not exist
		return fmt.Errorf("validate new operator: %w", err)
	}

	operatorPod, err := caopods.GetOperatorPod(c.Namespace)
	if err != nil {
		return fmt.Errorf("validate new operator: %w", err)
	}

	if c.OperatorConfig.Scope == nil || c.OperatorConfig.OperatorImage == nil {
		return fmt.Errorf("validate new operator: %w", ErrIllegalConfig)
	}

	if operatorPod.Spec.Containers[0].Image != *c.OperatorConfig.OperatorImage {
		return fmt.Errorf("validate new operator: %w", ErrOperatorImageMismatch)
	}

	// TODO: Once dev gets back on adding an annotation, etc for scope, add that here

	assetOperatorPod, err := assets.NewOperatorPod(operatorPod.Metadata.Name,
		*c.OperatorConfig.OperatorImage, c.Namespace, assets.ScopeType(*c.OperatorConfig.Scope))
	if err != nil {
		return fmt.Errorf("validate new operator: %w", err)
	}

	if err := k8sCluster.SetOperatorPod(assetOperatorPod); err != nil {
		return fmt.Errorf("validate new operator: %w", err)
	}

	return nil
}

func (c *OperatorValidator) ValidateOperatorPreviousState(testAssets assets.TestAssetGetterSetter) error {
	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		// Cluster does not exist
		return fmt.Errorf("validator operator previous state: %w", err)
	}

	operatorPods := k8sCluster.GetAllOperatorPodsGetterSetter()

	var assetOperatorPod assets.OperatorPodGetterSetter

	for _, pod := range operatorPods {
		if pod.GetNamespace() == c.Namespace {
			// Operator pod scoped to current namespace
			assetOperatorPod = pod
		}
	}

	// TODO: Once dev gets back on adding an annotation, etc for scope, add that here

	operatorPod, err := caopods.GetOperatorPod(c.Namespace)
	if err != nil {
		return fmt.Errorf("validator operator previous state: %w", err)
	}

	if operatorPod.Metadata.Name != assetOperatorPod.GetOperatorPodName() {
		return fmt.Errorf("validator operator previous state: %w", ErrOperatorMismatch)
	}

	if operatorPod.Spec.Containers[0].Image != assetOperatorPod.GetOperatorImage() {
		return fmt.Errorf("validator operator previous state: %w", ErrOperatorImageMismatch)
	}

	return nil
}

func (c *OperatorValidator) ValidateOperatorUpdate(testAssets assets.TestAssetGetterSetter) error {
	return ErrNotImplemented
}

func (c *OperatorValidator) GetState() string {
	return c.State
}

func checkConfigIsNil(v interface{}) (bool, error) {
	val := reflect.ValueOf(v)

	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return false, fmt.Errorf("check config nil: %w", ErrValueNotStruct)
	}

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)

		if field.Kind() == reflect.Ptr && field.IsNil() {
			continue
		}
		if field.Kind() == reflect.Slice && field.Len() == 0 {
			continue
		}

		return false, fmt.Errorf("check config nil: field %s is not nil: %w", val.Type().Field(i).Name, ErrFieldNotNil)
	}

	return true, nil
}
