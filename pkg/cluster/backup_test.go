package cluster

import (
	"fmt"
	"reflect"
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	v1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

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
