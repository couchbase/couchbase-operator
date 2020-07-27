package e2eutil

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// backoffLimit allows the container to only run once.
var backoffLimit int32 = 0

// jobTemplate is the static part of every SDK job.
var jobTemplate = &batchv1.Job{
	ObjectMeta: metav1.ObjectMeta{
		Name: "sdk",
	},
	Spec: batchv1.JobSpec{
		BackoffLimit: &backoffLimit,
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				Containers: []corev1.Container{
					{
						Name: "sdk",
					},
				},
			},
		},
	},
}

// createSDKJob creates an SDK test job for the specified image with the necessary configuration.
// See inline comments for the API.
func createSDKJob(t *testing.T, remote *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket metav1.Object, image string, tls *TLSContext, cccp bool) *batchv1.Job {
	// Old things do things need to be treated specially.
	service := cluster.Name

	if cccp {
		service = fmt.Sprintf("%s-srv", cluster.Name)
	}

	// Handle plaintext vs TLS.
	scheme := "couchbase"

	if tls != nil {
		scheme = "couchbases"
	}

	connstr := fmt.Sprintf("%s://%s.%s", scheme, service, remote.Namespace)

	// This is the expected interface for an SDK test.  TLS is optional.
	args := []string{
		fmt.Sprintf("-connection=%s", connstr),
		fmt.Sprintf("-username=%s", string(remote.DefaultSecret.Data["username"])),
		fmt.Sprintf("-password=%s", string(remote.DefaultSecret.Data["password"])),
		fmt.Sprintf("-bucket=%s", bucket.GetName()),
	}

	if tls != nil {
		args = append(args, "-cafile=/etc/sdk/ca.pem")
	}

	t.Logf("testing SDK with image \"%s\"", image)
	t.Logf("testing SDK with arguments \"%s\"", strings.Join(args, " "))

	job := jobTemplate.DeepCopy()
	job.Spec.Template.Spec.Containers[0].Image = image
	job.Spec.Template.Spec.Containers[0].Args = args

	if tls != nil {
		job.Spec.Template.Spec.Volumes = []corev1.Volume{
			{
				Name: "config",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: "sdk",
					},
				},
			},
		}

		job.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
			{
				Name:      "config",
				MountPath: "/etc/sdk",
				ReadOnly:  true,
			},
		}
	}

	return job
}

// MustCreateSDKJob creates an SDK test job and sets it running.
func MustCreateSDKJob(t *testing.T, local, remote *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket metav1.Object, image string, tls *TLSContext, cccp bool) *batchv1.Job {
	if tls != nil {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: "sdk",
			},
			Data: map[string][]byte{
				"ca.pem": tls.CA.Certificate,
			},
		}

		ApplyGarbageCollectedObjectLabels(secret)

		if _, err := local.KubeClient.CoreV1().Secrets(local.Namespace).Create(secret); err != nil {
			Die(t, err)
		}
	}

	job := createSDKJob(t, remote, cluster, bucket, image, tls, cccp)

	ApplyGarbageCollectedObjectLabels(job)

	newJob, err := local.KubeClient.BatchV1().Jobs(local.Namespace).Create(job)
	if err != nil {
		Die(t, err)
	}

	return newJob
}

// MustWaitForSDKJobCompletion waits for the SDK test job to complete.  If it fails then
// die and report the error emitted in the logs, it should be useful enough to debug
// what went wrong!
func MustWaitForSDKJobCompletion(t *testing.T, local *types.Cluster, job *batchv1.Job, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// If we've failed, then we break out of the retry loop in order to
	// fail fast, this flag then controls the handling.
	succeeded := false

	callback := func() error {
		j, err := local.KubeClient.BatchV1().Jobs(local.Namespace).Get(job.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if j.Status.Active > 0 {
			return fmt.Errorf("job active")
		}

		if j.Status.Succeeded > 0 {
			succeeded = true
			return nil
		}

		if j.Status.Failed > 0 {
			return nil
		}

		return fmt.Errorf("unknown status")
	}

	if err := retryutil.RetryOnErr(ctx, time.Second, callback); err != nil {
		Die(t, err)
	}

	if !succeeded {
		t.Logf("SDK logs: %s", getJobLogs(t, local, job))

		Die(t, fmt.Errorf("sdk job failed"))
	}
}

// getJobLogs is a helper to pull off logs as debugging what's wrong is next to
// impossible without this!
func getJobLogs(t *testing.T, local *types.Cluster, job *batchv1.Job) string {
	selector, err := metav1.LabelSelectorAsSelector(job.Spec.Selector)
	if err != nil {
		t.Logf("failed to create sdk job selector: %v", err)
		return ""
	}

	pods, err := local.KubeClient.CoreV1().Pods(local.Namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		t.Logf("failed to list sdk job pods: %v", err)
		return ""
	}

	if len(pods.Items) != 1 {
		t.Logf("unexpected sdk job pods %d", len(pods.Items))
		return ""
	}

	logs := local.KubeClient.CoreV1().Pods(local.Namespace).GetLogs(pods.Items[0].Name, &corev1.PodLogOptions{})

	raw, err := logs.DoRaw()
	if err != nil {
		t.Logf("failed to get sdk job logs: %v", err)
		return ""
	}

	return string(raw)
}

// CleanupSDKResources is a best effort clean up that should be run after each test.
// Note: Most of this goes away with parallel testing.  The rest is handled by doing
// dynamic allocation of resource names, but I'll wait until this all gets cleaned
// down and we can run in parallel.
func CleanupSDKResources(t *testing.T, local *types.Cluster) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	job, err := local.KubeClient.BatchV1().Jobs(local.Namespace).Get("sdk", metav1.GetOptions{})
	if err != nil {
		t.Logf("failed to get sdk job resource: %v", err)
	} else {
		selector, err := metav1.LabelSelectorAsSelector(job.Spec.Selector)
		if err != nil {
			t.Logf("failed to create sdk job selector: %v", err)
		}

		_ = local.KubeClient.CoreV1().Pods(local.Namespace).DeleteCollection(metav1.NewDeleteOptions(0), metav1.ListOptions{LabelSelector: selector.String()})
	}

	_ = local.KubeClient.BatchV1().Jobs(local.Namespace).Delete("sdk", metav1.NewDeleteOptions(0))

	callback := func() error {
		_, err := local.KubeClient.BatchV1().Jobs(local.Namespace).Get("sdk", metav1.GetOptions{})
		if err != nil && errors.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("sdk job resource still exists")
	}

	if err := retryutil.RetryOnErr(ctx, time.Second, callback); err != nil {
		t.Logf("failed to delete sdk job resource: %v", err)
	}

	_ = local.KubeClient.CoreV1().Secrets(local.Namespace).Delete("sdk", metav1.NewDeleteOptions(0))

	callback = func() error {
		_, err := local.KubeClient.CoreV1().Secrets(local.Namespace).Get("sdk", metav1.GetOptions{})
		if err != nil && errors.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("sdk secret resource still exists")
	}

	if err := retryutil.RetryOnErr(ctx, time.Second, callback); err != nil {
		t.Logf("failed to delete sdk secret resource: %v", err)
	}
}
