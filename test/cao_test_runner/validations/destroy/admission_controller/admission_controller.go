package destroyadmissioncontrollervalidator

import (
	"errors"
	"fmt"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	caopods "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/cao_pods"
)

var (
	ErrAdmissionControllerPodsExists      = errors.New("admission controller pods exist")
	ErrAdmissionControllerPodsNotInAssets = errors.New("admission controller pods not in assets")
)

type DestroyAdmissionControllerValidator struct {
	Name        string `yaml:"name" caoCli:"required"`
	Namespace   string `yaml:"namespace" caoCli:"required,context"`
	ClusterName string `yaml:"clusterName" caoCli:"required,context"`
	State       string `yaml:"state" caoCli:"required"`
}

func (c *DestroyAdmissionControllerValidator) Run(ctx *context.Context, testAssets assets.TestAssetGetterSetter) error {
	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		// Cluster does not exist
		return fmt.Errorf("validator run: %w", err)
	}

	admissionPods := k8sCluster.GetAllAdmissionControllerPodsGetterSetter()

	var assetAdmissionPod []assets.AdmissionControllerPodGetterSetter

	for _, pod := range admissionPods {
		if pod.GetNamespace() == c.Namespace {
			assetAdmissionPod = append(assetAdmissionPod, pod)
		}
	}

	if len(assetAdmissionPod) == 0 {
		// Admission controller pods does not exist in testAssets
		return fmt.Errorf("validator run: %w", ErrAdmissionControllerPodsExists)
	}

	admissionControllerPods, err := caopods.GetAdmissionPods(c.Namespace)
	if err != nil {
		return fmt.Errorf("validator run: %w", err)
	}

	if len(admissionControllerPods) != 0 {
		// Admission controller pods still exists
		return fmt.Errorf("validator run: %w", ErrAdmissionControllerPodsNotInAssets)
	}

	for retry := 1; retry <= 3; retry += 1 {
		_, err := caopods.GetAdmissionPods(c.Namespace)
		if err != nil && errors.Is(err, caopods.ErrAdmissionPodDoesntExist) {
			break
		} else if err != nil {
			return fmt.Errorf("validator run: %w", err)
		} else if retry == 3 {
			return fmt.Errorf("validator run: %w", ErrAdmissionControllerPodsExists)
		} else {
			time.Sleep(5 * time.Second)
		}
	}

	for _, assetPod := range assetAdmissionPod {
		if err := k8sCluster.DeleteAdmissionControllerPod(assetPod.GetAdmissionControllerPodName()); err != nil {
			return fmt.Errorf("validator run: %w", err)
		}
	}

	return nil
}

func (c *DestroyAdmissionControllerValidator) GetState() string {
	return c.State
}
