package cluster

import (
	"encoding/json"
	"fmt"
	"strconv"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	batchv1 "k8s.io/api/batch/v1"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type CBBackupmgrAction string

const (
	Incremental CBBackupmgrAction = "incremental"
	Full        CBBackupmgrAction = "full"
)

// generateCronJobs generates the appropriate backup Cronjobs for a CouchbaseBackup.
func (c *Cluster) generateCronJobs(backups []couchbasev2.CouchbaseBackup) ([]*batchv1beta1.CronJob, error) {
	var cronjobs []*batchv1beta1.CronJob

	for i := range backups {
		backup := backups[i]

		if backup.Spec.Strategy == couchbasev2.FullIncremental {
			cronjobs = append(cronjobs, c.generateBackupCronjob(&backup, Incremental, couchbasev2.FullIncremental))
		}

		cronjobs = append(cronjobs, c.generateBackupCronjob(&backup, Full, backup.Spec.Strategy))
	}

	// apply annotations
	for _, cronjob := range cronjobs {
		k8sutil.ApplyBaseAnnotations(cronjob)

		specJSON, err := json.Marshal(cronjob.Spec)
		if err != nil {
			return nil, err
		}

		cronjob.Annotations[constants.CronjobSpecAnnotation] = string(specJSON)
	}

	// if TLS enabled apply TLS config to cronjobs
	if c.cluster.Spec.Networking.TLS != nil {
		for _, cronjob := range cronjobs {
			if err := applyTLSConfiguration(c.cluster.Spec, &cronjob.Spec.JobTemplate.Spec); err != nil {
				return nil, err
			}
		}
	}

	return cronjobs, nil
}

// Given a podSpec, return a pointer to the backup container.
func getBackupContainerForTLS(podSpec corev1.PodSpec, containerName string) (*corev1.Container, error) {
	for index := range podSpec.Containers {
		if podSpec.Containers[index].Name == containerName {
			return &podSpec.Containers[index], nil
		}
	}

	return nil, fmt.Errorf("unable to locate backup container: %w", errors.NewStackTracedError(errors.ErrResourceRequired))
}

// Adds any necessary pod prerequisites before enabling TLS.
func applyTLSConfiguration(cs couchbasev2.ClusterSpec, job *batchv1.JobSpec) error {
	if cs.Networking.TLS != nil {
		// Static configuration:
		// * Defines a volume which contains the secrets necessary
		//   to explicitly define TLS certificates and keys
		if cs.Networking.TLS.Static != nil {
			// Ensure the schema is correct
			// TODO: does this make sense not to be a pointer?
			if len(cs.Networking.TLS.Static.OperatorSecret) == 0 {
				return fmt.Errorf("static tls operator secret required: %w", errors.NewStackTracedError(errors.ErrResourceRequired))
			}

			//Add the TLS secret volume to the podSpec
			volume := corev1.Volume{
				Name: "couchbase-operator-tls",
			}
			volume.VolumeSource.Secret = &corev1.SecretVolumeSource{
				SecretName: cs.Networking.TLS.Static.OperatorSecret,
			}
			job.Template.Spec.Volumes = append(job.Template.Spec.Volumes, volume)

			// add --cacert argument to backup_script
			containerArgs := job.Template.Spec.Containers[0].Args
			containerArgs = append(containerArgs, "--cacert")
			containerArgs = append(containerArgs, "/var/run/secrets/couchbase.com/tls-mount/ca.crt")
			job.Template.Spec.Containers[0].Args = containerArgs

			// Mount the secret volume in Couchbase's inbox
			volumeMount := corev1.VolumeMount{
				Name:      "couchbase-operator-tls",
				ReadOnly:  true,
				MountPath: "/var/run/secrets/couchbase.com/tls-mount",
			}

			containerName := job.Template.Spec.Containers[0].Name

			container, err := getBackupContainerForTLS(job.Template.Spec, containerName)
			if err != nil {
				return err
			}

			container.VolumeMounts = append(container.VolumeMounts, volumeMount)

			// Annotate the podSpec as having TLS enabled
			if job.Template.Annotations == nil {
				job.Template.Annotations = map[string]string{}
			}

			job.Template.Annotations[constants.PodTLSAnnotation] = "enabled"
		}
	}

	return nil
}

// generateBackupCronjob generates a backup cronjob taking into account the backup strategy and the cbbackupmgr action.
func (c *Cluster) generateBackupCronjob(backup *couchbasev2.CouchbaseBackup, action CBBackupmgrAction, strategy couchbasev2.Strategy) *batchv1beta1.CronJob {
	var schedule string

	var container corev1.Container

	var affinity *corev1.Affinity

	switch action {
	case Incremental:
		schedule = backup.Spec.Incremental.Schedule
		container = c.generateBackupContainer("cbbackupmgr-incremental", strategy, false,
			*backup.Spec.LogRetention, *backup.Spec.BackupRetention)
	case Full:
		schedule = backup.Spec.Full.Schedule
		container = c.generateBackupContainer("cbbackupmgr-full", strategy, true,
			*backup.Spec.LogRetention, *backup.Spec.BackupRetention)
	}

	if c.cluster.Spec.AntiAffinity {
		affinity = k8sutil.AntiAffinityForCluster(c.cluster.Name)
	}

	cronjob := &batchv1beta1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name: backup.Name + "-" + string(action),
			Labels: map[string]string{
				constants.LabelApp:     constants.App,
				constants.LabelCluster: c.cluster.Name,
				constants.LabelBackup:  backup.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				c.cluster.AsOwner(),
			},
		},
		Spec: batchv1beta1.CronJobSpec{
			Schedule:                   schedule,
			SuccessfulJobsHistoryLimit: &backup.Spec.SuccessfulJobsHistoryLimit,
			FailedJobsHistoryLimit:     &backup.Spec.FailedJobsHistoryLimit,
			ConcurrencyPolicy:          batchv1beta1.ForbidConcurrent,
			JobTemplate: batchv1beta1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					BackoffLimit: &backup.Spec.BackoffLimit,
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								constants.LabelBackup: backup.Name,
							},
						},
						Spec: corev1.PodSpec{
							Affinity: affinity,
							Containers: []corev1.Container{
								container,
							},
							NodeSelector:  c.cluster.Spec.Backup.NodeSelector,
							RestartPolicy: corev1.RestartPolicyNever,
							SecurityContext: &corev1.PodSecurityContext{
								FSGroup: c.cluster.Spec.SecurityContext.FSGroup,
							},
							ServiceAccountName: c.cluster.Spec.Backup.ServiceAccount,
							Tolerations:        c.cluster.Spec.Backup.Tolerations,
							Volumes: []corev1.Volume{
								{
									Name: "couchbase-cluster-backup-volume",
									VolumeSource: corev1.VolumeSource{
										PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
											ClaimName: backup.Name,
											ReadOnly:  false,
										},
									},
								},
								{
									Name: "couchbase-admin",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName: c.cluster.Spec.Security.AdminSecret,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	return cronjob
}

// generateBackupContainer returns the actual backup container
// with the correct image and executable command and arguments
// config is a boolean that determines whether we take want to config a new repo
// and then take a Full backup (true) or just an incremental backup (false).
func (c *Cluster) generateBackupContainer(containerName string, strategy couchbasev2.Strategy, config bool, logRetention, backupRetention metav1.Duration) corev1.Container {
	var resources corev1.ResourceRequirements

	if c.cluster.Spec.Backup.Resources != nil {
		resources = *c.cluster.Spec.Backup.Resources
	}

	return corev1.Container{
		Name:    containerName,
		Image:   c.cluster.Spec.BackupImage(),
		Command: []string{"backup_script"},
		Args: []string{c.cluster.Name, "--strategy", string(strategy), "--config", strconv.FormatBool(config),
			"--mode", "backup",
			"--backup-ret", fmt.Sprintf("%.2f", backupRetention.Hours()),
			"--log-ret", fmt.Sprintf("%.2f", logRetention.Hours()),
			"-v", "INFO"},
		WorkingDir: "/",
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "couchbase-cluster-backup-volume",
				ReadOnly:  false,
				MountPath: "/data",
			},
			{
				Name:      "couchbase-admin",
				ReadOnly:  true,
				MountPath: "/var/run/secrets/couchbase",
			},
		},
		Resources: resources,
	}
}

// generateRestoreJob returns a job that performs a cbbackupmgr restore command.
func (c *Cluster) generateRestoreJob(restore couchbasev2.CouchbaseBackupRestore) (*batchv1.Job, error) {
	var start string

	var end string

	if restore.Spec.Start.Int != nil {
		start = strconv.Itoa(*restore.Spec.Start.Int)
	} else {
		start = *restore.Spec.Start.Str
	}

	if restore.Spec.End.Int != nil {
		end = strconv.Itoa(*restore.Spec.End.Int)
	} else {
		end = *restore.Spec.End.Str
	}

	restorejob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: restore.Name,
			Labels: map[string]string{
				constants.LabelApp:     constants.App,
				constants.LabelCluster: c.cluster.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: couchbasev2.Group,
					Kind:       couchbasev2.BackupRestoreCRDResourceKind,
					Name:       restore.Name,
					UID:        restore.UID,
				},
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &restore.Spec.BackoffLimit,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: c.cluster.Spec.Backup.ServiceAccount,
					InitContainers:     nil,
					Containers: []corev1.Container{
						c.generateRestoreContainer(restore.Spec, start, end),
					},
					RestartPolicy: corev1.RestartPolicyNever,
					Volumes: []corev1.Volume{
						{
							Name: "couchbase-cluster-backup-volume",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: restore.Spec.Backup,
									ReadOnly:  false,
								},
							},
						},
						{
							Name: "couchbase-admin",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: c.cluster.Spec.Security.AdminSecret,
								},
							},
						},
					},
				},
			},
		},
	}

	if err := applyTLSConfiguration(c.cluster.Spec, &restorejob.Spec); err != nil {
		return nil, err
	}

	return restorejob, nil
}

// generateRestoreContainer returns a container that uses the operator-backup image
// but specifies the restore mode to the backup_script instead of the backup mode.
func (c *Cluster) generateRestoreContainer(spec couchbasev2.CouchbaseBackupRestoreSpec, start, end string) corev1.Container {
	var resources corev1.ResourceRequirements

	if c.cluster.Spec.Backup.Resources != nil {
		resources = *c.cluster.Spec.Backup.Resources
	}

	return corev1.Container{
		Name:    "cbbackupmgr-restore",
		Image:   c.cluster.Spec.BackupImage(),
		Command: []string{"backup_script"},
		Args: []string{c.cluster.Name, "--mode", "restore",
			"--repo", spec.Repo, "--start", start, "--end", end,
			"--log-ret", fmt.Sprintf("%.2f", spec.LogRetention.Hours())},
		WorkingDir: "/",
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "couchbase-cluster-backup-volume",
				ReadOnly:  false,
				MountPath: "/data",
			},
			{
				Name:      "couchbase-admin",
				ReadOnly:  true,
				MountPath: "/var/run/secrets/couchbase",
			},
		},
		Resources: resources,
	}
}

// get the Repo value from a Backup Object to use in a Restore object.
func (c *Cluster) getBackupRepo(restore *couchbasev2.CouchbaseBackupRestore) error {
	var backupFound bool

	backupName := restore.Spec.Backup
	backups := c.k8s.CouchbaseBackups.List()

	if len(backups) == 0 {
		return fmt.Errorf("no CouchbaseBackups exist currently: %w", errors.NewStackTracedError(errors.ErrResourceRequired))
	}

	for _, backup := range backups {
		if backup.Name == backupName {
			if len(backup.Status.Repo) != 0 {
				restore.Spec.Repo = backup.Status.Repo
				backupFound = true

				break
			}
		}
	}

	if !backupFound {
		return fmt.Errorf("no corresponding CouchbaseBackup Repo found: %w", errors.NewStackTracedError(errors.ErrResourceRequired))
	}

	return nil
}

func generateBackupPVCs(backups []couchbasev2.CouchbaseBackup, cluster *couchbasev2.CouchbaseCluster) []*corev1.PersistentVolumeClaim {
	var pvcs []*corev1.PersistentVolumeClaim

	for _, backup := range backups {
		pvcs = append(pvcs, generateBackupPVC(backup.Name, cluster, backup.Spec.Size))
	}

	return pvcs
}

// generateBackupPVC returns the PVC that backups will be stored on.
func generateBackupPVC(pvcName string, cluster *couchbasev2.CouchbaseCluster, storage *resource.Quantity) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:   pvcName,
			Labels: k8sutil.LabelsForCluster(cluster),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: *storage,
				},
			},
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
		},
	}
}

func (c *Cluster) deleteBackups() error {
	deletedBackups := make(map[string]bool)

	// loop over the current existing actualCronjobs
	actualCronjobs := c.k8s.CronJobs.List()
	for _, cronjob := range actualCronjobs {
		// check if the job has an "owner" backup
		backupToDelete := cronjob.Labels[constants.LabelBackup]

		// no "owner" backup exists, must have been deleted. cleanup cronjobs which in turn deletes jobs + pods
		if _, ok := c.k8s.CouchbaseBackups.Get(backupToDelete); !ok {
			if err := c.k8s.KubeClient.BatchV1beta1().CronJobs(c.cluster.Namespace).Delete(cronjob.Name, &metav1.DeleteOptions{}); err != nil {
				return err
			}

			log.Info("Backup Cronjob deleted", "cbbackup", backupToDelete, "cronjob", cronjob.Name)
			// add and raise events later
			deletedBackups[backupToDelete] = true
		}
	}

	for backup := range deletedBackups {
		log.Info("Backup deleted", "cbbackup", backup)
		c.raiseEvent(k8sutil.BackupDeleteEvent(backup, c.cluster))
	}

	return nil
}
