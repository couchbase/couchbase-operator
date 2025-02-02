package admissioncontrollervalidator

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	caopods "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/cao_pods"
)

var (
	ErrIllegalConfig                    = errors.New("illegal config, no admission controller pods to validate")
	ErrNotImplemented                   = errors.New("not implemented")
	ErrAdmissionControllerImageMismatch = errors.New("admission controller image mismatch")
	ErrAdmissionControllerMismatch      = errors.New("admission controller mismatch")
	ErrValueNotStruct                   = errors.New("value is not a struct")
	ErrFieldNotNil                      = errors.New("field is not nil")
)

type AdmissionControllerConfig struct {
	AdmissionControllerImage *string `yaml:"admissionControllerImage" env:"ADMISSION_CONTROLLER_IMAGE"`
	Scope                    *string `yaml:"scope"`
	Replicas                 *int    `yaml:"replicas"`
}

type AdmissionControllerValidator struct {
	Name                      string                     `yaml:"name" caoCli:"required"`
	ClusterName               string                     `yaml:"clusterName" caoCli:"required,context"`
	Namespace                 string                     `yaml:"namespace" caoCli:"required,context"`
	State                     string                     `yaml:"state" caoCli:"required"`
	AdmissionControllerConfig *AdmissionControllerConfig `yaml:"admissionControllerConfig"`
}

func (c *AdmissionControllerValidator) Run(ctx *context.Context, testAssets assets.TestAssetGetterSetter) error {
	/*
		If validator param AdmissionControllerConfig is nil and testAssets does not hold any admission controller
			The validator does not know what to validate and will error out

		If validator param AdmissionControllerConfig is nil and testAssets does hold some admission controllers
			The validator checks and validates if the admission controllers are as per state held in testAssets

		If validator param AdmissionControllerConfig is not nil and testAssets does not hold admission controllers
			It is a new admission controller and did not exist previously. Validator should validate the new admission controller pods according to
			the config

		If validator param AdmissionControllerConfig is not nil and testAssets does hold admission controllers
			Validator validates the admission controller pods according to the new config (Could be an update)

		No support for admission controller scoped to namespace. Only cluster scoped admission controllers are supported
	*/

	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		// Cluster does not exist
		return fmt.Errorf("validator run: %w", err)
	}

	admissionControllerPods := k8sCluster.GetAllAdmissionControllerPodsGetterSetter()

	var admissionPods []assets.AdmissionControllerPodGetterSetter

	configNil, _ := checkConfigIsNil(c.AdmissionControllerConfig)

	for _, pod := range admissionControllerPods {
		if pod.GetNamespace() == c.Namespace {
			admissionPods = append(admissionPods, pod)
		}
	}

	// No Admission pods in testAssets and no info in operatorConfig
	if len(admissionPods) == 0 && configNil {
		return fmt.Errorf("validator run: %w", ErrIllegalConfig)
	}

	// New admission pods
	if len(admissionPods) == 0 && !configNil {
		if err := c.ValidateNewAdmissionController(testAssets); err != nil {
			return fmt.Errorf("validator run: %w", err)
		}
	}

	// Check previous state of these admission pods
	if len(admissionPods) != 0 && configNil {
		if err := c.ValidateAdmissionControllerPreviousState(testAssets); err != nil {
			return fmt.Errorf("validator run: %w", err)
		}
	}

	// Update admission pods
	if len(admissionPods) != 0 && !configNil {
		if err := c.ValidateAdmissionControllerUpdate(testAssets); err != nil {
			return fmt.Errorf("validator run: %w", err)
		}
	}

	return nil
}

func (c *AdmissionControllerValidator) ValidateNewAdmissionController(testAssets assets.TestAssetGetterSetter) error {
	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		// Cluster does not exist
		return fmt.Errorf("validate new admission controller: %w", err)
	}

	if c.AdmissionControllerConfig.AdmissionControllerImage == nil || c.AdmissionControllerConfig.Scope == nil || c.AdmissionControllerConfig.Replicas == nil {
		return fmt.Errorf("validate new admission controller: %w", ErrIllegalConfig)
	}

	admissionControllerPods, err := caopods.GetAdmissionPods(c.Namespace)
	if err != nil {
		return fmt.Errorf("validate new admission controller: %w", err)
	}

	if len(admissionControllerPods) != *c.AdmissionControllerConfig.Replicas {
		return fmt.Errorf("validate new admission controller: %w", ErrAdmissionControllerMismatch)
	}

	for _, pod := range admissionControllerPods {
		if pod.Spec.Containers[0].Image != *c.AdmissionControllerConfig.AdmissionControllerImage {
			return fmt.Errorf("validate new admission controller: %w", ErrAdmissionControllerImageMismatch)
		}

		assetAdmissionControllerPod, err := assets.NewAdmissionControllerPod(pod.Metadata.Name,
			*c.AdmissionControllerConfig.AdmissionControllerImage, c.Namespace,
			assets.ScopeType(*c.AdmissionControllerConfig.Scope), *c.AdmissionControllerConfig.Replicas)
		if err != nil {
			return fmt.Errorf("validate new admission controller: %w", err)
		}

		if err := k8sCluster.SetAdmissionControllerPod(assetAdmissionControllerPod); err != nil {
			return fmt.Errorf("validate new admission controller: %w", err)
		}
	}

	return nil
}

func (c *AdmissionControllerValidator) ValidateAdmissionControllerPreviousState(testAssets assets.TestAssetGetterSetter) error {
	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		// Cluster does not exist
		return fmt.Errorf("validator admission controller previous state: %w", err)
	}

	admissionControllerPods := k8sCluster.GetAllAdmissionControllerPodsGetterSetter()

	var assetAdmissionControllerPods []assets.AdmissionControllerPodGetterSetter

	for _, pod := range admissionControllerPods {
		if pod.GetNamespace() == c.Namespace {
			assetAdmissionControllerPods = append(assetAdmissionControllerPods, pod)
		}
	}

	admissionPods, err := caopods.GetAdmissionPods(c.Namespace)
	if err != nil {
		return fmt.Errorf("validator admission controller previous state: %w", err)
	}

	if len(admissionPods) != len(assetAdmissionControllerPods) {
		return fmt.Errorf("validator admission controller previous state: %w", ErrAdmissionControllerMismatch)
	}

	if len(admissionPods) == 0 {
		return fmt.Errorf("validator admission controller previous state: no admission controllers to validate: %w", ErrAdmissionControllerMismatch)
	}

	if len(admissionPods) != assetAdmissionControllerPods[0].GetReplicas() {
		return fmt.Errorf("validator admission controller previous state: %w", ErrAdmissionControllerMismatch)
	}

	for _, pod := range admissionPods {
		flag := false
		for _, assetPod := range assetAdmissionControllerPods {
			if assetPod.GetAdmissionControllerPodName() == pod.Metadata.Name {
				if pod.Spec.Containers[0].Image != assetPod.GetAdmissionControllerImage() {
					return fmt.Errorf("validator admission controller previous state: %w", ErrAdmissionControllerImageMismatch)
				}

				if len(admissionPods) != assetPod.GetReplicas() {
					return fmt.Errorf("validator admission controller previous state: %w", ErrAdmissionControllerMismatch)
				}

				flag = true
				break
			}
		}

		if !flag {
			return fmt.Errorf("validator admission controller previous state: %w", ErrAdmissionControllerMismatch)
		}
	}

	return nil
}

func (c *AdmissionControllerValidator) ValidateAdmissionControllerUpdate(testAssets assets.TestAssetGetterSetter) error {
	return ErrNotImplemented
}

func (c *AdmissionControllerValidator) GetState() string {
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
