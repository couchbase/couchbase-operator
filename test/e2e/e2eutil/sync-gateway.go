/*
Copyright 2020-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2eutil

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// syncGatewayResourceName is the name used for all sync gateway resources.
	syncGatewayResourceName = "test-sync-gateway"

	// syncGatewayAPIPort is what clients connect to.
	syncGatewayAPIPort = 4984

	// syncGatewayMetricsPort is what prometheus connects to.
	syncGatewayMetricsPort = 4986
)

// applyImagePullSecrets applies the k8s pull secrets to sync-gateway pod spec
// in order to use sync-gateway images from private registries.
func applyImagePullSecrets(podTemplate *corev1.PodTemplateSpec, imagePullSecrets []string) {
	if imagePullSecrets == nil {
		return
	}

	references := make([]corev1.LocalObjectReference, len(imagePullSecrets))

	for i, secret := range imagePullSecrets {
		references[i].Name = secret
	}

	podTemplate.Spec.ImagePullSecrets = references
}

// createSyncGateway creates a sync gateway instance in the given cluster.
// Communication, being external has to be with a port-forward so creating
// a service is pointless.
func createSyncGateway(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, image, bucket string, auth *corev1.Secret, dns *corev1.Service, serverTLS, clientTLS *TLSContext, timeout time.Duration) error {
	// Define and create the configuration secret.
	// Dynamically select the scheme based on TLS mode, also optionally add in CA
	// and client certificates as required.  As the config is going to be mounted
	// we piggy back the certificates on this mount for simplicity.
	scheme := "couchbase"

	if serverTLS != nil {
		scheme = "couchbases"
	}

	query := url.Values{}

	// We only ever test using DNS, either local or forwarded, so addresses should
	// always be "default".  We explcitly state the network for consistency across
	// all clients.  The only time this should be set to external is if SGW is
	// provisioned in a different Kubernetes cluster to the Couchbase cluster, and
	// only if we are using some form of DNAT e.g. LoadBalancer or NodePort networking.
	query.Add("network", "default")

	connstr := url.URL{
		Scheme:   scheme,
		Host:     fmt.Sprintf("%s-srv.%s", cluster.Name, cluster.Namespace),
		RawQuery: query.Encode(),
	}

	username := k8s.DefaultSecret.Data[constants.SecretUsernameKey]
	password := k8s.DefaultSecret.Data[constants.SecretPasswordKey]

	if auth != nil {
		username = auth.Data[constants.SecretUsernameKey]
		password = auth.Data[constants.SecretPasswordKey]
	}

	databaseConfig := map[string]interface{}{
		"server":   connstr.String(),
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

	if serverTLS != nil {
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

	if serverTLS != nil {
		secret.Data["ca.pem"] = serverTLS.CA.Certificate
		secret.Data["client.pem"] = clientTLS.ClientCert
		secret.Data["client.key"] = clientTLS.ClientKey
	}

	if _, err := k8s.KubeClient.CoreV1().Secrets(k8s.Namespace).Create(context.Background(), secret, metav1.CreateOptions{}); err != nil {
		return err
	}

	// Define and create the deployment.
	// If a custom DNS service is specified override the default DNS configuration
	// in the sync-gateway containers.
	// Remember folks, we document what we test, and test what we document, so keep
	// this N Sync with the tutorials.
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
					Ports: []corev1.ContainerPort{
						{
							Name:          "http-api",
							ContainerPort: int32(syncGatewayAPIPort),
						},
						{
							Name:          "http-metrics",
							ContainerPort: int32(syncGatewayMetricsPort),
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1"),
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
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

	// Adding imagePullSecrets for SGW image.
	applyImagePullSecrets(&podTemplate, k8s.PullSecrets)

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

	deployment, err = k8s.KubeClient.AppsV1().Deployments(k8s.Namespace).Create(context.Background(), deployment, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	if err := retryutil.RetryFor(timeout, ResourceCondition(k8s, deployment, string(appsv1.DeploymentAvailable), string(corev1.ConditionTrue))); err != nil {
		return err
	}

	return nil
}

func MustCreateSyncGateway(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, image, bucket string, auth *corev1.Secret, dns *corev1.Service, serverTLS *TLSContext, timeout time.Duration) {
	if err := createSyncGateway(k8s, cluster, image, bucket, auth, dns, serverTLS, serverTLS, timeout); err != nil {
		Die(t, err)
	}
}

func MustCreateSyncGatewayWithMultipleCAs(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, image, bucket string, auth *corev1.Secret, dns *corev1.Service, serverTLS, clientTLS *TLSContext, timeout time.Duration) {
	if err := createSyncGateway(k8s, cluster, image, bucket, auth, dns, serverTLS, clientTLS, timeout); err != nil {
		Die(t, err)
	}
}
