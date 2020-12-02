package e2eutil

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// syncGatewayResourceName is the name used for all sync gateway resources.
	syncGatewayResourceName = "test-sync-gateway"
)

// waitSyncGatewayAvailable waits for the sync gateway deployment to become available.
func waitSyncGatewayAvailable(k8s *types.Cluster, timeout time.Duration) error {
	callback := func() error {
		deployment, err := k8s.KubeClient.AppsV1().Deployments(k8s.Namespace).Get(context.Background(), syncGatewayResourceName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		for _, condition := range deployment.Status.Conditions {
			if condition.Type == appsv1.DeploymentAvailable && condition.Status == corev1.ConditionTrue {
				return nil
			}
		}

		return fmt.Errorf("sync-gateway deployment not available")
	}

	return retryutil.RetryFor(timeout, callback)
}

// createSyncGateway creates a sync gateway instance in the given cluster.
// Communication, being external has to be with a port-forward so creating
// a service is pointless.
func createSyncGateway(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, image, bucket string, auth *corev1.Secret, dns *corev1.Service, tls *TLSContext, timeout time.Duration) error {
	// Define and create the configuration secret.
	// Dynamically select the scheme based on TLS mode, also optionally add in CA
	// and client certificates as required.  As the config is going to be mounted
	// we piggy back the certificates on this mount for simplicity.
	scheme := "couchbase"

	if tls != nil {
		scheme = "couchbases"
	}

	username := k8s.DefaultSecret.Data[constants.SecretUsernameKey]
	password := k8s.DefaultSecret.Data[constants.SecretPasswordKey]

	if auth != nil {
		username = auth.Data[constants.SecretUsernameKey]
		password = auth.Data[constants.SecretPasswordKey]
	}

	databaseConfig := map[string]interface{}{
		"server":   scheme + "://" + cluster.Name + "-srv." + cluster.Namespace,
		"bucket":   bucket,
		"username": string(username),
		"password": string(password),
		"users": map[string]interface{}{
			"GUEST": map[string]interface{}{
				"disabled": false,
				"admin_channels": []interface{}{
					"*",
				},
			},
		},
		"allow_conflicts":             false,
		"revs_limit":                  20,
		"enable_shared_bucket_access": true,
	}

	if tls != nil {
		databaseConfig["cacertpath"] = "/etc/sync_gateway/ca.pem"

		if cluster.Spec.Networking.TLS.ClientCertificatePolicy != nil {
			databaseConfig["certpath"] = "/etc/sync_gateway/client.pem"
			databaseConfig["keypath"] = "/etc/sync_gateway/client.key"
		}
	}

	config := map[string]interface{}{
		"logging": map[string]interface{}{
			"console": map[string]interface{}{
				"enabled":   true,
				"log_level": "debug",
				"log_keys": []interface{}{
					"*",
				},
			},
		},
		"databases": map[string]interface{}{
			"db": databaseConfig,
		},
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		return err
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: syncGatewayResourceName,
		},
		Data: map[string][]byte{
			"config.json": configJSON,
		},
	}

	if tls != nil {
		secret.Data["ca.pem"] = tls.CA.Certificate
		secret.Data["client.pem"] = tls.ClientCert
		secret.Data["client.key"] = tls.ClientKey
	}

	if _, err := k8s.KubeClient.CoreV1().Secrets(k8s.Namespace).Create(context.Background(), secret, metav1.CreateOptions{}); err != nil {
		return err
	}

	// Define and create the deployment.
	// If a custom DNS service is specified override the default DNS configuration
	// in the sync-gateway containers.
	podTemplate := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app": syncGatewayResourceName,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "sync-gateway",
					Image: image,
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "config",
							MountPath: "/etc/sync_gateway",
							ReadOnly:  true,
						},
					},
					Env: []corev1.EnvVar{
						{
							Name:  "GOMAXPROCS",
							Value: "1",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "config",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: syncGatewayResourceName,
						},
					},
				},
			},
		},
	}

	if dns != nil {
		podTemplate.Spec.DNSPolicy = corev1.DNSNone
		podTemplate.Spec.DNSConfig = &corev1.PodDNSConfig{
			Nameservers: []string{
				dns.Spec.ClusterIP,
			},
			Searches: getSearchDomains(k8s),
		}
	}

	replicas := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: syncGatewayResourceName,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": syncGatewayResourceName,
				},
			},
			Template: podTemplate,
		},
	}

	if _, err := k8s.KubeClient.AppsV1().Deployments(k8s.Namespace).Create(context.Background(), deployment, metav1.CreateOptions{}); err != nil {
		return err
	}

	if err := waitSyncGatewayAvailable(k8s, timeout); err != nil {
		return err
	}

	return nil
}

func MustCreateSyncGateway(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, image, bucket string, auth *corev1.Secret, dns *corev1.Service, tls *TLSContext, timeout time.Duration) {
	if err := createSyncGateway(k8s, cluster, image, bucket, auth, dns, tls, timeout); err != nil {
		Die(t, err)
	}
}
