/*
Copyright 2023-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cluster

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/unreconcilable"
	v1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func TestBackupResourceListUnique(t *testing.T) {
	cronjob := &v1.CronJob{}

	cronjobs := backupResourcesList{
		backupResources{
			name: "cows",
		},
		backupResources{
			name: "go",
		},
		backupResources{
			name:        "moo",
			fullCronJob: cronjob,
		},
	}

	jobs := backupResourcesList{
		backupResources{
			name: "foo",
		},
		backupResources{
			name: "bar",
		},
		backupResources{
			name:               "moo",
			immediateBackupJob: &v1.Job{},
		},
	}

	expected := backupResourcesList{
		backupResources{
			name: "foo",
		},
		backupResources{
			name: "bar",
		},
		backupResources{
			name: "cows",
		},
		backupResources{
			name: "go",
		},
		backupResources{
			name:        "moo",
			fullCronJob: cronjob,
		},
	}

	unique := cronjobs.unique(jobs)

	// sort them
	sort.Slice(unique, func(i int, j int) bool {
		return unique[i].name > unique[j].name
	})
	sort.Slice(expected, func(i int, j int) bool {
		return expected[i].name > expected[j].name
	})

	for i := range unique {
		if unique[i] != expected[i] {
			t.Fatalf("unexpected element, expected %v but found %v", expected[i], unique[i])
		}
	}
}

func TestCreateBaseJobSpec(t *testing.T) {
	fsGroup := int64(1001)
	runAsUser := int64(8453)
	cluster := &couchbasev2.CouchbaseCluster{
		Spec: couchbasev2.ClusterSpec{
			Backup: couchbasev2.Backup{
				NodeSelector: map[string]string{"az": "A", "foo": "bar"},
				Tolerations: []corev1.Toleration{
					{
						Key:      "FooBar",
						Operator: corev1.TolerationOpExists,
					},
					{
						Key:      "BarFooCount",
						Operator: corev1.TolerationOpEqual,
						Value:    "11",
					},
				},
				ServiceAccount: "cb-example-sa",
				ImagePullSecrets: []corev1.LocalObjectReference{
					{
						Name: "/api/v1/secret/shhh",
					},
				},
			},
			Security: couchbasev2.CouchbaseClusterSecuritySpec{
				AdminSecret: "admin123",
				PodSecurityContext: &corev1.PodSecurityContext{
					FSGroup:   &fsGroup,
					RunAsUser: &runAsUser,
				},
			},
		},
	}
	c := &Cluster{
		cluster: cluster,
	}

	boLimit := int32(10)
	ttlSecondsAfterFinished := int32(64)
	container := corev1.Container{Name: "TestCOntainer"}
	volume := corev1.Volume{Name: "TestVolume"}
	jobSpec := c.createBaseJobSpec(&boLimit, &ttlSecondsAfterFinished, container, volume)

	validateBackupJob(t, c.cluster, jobSpec, &boLimit, &ttlSecondsAfterFinished, container, volume)
}

//nolint:goerr113
func validateBackupJob(t *testing.T, cluster *couchbasev2.CouchbaseCluster, jobSpec *v1.JobSpec, backoffLimit, ttlSecondsAfterFinished *int32, container corev1.Container, volume corev1.Volume) {
	jobTemplateSpec := jobSpec.Template.Spec
	backup := cluster.Spec.Backup
	errs := make([]error, 0)

	if !reflect.DeepEqual(jobTemplateSpec.NodeSelector, backup.NodeSelector) {
		errs = append(errs, fmt.Errorf("Expected jobspec NodeSelector to be: %s but got: %s", backup.NodeSelector, &jobTemplateSpec.NodeSelector))
	}

	if !reflect.DeepEqual(jobTemplateSpec.NodeSelector, backup.NodeSelector) {
		errs = append(errs, fmt.Errorf("Expected jobspec NodeSelector to be: %s but got: %s", backup.NodeSelector, &jobTemplateSpec.NodeSelector))
	}

	if !reflect.DeepEqual(backup.Tolerations, jobTemplateSpec.Tolerations) {
		errs = append(errs, fmt.Errorf("Expected jobspec Tolerations to be: %v but got: %v", backup.Tolerations, jobTemplateSpec.Tolerations))
	}

	if !reflect.DeepEqual(backup.ServiceAccount, jobTemplateSpec.ServiceAccountName) {
		errs = append(errs, fmt.Errorf("Expected jobspec ServiceAccountName to be: %s but got: %s", backup.ServiceAccount, jobTemplateSpec.ServiceAccountName))
	}

	if !reflect.DeepEqual(backup.ImagePullSecrets, jobTemplateSpec.ImagePullSecrets) {
		errs = append(errs, fmt.Errorf("Expected jobspec ImagePullSecrets to be: %v but got: %v", backup.ImagePullSecrets, jobTemplateSpec.ImagePullSecrets))
	}

	var expectedSecurityContext *corev1.PodSecurityContext

	if cluster.Spec.Security.PodSecurityContext != nil {
		// both cluster.Spec.SecurityContext (if present) and cluster.Spec.Security.PodSecurityContext
		// are equal.
		expectedSecurityContext = cluster.Spec.Security.PodSecurityContext
	} else {
		expectedSecurityContext = cluster.Spec.SecurityContext
	}

	if !reflect.DeepEqual(jobTemplateSpec.SecurityContext, expectedSecurityContext) {
		errs = append(errs, fmt.Errorf("Expected jobspec PodSecurityContext to be: %v but got: %v", expectedSecurityContext, jobTemplateSpec.SecurityContext))
	}

	if len(jobTemplateSpec.Containers) != 1 {
		errs = append(errs, fmt.Errorf("Expected jobspec to have a single container but found: %v", len(jobTemplateSpec.Containers)))
	} else if !reflect.DeepEqual(jobTemplateSpec.Containers[0], container) {
		errs = append(errs, fmt.Errorf("Expected jobspec container to be: %v but got: %v", container, jobTemplateSpec.Containers[0]))
	}

	if len(jobTemplateSpec.Volumes) != 2 {
		errs = append(errs, fmt.Errorf("Expected jobspec to have 2 volumes but found: %v", len(jobTemplateSpec.Volumes)))
	} else {
		if !reflect.DeepEqual(jobTemplateSpec.Volumes[0], volume) {
			errs = append(errs, fmt.Errorf("Expected jobspecs first volume to be: %v but got: %v", volume, jobTemplateSpec.Volumes[0]))
		}

		if jobTemplateSpec.Volumes[1].Name != "couchbase-admin" {
			errs = append(errs, fmt.Errorf("Expected second volume to have name: couchbase-admin but got: %s", jobTemplateSpec.Volumes[1].Name))
		}

		if jobTemplateSpec.Volumes[1].VolumeSource.Secret.SecretName != cluster.Spec.Security.AdminSecret {
			errs = append(errs, fmt.Errorf("Expected jobspec couchbase-admin volume to have secret name to be: %v but got: %v",
				cluster.Spec.Security.AdminSecret, jobTemplateSpec.Volumes[1].VolumeSource.Secret.SecretName))
		}
	}

	if !reflect.DeepEqual(jobTemplateSpec.SecurityContext, expectedSecurityContext) {
		errs = append(errs, fmt.Errorf("Expected jobspec PodSecurityContext to be: %v but got: %v", expectedSecurityContext, jobTemplateSpec.SecurityContext))
	}

	if jobSpec.BackoffLimit != backoffLimit {
		errs = append(errs, fmt.Errorf("Expected jobspec BackoffLimit to be: %v but got: %v", backoffLimit, jobSpec.BackoffLimit))
	}

	if jobSpec.TTLSecondsAfterFinished != ttlSecondsAfterFinished {
		errs = append(errs, fmt.Errorf("Expected jobspec ttlSecondsAfterFinished to be: %v but got: %v", ttlSecondsAfterFinished, jobSpec.TTLSecondsAfterFinished))
	}

	if len(errs) != 0 {
		t.Error(errors.Join(errs...))
	}
}

// TestCleanupUnusedBackupResourcesAfterSwitchToSnapshot checks that when an archive backup
// switches to snapshotBackup, its archive CronJob is removed but its PVC is kept, so the
// archive data from before the switch can still be restored. A backup that's still archive
// based keeps its CronJob.
func TestCleanupUnusedBackupResourcesAfterSwitchToSnapshot(t *testing.T) {
	cronJob := func(name string) *v1.CronJob {
		return &v1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: fakeClusterNamespace}}
	}

	switchedCronJob := cronJob("switched-backup-full")
	archiveCronJob := cronJob("archive-backup-full")
	switchedPVC := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "switched-backup", Namespace: fakeClusterNamespace}}

	kubeClient := kubefake.NewSimpleClientset(switchedCronJob, archiveCronJob, switchedPVC)

	c := &Cluster{
		k8s:            &client.Client{KubeClient: kubeClient},
		cluster:        &couchbasev2.CouchbaseCluster{ObjectMeta: metav1.ObjectMeta{Name: fakeClusterName, Namespace: fakeClusterNamespace}},
		log:            logf.Log.WithName("test"),
		unreconcilable: unreconcilable.New(fakeClusterName),
	}

	current := backupResourcesList{
		{
			name: "switched-backup",
			backup: &couchbasev2.CouchbaseBackup{
				ObjectMeta: metav1.ObjectMeta{Name: "switched-backup"},
				Spec:       couchbasev2.CouchbaseBackupSpec{SnapshotBackup: &couchbasev2.CouchbaseSnapshotBackupSpec{}},
			},
			fullCronJob: switchedCronJob,
			pvc:         switchedPVC,
		},
		{
			name:        "archive-backup",
			backup:      &couchbasev2.CouchbaseBackup{ObjectMeta: metav1.ObjectMeta{Name: "archive-backup"}},
			fullCronJob: archiveCronJob,
		},
	}

	if err := c.cleanupUnusedBackupResources(current); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	cronJobs, err := kubeClient.BatchV1().CronJobs(fakeClusterNamespace).List(t.Context(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list cronjobs: %v", err)
	}

	var remaining []string

	for _, item := range cronJobs.Items {
		remaining = append(remaining, item.Name)
	}

	if !reflect.DeepEqual(remaining, []string{"archive-backup-full"}) {
		t.Errorf("expected only archive-backup-full to remain, got %v", remaining)
	}

	if _, err := kubeClient.CoreV1().PersistentVolumeClaims(fakeClusterNamespace).Get(t.Context(), "switched-backup", metav1.GetOptions{}); err != nil {
		t.Errorf("expected the switched backup's PVC to be kept, got %v", err)
	}
}
