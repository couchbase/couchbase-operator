package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

type CBBackupmgrAction string

const (
	Incremental       CBBackupmgrAction = "incremental"
	Full              CBBackupmgrAction = "full"
	ObjEndpointCrtDir string            = "/var/run/secrets/objectendpoint"
	backupTLSMountDir string            = "/var/run/secrets/couchbase.com/tls-mount"
)

// backupResources contains all the resources required to create and manage a backup.
type backupResources struct {
	// name is the backup this set of resources relates to.
	name string

	// backup is the backup resource this relates to.
	// If this is nil, then it needs deleting.
	backup *couchbasev2.CouchbaseBackup

	// fullCronJob deals with running and scheduling a full backup.
	fullCronJob *batchv1.CronJob

	// incrementalCronJob (optional) deals with running and schedling an incremental backup.
	incrementalCronJob *batchv1.CronJob

	// pvc deals with persisting the backup data.
	pvc *corev1.PersistentVolumeClaim
}

type backupResourcesList []backupResources

func (l backupResourcesList) find(name string) *backupResources {
	for i, r := range l {
		if r.name == name {
			return &l[i]
		}
	}

	return nil
}

func (l backupResourcesList) contains(resource backupResources) bool {
	return l.find(resource.name) != nil
}

// generateBackupResources evaluates the specification and determines the resources
// required to implement the intended function.
func (c *Cluster) generateBackupResources() (backupResourcesList, error) {
	var resources backupResourcesList

	backups, err := c.gatherBackups()
	if err != nil {
		return nil, err
	}

	for i := range backups {
		backup := &backups[i]

		fullCronJob, err := c.generateBackupCronjob(backup, Full)
		if err != nil {
			return nil, err
		}

		resource := backupResources{
			name:        backup.Name,
			backup:      backup,
			fullCronJob: fullCronJob,
			pvc:         c.generateBackupPVC(backup),
		}

		if backup.Spec.Strategy == couchbasev2.FullIncremental {
			incrementalCronJob, err := c.generateBackupCronjob(backup, Incremental)
			if err != nil {
				return nil, err
			}

			resource.incrementalCronJob = incrementalCronJob
		}

		resources = append(resources, resource)
	}

	return resources, nil
}

// listBackupResources searches Kubernetes for any backup resources and returns them.
func (c Cluster) listBackupResources() (backupResourcesList, error) {
	var resources backupResourcesList

	for _, cronjob := range c.k8s.CronJobs.List() {
		// Extract the backup name, which is defined as a label.
		// If these fire then either the job hasn't been labelled correctly or,
		// even more sinful, we are caching things that we shouldn't.
		if cronjob.Labels == nil {
			log.Info("cronjob missing labels", "cluster", c.namespacedName(), "cronjob", cronjob.Name)
			continue
		}

		name, ok := cronjob.Labels[constants.LabelBackup]
		if !ok {
			log.Info("cronjob missing backup label", "cluster", c.namespacedName(), "cronjob", cronjob.Name)
			continue
		}

		// As we may have multiple cronjobs per backup we're either going to
		// modify an existing set of resources, or create a new one.  Look for
		// an existing one...
		var resource *backupResources

		for i := range resources {
			if resources[i].name == name {
				resource = &resources[i]
				break
			}
		}

		// .. if it doesn't exist, create temporary storage and record that
		// it needs appending.
		var create bool

		if resource == nil {
			resource = &backupResources{
				name: name,
			}
			create = true
		}

		backup, ok := c.k8s.CouchbaseBackups.Get(name)
		if ok {
			resource.backup = backup
		}

		// There is nothing to discriminate between usage other than the name
		// so go off this.
		switch {
		case strings.HasSuffix(cronjob.Name, string(Full)):
			resource.fullCronJob = cronjob
		case strings.HasSuffix(cronjob.Name, string(Incremental)):
			resource.incrementalCronJob = cronjob
		default:
			return nil, fmt.Errorf("unable to determine cronjob type: %w", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
		}

		// There is nothing to tell us that a PVC belongs to a backup, so this is a little
		// dodgy, well a lot dodgy, in that we only get an entry in the result if a cronjob
		// is defeind.  The knock on effect is that we may end up "recreating" a backup
		// job, not rectifying it, and during that creation we need to be on the lookout for
		// the PVC already existing.
		pvc, ok := c.k8s.PersistentVolumeClaims.Get(name)
		if ok {
			resource.pvc = pvc
		}

		if create {
			resources = append(resources, *resource)
		}
	}

	return resources, nil
}

// createBackupResource creates all Kubernetes resources associated with a backup.
func (c *Cluster) createBackupResource(resource backupResources) error {
	// There is nothing to flag a PVC as belonging to a backup (bug!)
	// so we won't create a current backup resource for it, and thus
	// it's possible to delete all cronjobs and end up here, so ensure
	// the PVC doesn't already exist first.
	if _, ok := c.k8s.PersistentVolumeClaims.Get(resource.pvc.Name); !ok {
		if _, err := c.k8s.KubeClient.CoreV1().PersistentVolumeClaims(c.cluster.Namespace).Create(context.Background(), resource.pvc, metav1.CreateOptions{}); err != nil {
			return err
		}
	}

	if _, err := c.k8s.KubeClient.BatchV1().CronJobs(c.cluster.Namespace).Create(context.Background(), resource.fullCronJob, metav1.CreateOptions{}); err != nil {
		return err
	}

	if resource.incrementalCronJob != nil {
		if _, err := c.k8s.KubeClient.BatchV1().CronJobs(c.cluster.Namespace).Create(context.Background(), resource.incrementalCronJob, metav1.CreateOptions{}); err != nil {
			return err
		}
	}

	log.Info("Backup created", "cbbackup", resource.name)

	c.raiseEvent(k8sutil.BackupCreateEvent(resource.name, c.cluster))

	return nil
}

// backupUpdateNotifier acts like a singleton pattern, raising the even only once.
type backupUpdateNotifier struct {
	// c is is the cluster this relates to.
	c *Cluster

	// name is the backup name.
	name string

	// raised is whether the even has been raised.
	raised bool
}

func (n *backupUpdateNotifier) notify() {
	if n.raised {
		return
	}

	log.Info("Backup updated", "cbbackup", n.name)

	n.c.raiseEvent(k8sutil.BackupUpdateEvent(n.name, n.c.cluster))

	n.raised = true
}

// updateBackupResource conditionally updates Kubernetes resources associated with a backup.
func (c *Cluster) updateBackupResource(requested backupResources, current *backupResources) error {
	notifier := &backupUpdateNotifier{
		c:    c,
		name: requested.name,
	}

	if err := c.updateBackupCronJob(notifier, requested.fullCronJob, current.fullCronJob); err != nil {
		return err
	}

	if err := c.updateBackupCronJob(notifier, requested.incrementalCronJob, current.incrementalCronJob); err != nil {
		return err
	}

	if err := c.updateBackupPVC(notifier, requested.backup, requested.pvc, current.pvc); err != nil {
		return err
	}

	return nil
}

// updateBackupCronJob recreates jobs if they have been deleted, deletes them if they need
// to be e.g. switching from incremental to full, or modifies the configuration.
func (c *Cluster) updateBackupCronJob(notifier *backupUpdateNotifier, requested, current *batchv1.CronJob) error {
	if requested == nil && current == nil {
		return nil
	}

	if requested != nil && current == nil {
		if _, err := c.k8s.KubeClient.BatchV1().CronJobs(c.cluster.Namespace).Create(context.Background(), requested, metav1.CreateOptions{}); err != nil {
			return err
		}

		notifier.notify()

		return nil
	}

	if requested == nil && current != nil {
		if err := c.k8s.KubeClient.BatchV1().CronJobs(c.cluster.Namespace).Delete(context.Background(), current.Name, metav1.DeleteOptions{}); err != nil {
			return err
		}

		notifier.notify()

		return nil
	}

	requestedSpec := &batchv1.CronJobSpec{}

	if err := json.Unmarshal([]byte(requested.Annotations[constants.CronjobSpecAnnotation]), requestedSpec); err != nil {
		return err
	}

	currentSpec := &batchv1.CronJobSpec{}

	if err := json.Unmarshal([]byte(current.Annotations[constants.CronjobSpecAnnotation]), currentSpec); err != nil {
		return err
	}

	if reflect.DeepEqual(requestedSpec, currentSpec) {
		return nil
	}

	resource := current.DeepCopy()
	resource.Annotations = requested.Annotations
	resource.Spec = requested.Spec

	if _, err := c.k8s.KubeClient.BatchV1().CronJobs(c.cluster.Namespace).Update(context.Background(), resource, metav1.UpdateOptions{}); err != nil {
		return err
	}

	notifier.notify()

	return nil
}

// updateBackupPVC recreates the PVC if it has been deleted or does dyanmic expansion if
// the backup is reporting that space is running low.
func (c *Cluster) updateBackupPVC(notifier *backupUpdateNotifier, backup *couchbasev2.CouchbaseBackup, requested, current *corev1.PersistentVolumeClaim) error {
	if current == nil {
		if _, err := c.k8s.KubeClient.CoreV1().PersistentVolumeClaims(c.cluster.Namespace).Create(context.Background(), requested, metav1.CreateOptions{}); err != nil {
			return err
		}

		notifier.notify()

		return nil
	}

	currentRequestedSize, ok := current.Spec.Resources.Requests[corev1.ResourceStorage]
	if !ok {
		log.V(1).Info("Skipping backup volume reconcile, no storage requested", "cluster", c.namespacedName(), "backup", requested.Name)
		return nil
	}

	currentActualSize, ok := current.Status.Capacity[corev1.ResourceStorage]
	if !ok {
		log.V(1).Info("Skipping backup volume reconcile, no capacity defined", "cluster", c.namespacedName(), "backup", requested.Name)
		return nil
	}

	if !currentRequestedSize.Equal(currentActualSize) {
		log.V(1).Info("Skipping backup volume reconcile, resize pending", "cluster", c.namespacedName(), "backup", requested.Name)
		return nil
	}

	// By this point we know that the volume is not resizing.  We will determine the size
	// the volume should be.  This starts as either the requested volume size, or the actual
	// volume size if it's larger (volume contraction is not supported).
	size := backup.Spec.Size

	if currentRequestedSize.Cmp(*size) > 0 {
		size = &currentRequestedSize
	}

	// Next we need to dynamically expand the volume size if requested.
	if backup.Spec.AutoScaling != nil {
		// If the free capacity is less than the threshold, we need to alter the
		// size by the configured amount.  We also need to cap this at the
		// limit if one is provided.
		if backup.Status.CapacityUsed != nil {
			free := size.DeepCopy()
			free.Sub(*backup.Status.CapacityUsed)

			threshold := resource.NewQuantity((size.Value()*int64(backup.Spec.AutoScaling.ThresholdPercent))/100, resource.BinarySI)

			log.V(1).Info("Backup autoscaler status", "cluster", c.namespacedName(), "backup", backup.Name, "size", size, "free", free, "used", backup.Status.CapacityUsed, "threshold", threshold)

			if free.Cmp(*threshold) < 0 {
				increment := resource.NewQuantity((size.Value()*(100+int64(backup.Spec.AutoScaling.IncrementPercent)))/100, resource.BinarySI)
				size.Add(*increment)

				if backup.Spec.AutoScaling.Limit != nil && size.Cmp(*backup.Spec.AutoScaling.Limit) > 0 {
					size = backup.Spec.AutoScaling.Limit
				}

				log.Info("Backup autoscaler scaling", "cluster", c.namespacedName(), "backup", backup.Name, "size", size)
			}
		}
	}

	if currentRequestedSize.Equal(*size) {
		return nil
	}

	pvc := current.DeepCopy()
	pvc.Spec.Resources.Requests[corev1.ResourceStorage] = *size

	if _, err := c.k8s.KubeClient.CoreV1().PersistentVolumeClaims(c.cluster.Namespace).Update(context.Background(), pvc, metav1.UpdateOptions{}); err != nil {
		return err
	}

	log.Info("Backup PVC resize pending", "cbbackup", pvc.Name, "new size", *size)

	notifier.notify()

	return nil
}

// deleteBackupResource deletes Kubernetes resources associated with a backup.
// The exception being the PVC, because it's not a very good backup if it gets deleted
// when you accidentally delete the backup :D
// So it strikes me that if the cronjobs were owned by the backup, then Kubernetes GC
// would do this for us, we'd lose the ability to generate an event... but then we could
// use the shared informer to raise it for us (if we are online at the time).
func (c *Cluster) deleteBackupResource(resource backupResources) error {
	if resource.fullCronJob != nil {
		if err := c.k8s.KubeClient.BatchV1().CronJobs(c.cluster.Namespace).Delete(context.Background(), resource.fullCronJob.Name, metav1.DeleteOptions{}); err != nil {
			return err
		}
	}

	if resource.incrementalCronJob != nil {
		if err := c.k8s.KubeClient.BatchV1().CronJobs(c.cluster.Namespace).Delete(context.Background(), resource.incrementalCronJob.Name, metav1.DeleteOptions{}); err != nil {
			return err
		}
	}

	log.Info("Backup deleted", "cbbackup", resource.name)

	c.raiseEvent(k8sutil.BackupDeleteEvent(resource.name, c.cluster))

	return nil
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
// BUG: this completely ignores mTLS...
func applyTLSConfiguration(cluster *couchbasev2.CouchbaseCluster, job *batchv1.JobSpec) error {
	if !cluster.IsTLSEnabled() {
		return nil
	}

	// cbbackupmgr doesn't actually support mTLS, so when client certs are mandatory
	// for security you cannot do backups!  In this case we, by definition need to
	// do it over plaintext!
	if cluster.IsMandatoryMutualTLSEnabled() {
		return nil
	}

	secret := k8sutil.ClientTLSSecretName(cluster)
	volumeName := constants.CouchbaseTLSVolumeName + "-backup"

	// Add the TLS secret volume to the podSpec
	volume := corev1.Volume{
		Name: volumeName,
	}
	volume.VolumeSource.Secret = &corev1.SecretVolumeSource{
		SecretName: secret,
	}
	job.Template.Spec.Volumes = append(job.Template.Spec.Volumes, volume)

	// add --cacert argument to backup_script
	containerArgs := job.Template.Spec.Containers[0].Args
	containerArgs = append(containerArgs, "--cacert")
	containerArgs = append(containerArgs, backupTLSMountDir+"/ca.crt")
	job.Template.Spec.Containers[0].Args = containerArgs

	// Mount the secret volume in Couchbase's inbox
	volumeMount := corev1.VolumeMount{
		Name:      volumeName,
		ReadOnly:  true,
		MountPath: backupTLSMountDir,
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

	job.Template.Annotations[constants.PodTLSAnnotation] = constants.EnabledValue

	return nil
}

// generateBackupCronjob generates a backup cronjob taking into account the backup strategy and the cbbackupmgr action.
func (c *Cluster) generateBackupCronjob(backup *couchbasev2.CouchbaseBackup, action CBBackupmgrAction) (*batchv1.CronJob, error) {
	var schedule string

	var container corev1.Container

	var affinity *corev1.Affinity

	switch action {
	case Incremental:
		schedule = backup.Spec.Incremental.Schedule
		container = c.generateBackupContainer("cbbackupmgr-incremental", backup, false)
	case Full:
		schedule = backup.Spec.Full.Schedule
		container = c.generateBackupContainer("cbbackupmgr-full", backup, true)
	}

	if c.cluster.Spec.AntiAffinity {
		affinity = k8sutil.AntiAffinityForCluster(c.cluster.Name)
	}

	labels := k8sutil.LabelsForCluster(c.cluster)
	labels[constants.LabelBackup] = backup.Name

	cronjob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:   backup.Name + "-" + string(action),
			Labels: labels,
			OwnerReferences: []metav1.OwnerReference{
				c.cluster.AsOwner(),
			},
		},
		Spec: batchv1.CronJobSpec{
			Schedule:                   schedule,
			SuccessfulJobsHistoryLimit: &backup.Spec.SuccessfulJobsHistoryLimit,
			FailedJobsHistoryLimit:     &backup.Spec.FailedJobsHistoryLimit,
			ConcurrencyPolicy:          batchv1.ForbidConcurrent,
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					BackoffLimit: &backup.Spec.BackoffLimit,
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								constants.LabelApp:     constants.App,
								constants.LabelCluster: c.cluster.Name,
								constants.LabelBackup:  backup.Name,
							},
						},
						Spec: corev1.PodSpec{
							Affinity: affinity,
							Containers: []corev1.Container{
								container,
							},
							NodeSelector:       c.cluster.Spec.Backup.NodeSelector,
							RestartPolicy:      corev1.RestartPolicyNever,
							SecurityContext:    c.cluster.Spec.SecurityContext,
							ServiceAccountName: c.cluster.Spec.Backup.ServiceAccount,
							ImagePullSecrets:   c.cluster.Spec.Backup.ImagePullSecrets,
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

	// Mount objectendpoint cert if set.
	c.applyObjEndpointCertToCronJob(cronjob)

	k8sutil.ApplyBaseAnnotations(cronjob)

	// if TLS enabled apply TLS config to cronjobs
	if err := applyTLSConfiguration(c.cluster, &cronjob.Spec.JobTemplate.Spec); err != nil {
		return nil, err
	}

	specJSON, err := json.Marshal(cronjob.Spec)
	if err != nil {
		return nil, errors.NewStackTracedError(err)
	}

	cronjob.Annotations[constants.CronjobSpecAnnotation] = string(specJSON)

	return cronjob, nil
}

// generateBackupContainer returns the actual backup container
// with the correct image and executable command and arguments
// config is a boolean that determines whether we take want to config a new repo
// and then take a Full backup (true) or just an incremental backup (false).
func (c *Cluster) generateBackupContainer(containerName string, backup *couchbasev2.CouchbaseBackup, full bool) corev1.Container {
	var resources corev1.ResourceRequirements

	if c.cluster.Spec.Backup.Resources != nil {
		resources = *c.cluster.Spec.Backup.Resources
	}

	args := []string{
		"--mode", "backup",
		"--backup-ret", fmt.Sprintf("%.2f", backup.Spec.BackupRetention.Hours()),
		"--log-ret", fmt.Sprintf("%.2f", backup.Spec.LogRetention.Hours()),
		"-v", "INFO",
		c.cluster.Name,
	}

	if full {
		args = append(args, "--full")
	} else {
		args = append(args, "--incremental")
	}

	// Old resources won't have this set until written.
	if backup.Spec.Threads != 0 {
		args = append(args, "--threads", strconv.Itoa(backup.Spec.Threads))
	}

	// These will all be set, to something due to defaulting, so the dereference is safe.
	flags := map[string]*bool{
		"--disable-bucket-config":     backup.Spec.Services.BucketConfig,
		"--disable-views":             backup.Spec.Services.Views,
		"--disable-gsi-indexes":       backup.Spec.Services.GSIndexes,
		"--disable-ft-indexes":        backup.Spec.Services.FTSIndexes,
		"--disable-ft-alias":          backup.Spec.Services.FTSAliases,
		"--disable-data":              backup.Spec.Services.Data,
		"--disable-analytics":         backup.Spec.Services.Analytics,
		"--disable-eventing":          backup.Spec.Services.Eventing,
		"--disable-cluster-analytics": backup.Spec.Services.ClusterAnalytics,
		"--disable-bucket-query":      backup.Spec.Services.BucketQuery,
		"--disable-cluster-query":     backup.Spec.Services.ClusterQuery,
	}

	for flag, value := range flags {
		if !*value {
			args = append(args, flag)
		}
	}

	if backup.Spec.Data != nil {
		if len(backup.Spec.Data.Include) > 0 {
			args = append(args, "--include-data", strings.Join(couchbasev2.BucketScopeOrCollectionNameWithDefaultsList(backup.Spec.Data.Include).StringSlice(), ","))
		}

		if len(backup.Spec.Data.Exclude) > 0 {
			args = append(args, "--exclude-data", strings.Join(couchbasev2.BucketScopeOrCollectionNameWithDefaultsList(backup.Spec.Data.Exclude).StringSlice(), ","))
		}
	}

	container := corev1.Container{
		Name:       containerName,
		Image:      c.cluster.Spec.BackupImage(),
		Args:       args,
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

	if len(backup.Spec.S3Bucket) != 0 {
		c.applyS3Configuration(&container, backup.Spec.S3Bucket)
	}

	// Set container volume mount and update args if custom endpoint is set.
	c.applyObjEndpointToContainer(&container)

	return container
}

// generateRestoreJob returns a job that performs a cbbackupmgr restore command.
func (c *Cluster) generateRestoreJob(restore *couchbasev2.CouchbaseBackupRestore) (*batchv1.Job, error) {
	var start string

	if restore.Spec.Start != nil {
		if restore.Spec.Start.Int != nil {
			start = strconv.Itoa(*restore.Spec.Start.Int)
		} else {
			start = *restore.Spec.Start.Str
		}
	}

	var end string

	if restore.Spec.End != nil {
		if restore.Spec.End.Int != nil {
			end = strconv.Itoa(*restore.Spec.End.Int)
		} else {
			end = *restore.Spec.End.Str
		}
	}

	labels := k8sutil.LabelsForCluster(c.cluster)
	labels[constants.LabelBackupRestore] = restore.Name

	restorejob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:   restore.Name,
			Labels: labels,
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
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						constants.LabelApp:           constants.App,
						constants.LabelCluster:       c.cluster.Name,
						constants.LabelBackupRestore: restore.Name,
					},
				},
				Spec: corev1.PodSpec{
					NodeSelector:       c.cluster.Spec.Backup.NodeSelector,
					ServiceAccountName: c.cluster.Spec.Backup.ServiceAccount,
					ImagePullSecrets:   c.cluster.Spec.Backup.ImagePullSecrets,
					InitContainers:     nil,
					Containers: []corev1.Container{
						c.generateRestoreContainer(restore, start, end),
					},
					RestartPolicy:   corev1.RestartPolicyNever,
					SecurityContext: c.cluster.Spec.SecurityContext,
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

	if err := applyTLSConfiguration(c.cluster, &restorejob.Spec); err != nil {
		return nil, err
	}
	// Mount objectendpoint cert if set
	c.applyObjEndpointCertToJob(restorejob)

	return restorejob, nil
}

// generateRestoreContainer returns a container that uses the operator-backup image
// but specifies the restore mode to the backup_script instead of the backup mode.
func (c *Cluster) generateRestoreContainer(restore *couchbasev2.CouchbaseBackupRestore, start, end string) corev1.Container {
	var resources corev1.ResourceRequirements

	if c.cluster.Spec.Backup.Resources != nil {
		resources = *c.cluster.Spec.Backup.Resources
	}

	spec := restore.Spec

	args := []string{
		"--mode", "restore",
		"--log-ret", fmt.Sprintf("%.2f", spec.LogRetention.Hours()),
		c.cluster.Name,
	}

	if spec.Repo != "" {
		args = append(args, "--repo", spec.Repo)
	}

	if start != "" {
		args = append(args, "--start", start)
	}

	if end != "" {
		args = append(args, "--end", end)
	}

	if spec.ForceUpdates {
		args = append(args, "--force-updates")
	}

	// Old resources won't have this set until written.
	if spec.Threads != 0 {
		args = append(args, "--threads", strconv.Itoa(spec.Threads))
	}

	// check if any bucket config has been defined
	if spec.Data != nil {
		if len(spec.Data.Include) != 0 {
			args = append(args, "--include-data", strings.Join(couchbasev2.BucketScopeOrCollectionNameWithDefaultsList(spec.Data.Include).StringSlice(), ","))
		}

		if len(spec.Data.Exclude) != 0 {
			args = append(args, "--exclude-data", strings.Join(couchbasev2.BucketScopeOrCollectionNameWithDefaultsList(spec.Data.Exclude).StringSlice(), ","))
		}

		if len(spec.Data.Map) != 0 {
			var mappings []string

			for _, m := range spec.Data.Map {
				mappings = append(mappings, string(m.Source)+"="+string(m.Target))
			}

			args = append(args, "--map-data", strings.Join(mappings, ","))
		}

		if spec.Data.FilterKeys != "" {
			args = append(args, "--filter-keys", spec.Data.FilterKeys)
		}

		if spec.Data.FilterValues != "" {
			args = append(args, "--filter-values", spec.Data.FilterValues)
		}
	}

	if spec.Services.BucketConfig {
		args = append(args, "--enable-bucket-config")
	}

	// These will all be set, to something due to defaulting, so the dereference is safe.
	flags := map[string]*bool{
		"--disable-views":             spec.Services.Views,
		"--disable-gsi-indexes":       spec.Services.GSIIndex,
		"--disable-ft-indexes":        spec.Services.FTIndex,
		"--disable-ft-alias":          spec.Services.FTAlias,
		"--disable-data":              spec.Services.Data,
		"--disable-analytics":         spec.Services.Analytics,
		"--disable-eventing":          spec.Services.Eventing,
		"--disable-cluster-analytics": spec.Services.ClusterAnalytics,
		"--disable-bucket-query":      spec.Services.BucketQuery,
		"--disable-cluster-query":     spec.Services.ClusterQuery,
	}

	for flag, value := range flags {
		if !*value {
			args = append(args, flag)
		}
	}

	container := corev1.Container{
		Name:       "cbbackupmgr-restore",
		Image:      c.cluster.Spec.BackupImage(),
		Args:       args,
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

	if len(spec.S3Bucket) != 0 {
		c.applyS3Configuration(&container, spec.S3Bucket)
	}

	// Set container volume mount and update args if custom endpoint is set.
	c.applyObjEndpointToContainer(&container)

	return container
}

func (c *Cluster) applyObjEndpointCertToJob(job *batchv1.Job) {
	if c.cluster.Spec.Backup.ObjectEndpoint == nil {
		return
	}

	if len(c.cluster.Spec.Backup.ObjectEndpoint.CertSecret) != 0 {
		volume := corev1.Volume{
			Name: c.cluster.Spec.Backup.ObjectEndpoint.CertSecret,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: c.cluster.Spec.Backup.ObjectEndpoint.CertSecret,
				},
			},
		}
		job.Spec.Template.Spec.Volumes = append(job.Spec.Template.Spec.Volumes, volume)
	}
}

func (c *Cluster) applyObjEndpointCertToCronJob(cronjob *batchv1.CronJob) {
	if c.cluster.Spec.Backup.ObjectEndpoint == nil {
		return
	}

	if len(c.cluster.Spec.Backup.ObjectEndpoint.CertSecret) != 0 {
		volume := corev1.Volume{
			Name: c.cluster.Spec.Backup.ObjectEndpoint.CertSecret,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: c.cluster.Spec.Backup.ObjectEndpoint.CertSecret,
				},
			},
		}
		cronjob.Spec.JobTemplate.Spec.Template.Spec.Volumes = append(cronjob.Spec.JobTemplate.Spec.Template.Spec.Volumes, volume)
	}
}

func (c *Cluster) applyObjEndpointToContainer(container *corev1.Container) {
	if c.cluster.Spec.Backup.ObjectEndpoint == nil {
		return
	}

	if len(c.cluster.Spec.Backup.ObjectEndpoint.URL) != 0 {
		container.Args = append(container.Args, "--obj-endpoint", c.cluster.Spec.Backup.ObjectEndpoint.URL)

		if len(c.cluster.Spec.Backup.ObjectEndpoint.CertSecret) != 0 {
			volumeMount := corev1.VolumeMount{
				Name:      c.cluster.Spec.Backup.ObjectEndpoint.CertSecret,
				ReadOnly:  true,
				MountPath: ObjEndpointCrtDir,
			}
			container.VolumeMounts = append(container.VolumeMounts, volumeMount)
			container.Args = append(container.Args, "--obj-cacert", fmt.Sprintf("%s/tls.crt", ObjEndpointCrtDir))
		}

		if c.cluster.Spec.Backup.ObjectEndpoint.UseVirtualPath {
			container.Args = append(container.Args, "--s3-force-path-style", fmt.Sprintf("%t", c.cluster.Spec.Backup.ObjectEndpoint.UseVirtualPath))
		}
	}
}

func (c *Cluster) applyS3Configuration(container *corev1.Container, s3BucketName couchbasev2.S3BucketURI) {
	container.Args = append(container.Args, "--s3-bucket", string(s3BucketName))
	container.Env = []corev1.EnvVar{
		{
			Name: "AWS_REGION",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: c.cluster.Spec.Backup.S3Secret,
					},
					Key: "region",
				},
			},
		},
	}

	if c.cluster.Spec.Backup.UseIAMRole {
		container.Env = append(container.Env, corev1.EnvVar{
			Name:  "CB_AWS_ENABLE_EC2_METADATA",
			Value: "true",
		})
	} else {
		container.Env = append(container.Env, []corev1.EnvVar{
			{
				Name: "AWS_ACCESS_KEY_ID",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: c.cluster.Spec.Backup.S3Secret,
						},
						Key: "access-key-id",
					},
				},
			},
			{
				Name: "AWS_SECRET_ACCESS_KEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: c.cluster.Spec.Backup.S3Secret,
						},
						Key: "secret-access-key",
					},
				},
			},
		}...)
	}
}

// generateBackupPVC returns the PVC that backups will be stored on.
func (c *Cluster) generateBackupPVC(backup *couchbasev2.CouchbaseBackup) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:   backup.Name,
			Labels: k8sutil.LabelsForCluster(c.cluster),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: *backup.Spec.Size,
				},
			},
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			StorageClassName: backup.Spec.StorageClassName,
		},
	}
}

// gatherBackups returns CouchbaseBackups based on the cluster Spec selector.
func (c *Cluster) gatherBackups() ([]couchbasev2.CouchbaseBackup, error) {
	selector := labels.Everything()

	if c.cluster.Spec.Backup.Selector != nil {
		var err error
		if selector, err = metav1.LabelSelectorAsSelector(c.cluster.Spec.Backup.Selector); err != nil {
			return nil, err
		}
	}

	couchbaseBackups := c.k8s.CouchbaseBackups.List()

	backups := []couchbasev2.CouchbaseBackup{}

	for _, backup := range couchbaseBackups {
		if !selector.Matches(labels.Set(backup.Labels)) {
			continue
		}

		backups = append(backups, *backup)
	}

	return backups, nil
}

// gatherBackupRestores returns CouchbaseBackupRestores based on the cluster Spec selector.
func (c *Cluster) gatherBackupRestores() ([]couchbasev2.CouchbaseBackupRestore, error) {
	selector := labels.Everything()

	if c.cluster.Spec.Backup.Selector != nil {
		var err error
		if selector, err = metav1.LabelSelectorAsSelector(c.cluster.Spec.Backup.Selector); err != nil {
			return nil, err
		}
	}

	couchbaseBackupRestores := c.k8s.CouchbaseBackupRestores.List()

	restores := []couchbasev2.CouchbaseBackupRestore{}

	for _, restore := range couchbaseBackupRestores {
		if !selector.Matches(labels.Set(restore.Labels)) {
			continue
		}

		restores = append(restores, *restore)
	}

	return restores, nil
}

func (c *Cluster) reconcileBackup() error {
	if !c.cluster.Spec.Backup.Managed {
		return nil
	}

	requested, err := c.generateBackupResources()
	if err != nil {
		return err
	}

	current, err := c.listBackupResources()
	if err != nil {
		return err
	}

	for _, req := range requested {
		// Requested resource doesn't exist (as best we know...), so create it.
		if !current.contains(req) {
			if err := c.createBackupResource(req); err != nil {
				return err
			}

			continue
		}

		// Check for any of the resources needing an update.
		if err := c.updateBackupResource(req, current.find(req.backup.Name)); err != nil {
			return err
		}
	}

	for _, cur := range current {
		if cur.backup != nil {
			continue
		}

		// Current resource is no longer valid, so delete them.
		if err := c.deleteBackupResource(cur); err != nil {
			return err
		}
	}

	return nil
}

func (c *Cluster) reconcileBackupRestore() error {
	if !c.cluster.Spec.Backup.Managed {
		return nil
	}

	// poll for an existing CouchbaseBackupRestore resource
	currentRestores, err := c.gatherBackupRestores()
	if err != nil {
		return err
	}

	// for the current CouchbaseBackupRestores, loop through and see if they have a Job created
	for i := range currentRestores {
		currentRestore := &currentRestores[i]

		requested, err := c.generateRestoreJob(currentRestore)
		if err != nil {
			return err
		}

		k8sutil.ApplyBaseAnnotations(requested)

		// Check if restore job already exists.  If it doesn't, then it's never been created, or
		// less likely, been deleted and needs recreating.
		currentjob, ok := c.k8s.Jobs.Get(requested.Name)
		if !ok {
			log.Info("Restore created", "cbrestore", currentRestore.Name)

			c.raiseEvent(k8sutil.BackupRestoreCreateEvent(currentRestore.Name, c.cluster))

			createdJob, err := c.k8s.KubeClient.BatchV1().Jobs(c.cluster.Namespace).Create(context.Background(), requested, metav1.CreateOptions{})
			if err != nil {
				return err
			}

			log.Info("restore job created", "cbrestore", currentRestore.Name, "created job", createdJob.Name)

			continue
		}

		// Cleanup completed restores so that aren't rerun.
		if currentjob.Status.Succeeded == 1 {
			log.Info("Deleting successful restore", "cluster", c.namespacedName(), "restore", currentRestore.Name)

			if err := c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseBackupRestores(c.cluster.Namespace).Delete(context.Background(), currentRestore.Name, *metav1.NewDeleteOptions(0)); err != nil {
				return err
			}
		}
	}

	return nil
}
