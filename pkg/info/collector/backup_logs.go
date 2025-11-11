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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	watchtools "k8s.io/client-go/tools/watch"
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

	// Get the cluster configuration for this backup.
	// The operator uses a label selector (cluster.Spec.Backup.Selector) to match backups to clusters.
	// For cao collect-logs, we use a simpler approach: find the cluster that matches the backup's selector,
	// or if no selector is configured, use the first cluster in the namespace.
	cluster, err := getClusterForBackup(ctx, &backup)
	if err != nil {
		return nil, err
	}

	image := cluster.Spec.BackupImage()

	// Validate that we have required configuration
	if image == "" {
		return nil, fmt.Errorf("no backup image configured in cluster %s", cluster.Name)
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
					SecurityContext:  securityContext,
					Containers:       []corev1.Container{container},
					Volumes:          volumes,
					RestartPolicy:    corev1.RestartPolicyNever,
					ImagePullSecrets: cluster.Spec.Backup.ImagePullSecrets,
					// Note: No ServiceAccountName specified - log collection doesn't need K8s API access.
					// The pod will use the namespace's default service account, which is sufficient
					// for mounting secrets/volumes via kubelet. The securityContext handles PVC file permissions.
				},
			},
			BackoffLimit: &[]int32{1}[0],
		},
	}, nil
}

// getClusterForBackup finds the CouchbaseCluster that manages this backup.
// The operator labels the backup PVC with "couchbase_cluster: <cluster-name>".
// We use this label to definitively determine which cluster owns the backup.
func getClusterForBackup(ctx *context.Context, backup *couchbasev2.CouchbaseBackup) (*couchbasev2.CouchbaseCluster, error) {
	// Get the PVC for this backup
	pvcName := backup.Name
	pvc, err := ctx.KubeClient.CoreV1().PersistentVolumeClaims(ctx.Namespace()).Get(
		stdctx.Background(),
		pvcName,
		metav1.GetOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get PVC for backup %s: %w", backup.Name, err)
	}

	// The operator labels the PVC with "couchbase_cluster: <cluster-name>"
	clusterName, ok := pvc.Labels["couchbase_cluster"]
	if !ok || clusterName == "" {
		return nil, fmt.Errorf("PVC %s is missing couchbase_cluster label", pvcName)
	}

	// Get the cluster by name
	cluster, err := ctx.CouchbaseClient.CouchbaseV2().CouchbaseClusters(ctx.Namespace()).Get(
		stdctx.Background(),
		clusterName,
		metav1.GetOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster %s for backup %s: %w", clusterName, backup.Name, err)
	}

	return cluster, nil
}

// waitForPodCompletion waits for the pod associated with the job to reach a terminal state.
// This ensures all stdout has been flushed before we attempt to read logs.
func waitForPodCompletion(ctx *context.Context, jobName string) error {
	labelSelector := fmt.Sprintf("job-name=%s", jobName)

	watchCtx, cancel := stdctx.WithTimeout(stdctx.Background(), 2*time.Minute)
	defer cancel()

	listWatch := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = labelSelector
			return ctx.KubeClient.CoreV1().Pods(ctx.Namespace()).List(watchCtx, options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = labelSelector
			return ctx.KubeClient.CoreV1().Pods(ctx.Namespace()).Watch(watchCtx, options)
		},
	}

	_, err := watchtools.UntilWithSync(
		watchCtx,
		listWatch,
		&corev1.Pod{},
		nil,
		func(event watch.Event) (bool, error) {
			pod, ok := event.Object.(*corev1.Pod)
			if !ok {
				return false, fmt.Errorf("unexpected object type %T while waiting for pod for job %s", event.Object, jobName)
			}

			switch event.Type {
			case watch.Added, watch.Modified:
				switch pod.Status.Phase {
				case corev1.PodSucceeded:
					return true, nil
				case corev1.PodFailed:
					return true, fmt.Errorf("pod %s failed", pod.Name)
				default:
					return false, nil
				}
			case watch.Deleted:
				return false, fmt.Errorf("pod %s was deleted before completion", pod.Name)
			case watch.Error:
				if status, ok := event.Object.(*metav1.Status); ok {
					return false, fmt.Errorf("error watching pod for job %s: %s", jobName, status.Message)
				}
				return false, fmt.Errorf("error watching pod for job %s", jobName)
			default:
				return false, nil
			}
		},
	)

	return err
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
