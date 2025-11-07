package collector

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"

	stdctx "context"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

const (
	// Secret keys (as defined in the documentation and used by the operator).
	StoreSecretRegion       = "region"
	StoreSecretAccessID     = "access-key-id"
	StoreSecretAccessKey    = "secret-access-key"
	StoreSecretRefreshToken = "refresh-token"
)

// CollectBackupLogs collects cbbackupmgr logs for all backups in the namespace.
// This is a standalone function, not part of the collector framework, as it operates
// on all backups at once rather than per-resource.
// It writes the logs to the provided backend (tar.gz file).
func CollectBackupLogs(ctx *context.Context, b backend.Backend) error {
	backups, err := ctx.CouchbaseClient.CouchbaseV2().CouchbaseBackups(ctx.Namespace()).List(stdctx.Background(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list backups: %w", err)
	}

	collected := 0
	for _, backup := range backups.Items {
		// Filter by name if specified
		if ctx.Config.BackupLogsName != "" && backup.Name != ctx.Config.BackupLogsName {
			continue
		}
		if err := collectBackupLogs(ctx, backup, b); err != nil {
			fmt.Printf("failed to collect logs for backup %s: %v\n", backup.Name, err)
		} else {
			collected++
		}
	}

	if collected == 0 {
		return fmt.Errorf("no backup logs collected")
	}

	return nil
}

func collectBackupLogs(ctx *context.Context, backup couchbasev2.CouchbaseBackup, b backend.Backend) error {
	// Skip ephemeral backups without object store
	if backup.Spec.EphemeralVolume && backup.Spec.ObjectStore == nil {
		fmt.Printf("skipping backup %s: ephemeral volume without object store\n", backup.Name)
		return nil
	}

	// Verify PVC exists
	pvc, err := ctx.KubeClient.CoreV1().PersistentVolumeClaims(ctx.Namespace()).Get(stdctx.Background(), backup.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("PVC not found: %w", err)
	}

	fmt.Printf("collecting logs for backup %s...\n", backup.Name)

	// Create job
	jobName := fmt.Sprintf("backup-logs-collector-%s-%d", backup.Name, time.Now().Unix())
	job, err := buildBackupLogsJob(ctx, jobName, backup, pvc)
	if err != nil {
		return fmt.Errorf("failed to build backup logs job: %w", err)
	}

	if _, err := ctx.KubeClient.BatchV1().Jobs(ctx.Namespace()).Create(stdctx.Background(), job, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("failed to create job: %w", err)
	}

	// Only delete the job if not keeping it for debugging
	if !ctx.Config.BackupLogsKeepJob {
		defer func() {
			if err := ctx.KubeClient.BatchV1().Jobs(ctx.Namespace()).Delete(stdctx.Background(), jobName, metav1.DeleteOptions{
				PropagationPolicy: func() *metav1.DeletionPropagation {
					policy := metav1.DeletePropagationBackground
					return &policy
				}(),
			}); err != nil {
				fmt.Printf("warning: failed to delete job %s: %v\n", jobName, err)
			}
		}()
	} else {
		fmt.Printf("keeping backup logs job %s for debugging (--backup-logs-keep-job)\n", jobName)
	}

	// Wait for pod to reach terminal state to ensure all logs are flushed.
	// We wait for the pod (not the job) because the job status changes to Succeeded
	// before the pod finishes writing to stdout, which can result in truncated logs.
	if err := waitForPodCompletion(ctx, jobName); err != nil {
		return fmt.Errorf("pod completion failed: %w", err)
	}

	// Capture zip data
	zipData, err := captureZipFromLogs(ctx, jobName)
	if err != nil {
		return err
	}

	// Write to backend (tar.gz file).
	path := util.ArchivePath(ctx.Namespace(), "CouchbaseBackup", backup.Name, "logs.zip")
	if err := b.WriteFile(path, string(zipData)); err != nil {
		return fmt.Errorf("failed to write backup logs to archive: %w", err)
	}

	fmt.Printf("✓ collected backup logs for %s (%d bytes)\n", backup.Name, len(zipData))
	return nil
}

// buildCbBackupMgrCommand builds the cbbackupmgr collect-logs command with appropriate arguments.
func buildCbBackupMgrCommand(backup *couchbasev2.CouchbaseBackup) string {
	// Get archive path - default to /data/backups (standard location)
	archivePath := "/data/backups"
	if backup.Status.Archive != "" {
		archivePath = backup.Status.Archive
	}

	// Start building cbbackupmgr arguments
	cbbackupmgrArgs := []string{
		"collect-logs",
		"--archive", archivePath,
		"--output-dir", "/tmp",
	}

	// Add object store args if needed
	if backup.Spec.ObjectStore != nil && backup.Spec.ObjectStore.URI != "" {
		// Use a unique staging directory on the PVC to avoid conflicts
		stagingDir := fmt.Sprintf("/data/staging/collect-logs-%d", time.Now().Unix())
		cbbackupmgrArgs = append(cbbackupmgrArgs, "--obj-staging-dir", stagingDir)

		if backup.Spec.ObjectStore.UseIAM != nil && *backup.Spec.ObjectStore.UseIAM {
			cbbackupmgrArgs = append(cbbackupmgrArgs, "--obj-auth-by-instance-metadata")
		}

		if backup.Spec.ObjectStore.Endpoint != nil {
			if backup.Spec.ObjectStore.Endpoint.URL != "" {
				cbbackupmgrArgs = append(cbbackupmgrArgs, "--obj-endpoint", backup.Spec.ObjectStore.Endpoint.URL)
			}
			if !backup.Spec.ObjectStore.Endpoint.UseVirtualPath {
				cbbackupmgrArgs = append(cbbackupmgrArgs, "--s3-force-path-style")
			}
			if backup.Spec.ObjectStore.Endpoint.CertSecret != "" {
				cbbackupmgrArgs = append(cbbackupmgrArgs, "--obj-cacert", "/var/run/secrets/objectendpoint/tls.crt")
			}
		}

		// Construct the final command string: cbbackupmgr ... > /tmp/log 2>&1 && rm -rf <stagingDir> && cat /tmp/*.zip
		// The redirection and cleanup are applied to the full cbbackupmgr command.
		return fmt.Sprintf("cbbackupmgr %s > /tmp/cbbackupmgr-output.log 2>&1 && rm -rf %s && cat /tmp/*.zip",
			strings.Join(cbbackupmgrArgs, " "), stagingDir)
	}

	// For local backups without object store, construct the command.
	return fmt.Sprintf("cbbackupmgr %s > /tmp/cbbackupmgr-output.log 2>&1 && cat /tmp/*.zip",
		strings.Join(cbbackupmgrArgs, " "))
}

// addCloudCredentials adds cloud object store credentials as environment variables.
func addCloudCredentials(ctx *context.Context, backup *couchbasev2.CouchbaseBackup, container *corev1.Container) {
	if backup.Spec.ObjectStore == nil || backup.Spec.ObjectStore.Secret == "" {
		return
	}

	secretName := backup.Spec.ObjectStore.Secret

	// Fetch secret to check which keys exist (like the operator does)
	secret, err := ctx.KubeClient.CoreV1().Secrets(ctx.Namespace()).Get(stdctx.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		return
	}

	// Add region if present (AWS S3)
	if _, ok := secret.Data[StoreSecretRegion]; ok {
		container.Env = append(container.Env, corev1.EnvVar{
			Name: "CB_OBJSTORE_REGION",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
					Key:                  StoreSecretRegion,
				},
			},
		})
	}

	// Skip access keys if using IAM
	usingIAM := backup.Spec.ObjectStore.UseIAM != nil && *backup.Spec.ObjectStore.UseIAM
	if !usingIAM {
		// Add refresh token if present (GCS)
		if _, ok := secret.Data[StoreSecretRefreshToken]; ok {
			container.Env = append(container.Env, corev1.EnvVar{
				Name: "CB_OBJSTORE_REFRESH_TOKEN",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
						Key:                  StoreSecretRefreshToken,
					},
				},
			})
		}

		// Add access credentials (common for AWS/Azure/GCS)
		container.Env = append(container.Env, []corev1.EnvVar{
			{
				Name: "CB_OBJSTORE_ACCESS_KEY_ID",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
						Key:                  StoreSecretAccessID,
					},
				},
			},
			{
				Name: "CB_OBJSTORE_SECRET_ACCESS_KEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
						Key:                  StoreSecretAccessKey,
					},
				},
			},
		}...)
	}
}

// setupVolumes creates volumes and volume mounts for the backup job.
func setupVolumes(backup *couchbasev2.CouchbaseBackup, pvc *corev1.PersistentVolumeClaim) ([]corev1.Volume, []corev1.VolumeMount) {
	volumes := []corev1.Volume{
		{Name: "backup-data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvc.Name}}},
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "backup-data", MountPath: "/data"},
	}

	// Add endpoint cert if needed
	if backup.Spec.ObjectStore != nil && backup.Spec.ObjectStore.Endpoint != nil && backup.Spec.ObjectStore.Endpoint.CertSecret != "" {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{Name: "endpoint-cert", MountPath: "/var/run/secrets/objectendpoint"})
		volumes = append(volumes, corev1.Volume{Name: "endpoint-cert", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: backup.Spec.ObjectStore.Endpoint.CertSecret}}})
	}

	return volumes, volumeMounts
}

func buildBackupLogsJob(ctx *context.Context, jobName string, backup couchbasev2.CouchbaseBackup, pvc *corev1.PersistentVolumeClaim) (*batchv1.Job, error) {
	// Build the cbbackupmgr command
	cmd := buildCbBackupMgrCommand(&backup)

	// Get cluster configuration
	clusters, err := ctx.CouchbaseClient.CouchbaseV2().CouchbaseClusters(ctx.Namespace()).List(stdctx.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list CouchbaseClusters: %w", err)
	}
	if len(clusters.Items) == 0 {
		return nil, fmt.Errorf("no CouchbaseClusters found in namespace %s", ctx.Namespace())
	}

	cluster := &clusters.Items[0]
	image := cluster.Spec.BackupImage()
	serviceAccount := cluster.Spec.Backup.ServiceAccount

	// Validate that we have required configuration
	if serviceAccount == "" {
		return nil, fmt.Errorf("no service account configured for backup operations")
	}
	if image == "" {
		return nil, fmt.Errorf("no backup image configured in cluster")
	}

	// Set up volumes and mounts
	volumes, volumeMounts := setupVolumes(&backup, pvc)

	// Create container spec
	container := corev1.Container{
		Name:         "backup-logs-collector",
		Image:        image,
		Command:      []string{"sh", "-c"},
		Args:         []string{cmd},
		VolumeMounts: volumeMounts,
		Env:          backup.Spec.Env,
	}

	// Add cloud credentials
	addCloudCredentials(ctx, &backup, &container)

	// Use the cluster's security context to ensure file ownership matches.
	// This is the idiomatic approach used by the operator for backup/restore jobs.
	// The security context determines file ownership on the PVC, so we must match it
	// to read/modify files created by backup jobs (cbbackupmgr collect-logs needs to chmod).
	var securityContext *corev1.PodSecurityContext
	if cluster.Spec.Security.PodSecurityContext != nil {
		securityContext = cluster.Spec.Security.PodSecurityContext
	} else {
		securityContext = cluster.Spec.SecurityContext
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: jobName, Namespace: ctx.Namespace()},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					SecurityContext:    securityContext,
					Containers:         []corev1.Container{container},
					Volumes:            volumes,
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: serviceAccount,
				},
			},
			BackoffLimit: &[]int32{1}[0],
		},
	}, nil
}

// waitForPodCompletion waits for the pod associated with the job to reach a terminal state.
// This ensures all stdout has been flushed before we attempt to read logs.
func waitForPodCompletion(ctx *context.Context, jobName string) error {
	// Find the pod for this job
	pods, err := ctx.KubeClient.CoreV1().Pods(ctx.Namespace()).List(stdctx.Background(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})
	if err != nil || len(pods.Items) == 0 {
		return fmt.Errorf("pod not found for job %s: %w", jobName, err)
	}

	pod := &pods.Items[0]
	podName := pod.Name

	// Check if pod is already in terminal state (avoid race condition with watch)
	if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		return nil
	}

	// Set up watch for pod state changes
	watchCtx, cancel := stdctx.WithTimeout(stdctx.Background(), 2*time.Minute)
	defer cancel()

	watcher, err := ctx.KubeClient.CoreV1().Pods(ctx.Namespace()).Watch(watchCtx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("metadata.name=%s", podName),
	})
	if err != nil {
		return fmt.Errorf("failed to watch pod %s: %w", podName, err)
	}
	defer watcher.Stop()

	for {
		select {
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return fmt.Errorf("watch channel closed unexpectedly for pod %s", podName)
			}

			switch event.Type {
			case watch.Modified:
				pod, ok := event.Object.(*corev1.Pod)
				if !ok {
					return fmt.Errorf("unexpected object type in watch event for pod %s", podName)
				}

				// Wait for pod to reach terminal state (Succeeded or Failed)
				if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
					return nil
				}
			case watch.Deleted:
				return fmt.Errorf("pod %s was deleted before completion", podName)
			}

		case <-watchCtx.Done():
			return watchCtx.Err()
		}
	}
}

func captureZipFromLogs(ctx *context.Context, jobName string) ([]byte, error) {
	// Find pod
	pods, err := ctx.KubeClient.CoreV1().Pods(ctx.Namespace()).List(stdctx.Background(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})
	if err != nil || len(pods.Items) == 0 {
		return nil, fmt.Errorf("pod not found: %w", err)
	}

	// Stream logs (contains zip)
	req := ctx.KubeClient.CoreV1().Pods(ctx.Namespace()).GetLogs(pods.Items[0].Name, &corev1.PodLogOptions{Container: "backup-logs-collector"})
	stream, err := req.Stream(stdctx.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get logs: %w", err)
	}
	defer stream.Close()

	// Read zip data
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, stream); err != nil {
		return nil, fmt.Errorf("failed to read logs: %w", err)
	}

	return buf.Bytes(), nil
}
