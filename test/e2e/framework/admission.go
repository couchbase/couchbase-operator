package framework

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	"github.com/sirupsen/logrus"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// admissionNamespace must be fixed, this prevents the case where we use
	// the same physical cluster for testing two different operators in different
	// namespaces and we end up with two instances.
	admissionNamespace = "default"
)

// createAdmissionController creates all the necessary resources to deploy the
// admission controller.
func createAdmissionController(k8s *types.Cluster, pullSecrets []string) error {
	// This is as close to the documentation as we can be e.g. validate what we
	// document.
	args := []string{
		"create",
		"admission",
		"--image=" + Global.AdmissionControllerImage,
		"--namespace=" + admissionNamespace,
		"--kubeconfig=" + k8s.KubeConfPath,
	}

	// On Autopilot mutation isn't supported, and stuff moves around, so use
	// multiple replicas in the deployment to keep it working.
	if Global.DynamicPlatform {
		args = append(args, "--with-mutation=false", "--replicas=3")
	}

	if k8s.Context != "" {
		args = append(args, "--context="+k8s.Context)
	}

	for _, secret := range pullSecrets {
		args = append(args, "--image-pull-secret="+secret)
	}

	if Global.PodImagePullPolicy.String() != "" {
		args = append(args, "--image-pull-policy="+Global.PodImagePullPolicy.String())
	}

	output, err := exec.Command("/cao", args...).CombinedOutput()
	if err != nil {
		logrus.Info(string(output))
		logrus.Fatal(err.Error())
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.AdmissionResourceName,
			Namespace: admissionNamespace,
		},
	}

	if err := waitAdmissionController(k8s, deployment); err != nil {
		return err
	}

	return nil
}

// deleteAdmissionController removes any existing admission controller resources.  Just does
// this unconditionally rather than having to check whether it exists or not which is a lot
// of boiler plate zzz.
func deleteAdmissionController(k8s *types.Cluster) error {
	// This is as close to the documentation as we can be e.g. validate what we
	// document.
	args := []string{
		"delete",
		"admission",
		"--namespace=" + admissionNamespace,
		"--kubeconfig=" + k8s.KubeConfPath,
	}

	if k8s.Context != "" {
		args = append(args, "--context="+k8s.Context)
	}

	output, err := exec.Command("/cao", args...).CombinedOutput()
	if err != nil {
		logrus.Info(string(output))
		logrus.Fatal(err.Error())
	}

	// Hack going from <2.3, there will be an old mutating webhook lingering like a
	// bad smell.
	_ = k8s.KubeClient.AdmissionregistrationV1().MutatingWebhookConfigurations().Delete(context.Background(), "couchbase-operator-admission", metav1.DeleteOptions{})

	return nil
}

func waitAdmissionController(k8s *types.Cluster, deployment *appsv1.Deployment) error {
	if err := retryutil.RetryFor(5*time.Minute, e2eutil.ResourceCondition(k8s, deployment, "Available", "True")); err != nil {
		return err
	}

	// Retry an operation that requires the DAC so we know for sure it's up and running.
	callback := func() error {
		bucket := &couchbasev2.CouchbaseBucket{
			ObjectMeta: metav1.ObjectMeta{
				Name: "dry-run",
			},
		}

		createOptions := metav1.CreateOptions{
			DryRun: []string{
				"All",
			},
		}

		newBucket, err := k8s.CRClient.CouchbaseV2().CouchbaseBuckets(admissionNamespace).Create(context.Background(), bucket, createOptions)
		if err != nil {
			return err
		}

		if newBucket.Spec.MemoryQuota == nil {
			return fmt.Errorf("admission defaulting not functional: %v", newBucket)
		}

		return nil
	}

	if err := retryutil.RetryFor(time.Minute, callback); err != nil {
		return err
	}

	return nil
}
