package e2eutil

import (
	"context"
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	autoscalingv1 "k8s.io/api/autoscaling/v1"
	autoscalingv2beta2 "k8s.io/api/autoscaling/v2beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NewAutoscaleCluster creates a new Autoscale enabled
// basic cluster, retrying if an error is encountered.
func NewAutoscaleCluster(t *testing.T, k8s *types.Cluster, size int) (*couchbasev2.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewBasicCluster(size)
	clusterSpec.Spec.Servers[0].AutoscaleEnabled = true
	clusterSpec.Spec.EnablePreviewScaling = true

	return newClusterFromSpec(t, k8s, clusterSpec)
}

func MustNewAutoscaleCluster(t *testing.T, k8s *types.Cluster, size int) *couchbasev2.CouchbaseCluster {
	cluster, err := NewAutoscaleCluster(t, k8s, size)
	if err != nil {
		Die(t, err)
	}

	return cluster
}

// NewAutoscaleClusterMDS creates new Autoscale enabled
// cluster with scaling enabled for specific servers.
func NewAutoscaleClusterMDS(t *testing.T, k8s *types.Cluster, size int, configName string, tls *TLSContext, policy *couchbasev2.ClientCertificatePolicy) (*couchbasev2.CouchbaseCluster, error) {
	// select only query config for autoscaling
	cluster := e2espec.NewBasicCluster(size)

	// If TLS is explcitly stated, then add it to the pod configuration.
	if tls != nil {
		cluster.Name = tls.ClusterName
		cluster.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
			Static: &couchbasev2.StaticTLS{
				ServerSecret:   tls.ClusterSecretName,
				OperatorSecret: tls.OperatorSecretName,
			},
		}

		if policy != nil {
			cluster.Spec.Networking.TLS.ClientCertificatePolicy = policy
			cluster.Spec.Networking.TLS.ClientCertificatePaths = []couchbasev2.ClientCertificatePath{
				{
					Path: "subject.cn",
				},
			}
		}
	}

	// add query only config with autoscale enabled
	queryConfig := couchbasev2.ServerConfig{
		Size:             size,
		Name:             configName,
		Services:         couchbasev2.ServiceList{couchbasev2.QueryService},
		AutoscaleEnabled: true,
	}
	cluster.Spec.Servers = append(cluster.Spec.Servers, queryConfig)

	return newClusterFromSpec(t, k8s, cluster)
}

func MustNewAutoscaleClusterMDS(t *testing.T, k8s *types.Cluster, size int, configName string, tls *TLSContext, policy *couchbasev2.ClientCertificatePolicy) *couchbasev2.CouchbaseCluster {
	cluster, err := NewAutoscaleClusterMDS(t, k8s, size, configName, tls, policy)
	if err != nil {
		Die(t, err)
	}

	return cluster
}

// MustDeleteCouchbaseAutoscaler requires successful deletion of autoscaler cr.
func MustDeleteCouchbaseAutoscaler(t *testing.T, k8s *types.Cluster, namespace string, autoscalerName string) {
	err := k8s.CRClient.CouchbaseV2().CouchbaseAutoscalers(namespace).Delete(context.Background(), autoscalerName, *metav1.NewDeleteOptions(0))
	if err != nil {
		Die(t, err)
	}
}

// MustGetCouchbaseAutoscaler requires successful deletion of autoscaler cr.
func MustGetCouchbaseAutoscaler(t *testing.T, k8s *types.Cluster, namespace string, autoscalerName string) *couchbasev2.CouchbaseAutoscaler {
	autoscaler, err := k8s.CRClient.CouchbaseV2().CouchbaseAutoscalers(namespace).Get(context.Background(), autoscalerName, metav1.GetOptions{})
	if err != nil {
		Die(t, err)
	}

	return autoscaler
}

// MustDisableCouchbaseAutoscaling requires successful disabling of autoscaling.
func MustDisableCouchbaseAutoscaling(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster) *couchbasev2.CouchbaseCluster {
	for i := range cluster.Spec.Servers {
		cluster.Spec.Servers[i].AutoscaleEnabled = false
	}

	cluster, err := k8s.CRClient.CouchbaseV2().CouchbaseClusters(cluster.Namespace).Update(context.Background(), cluster, metav1.UpdateOptions{})
	if err != nil {
		Die(t, err)
	}

	return cluster
}

// MustCreateAverageValueHPA requires successful creation of hpa resource.
func MustCreateAverageValueHPA(t *testing.T, k8s *types.Cluster, namespace string, name string, minSize int32, maxSize int32, metricName string, value int64) *autoscalingv2beta2.HorizontalPodAutoscaler {
	hpa := e2espec.NewAverageValueHPA(name, minSize, maxSize, metricName, value)
	hpa, err := k8s.AutoscaleClient.HorizontalPodAutoscalers(k8s.Namespace).Create(context.Background(), hpa, metav1.CreateOptions{})

	if err != nil {
		Die(t, err)
	}

	return hpa
}

// CreateCustomMetricServer creates custom metric api service and deployment.
// These collection of resources are used by HPA for reacting to target values.
// Specifically, the custom metric service exposes test metrics with various behaviors,
// such as incrementing, decrementing, average, and random value metrics.
func CreateCustomMetricServer(t *testing.T, k8s *types.Cluster, namespace string, clusterName string) (func(), error) {
	// generate resources
	serviceAccount := e2espec.GenerateCustomMetricServiceAccount(namespace)
	deploymentRole := e2espec.GenerateCustomMetricDeploymentRole(namespace)
	metricRoleBinding := e2espec.GenerateCustomMetricDeploymentRoleBinding(namespace)
	extensionRoleBinding := e2espec.GenerateAPIExtensionRoleBinding(namespace)
	authClusterRoleBinding := e2espec.GenerateAuthClusterRoleBinding(namespace)
	deployment := e2espec.GenerateCustomMetricDeployment(namespace, clusterName)
	deploymentService := e2espec.GenerateMetricService(namespace)
	serviceMetric := e2espec.GenerateMetricAPIService(namespace)

	// prepare to cleanup rootscoped resources
	cleanup := func() {
		_ = k8s.APIRegClient.APIServices().Delete(context.Background(), serviceMetric.Name, *metav1.NewDeleteOptions(0))
		_ = k8s.KubeClient.RbacV1().ClusterRoleBindings().Delete(context.Background(), authClusterRoleBinding.Name, *metav1.NewDeleteOptions(0))
		_ = k8s.KubeClient.RbacV1().RoleBindings("kube-system").Delete(context.Background(), extensionRoleBinding.Name, *metav1.NewDeleteOptions(0))
	}

	// create resources
	if _, err := k8s.KubeClient.CoreV1().ServiceAccounts(namespace).Create(context.Background(), serviceAccount, metav1.CreateOptions{}); err != nil {
		return cleanup, err
	}

	if _, err := k8s.KubeClient.RbacV1().Roles(namespace).Create(context.Background(), deploymentRole, metav1.CreateOptions{}); err != nil {
		return cleanup, err
	}

	if _, err := k8s.KubeClient.RbacV1().RoleBindings(namespace).Create(context.Background(), metricRoleBinding, metav1.CreateOptions{}); err != nil {
		return cleanup, err
	}

	// NOTE: see func comment about why this is being created in kube-system namespace
	if _, err := k8s.KubeClient.RbacV1().RoleBindings("kube-system").Create(context.Background(), extensionRoleBinding, metav1.CreateOptions{}); err != nil {
		return cleanup, err
	}

	if _, err := k8s.KubeClient.RbacV1().ClusterRoleBindings().Create(context.Background(), authClusterRoleBinding, metav1.CreateOptions{}); err != nil {
		return cleanup, err
	}

	if _, err := k8s.KubeClient.AppsV1().Deployments(namespace).Create(context.Background(), deployment, metav1.CreateOptions{}); err != nil {
		return cleanup, err
	}

	if _, err := k8s.KubeClient.CoreV1().Services(namespace).Create(context.Background(), deploymentService, metav1.CreateOptions{}); err != nil {
		return cleanup, err
	}

	if _, err := k8s.APIRegClient.APIServices().Create(context.Background(), serviceMetric, metav1.CreateOptions{}); err != nil {
		return cleanup, err
	}

	return cleanup, nil
}

// MustCreateCustomMetricServer requires creation of metric server or fails.
func MustCreateCustomMetricServer(t *testing.T, k8s *types.Cluster, namespace string, clusterName string) func() {
	cleanup, err := CreateCustomMetricServer(t, k8s, namespace, clusterName)
	if err != nil {
		cleanup()
		Die(t, err)
	}

	return cleanup
}

// UpdateScale changes scale of CouchbaseAutoscaler resource to requested size.
func UpdateScale(t *testing.T, k8s *types.Cluster, namespace string, name string, size int32) (*autoscalingv1.Scale, error) {
	scale, err := k8s.CRClient.CouchbaseV2().CouchbaseAutoscalers(namespace).GetScale(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	scale.Spec.Replicas = size

	return k8s.CRClient.CouchbaseV2().CouchbaseAutoscalers(namespace).UpdateScale(context.Background(), name, scale, metav1.UpdateOptions{})
}

// MustUpdateScale requires successful scale update.
func MustUpdateScale(t *testing.T, k8s *types.Cluster, namespace string, name string, size int32) *autoscalingv1.Scale {
	scale, err := UpdateScale(t, k8s, namespace, name, size)
	if err != nil {
		Die(t, err)
	}

	return scale
}
