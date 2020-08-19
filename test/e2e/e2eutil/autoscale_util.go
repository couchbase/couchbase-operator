package e2eutil

import (
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
func NewAutoscaleCluster(t *testing.T, k8s *types.Cluster, size int, autoscaleServers []string) (*couchbasev2.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewBasicCluster(size)
	clusterSpec.Spec.Servers[0].AutoscaleEnabled = true

	return newClusterFromSpec(t, k8s, clusterSpec)
}

func MustNewAutoscaleCluster(t *testing.T, k8s *types.Cluster, size int, autoscaleServers []string) *couchbasev2.CouchbaseCluster {
	cluster, err := NewAutoscaleCluster(t, k8s, size, autoscaleServers)
	if err != nil {
		Die(t, err)
	}

	return cluster
}

// NewAutoscaleMDSCluster creates new Autoscale enabled
// cluster with scaling enabled for specific servers.
func NewAutoscaleClusterMDS(t *testing.T, k8s *types.Cluster, size int, configName string) (*couchbasev2.CouchbaseCluster, error) {
	// select only query config for autoscaling
	cluster := e2espec.NewBasicCluster(size)

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

func MustNewAutoscaleClusterMDS(t *testing.T, k8s *types.Cluster, size int, configName string) *couchbasev2.CouchbaseCluster {
	cluster, err := NewAutoscaleClusterMDS(t, k8s, size, configName)
	if err != nil {
		Die(t, err)
	}

	return cluster
}

// MustDeleteCouchbaseAutoscaler requires successful deletion of autoscaler cr.
func MustDeleteCouchbaseAutoscaler(t *testing.T, k8s *types.Cluster, namespace string, autoscalerName string) {
	err := k8s.CRClient.CouchbaseV2().CouchbaseAutoscalers(namespace).Delete(autoscalerName, metav1.NewDeleteOptions(0))
	if err != nil {
		Die(t, err)
	}
}

// MustGetCouchbaseAutoscaler requires successful deletion of autoscaler cr.
func MustGetCouchbaseAutoscaler(t *testing.T, k8s *types.Cluster, namespace string, autoscalerName string) *couchbasev2.CouchbaseAutoscaler {
	autoscaler, err := k8s.CRClient.CouchbaseV2().CouchbaseAutoscalers(namespace).Get(autoscalerName, metav1.GetOptions{})
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

	cluster, err := k8s.CRClient.CouchbaseV2().CouchbaseClusters(cluster.Namespace).Update(cluster)
	if err != nil {
		Die(t, err)
	}

	return cluster
}

// MustNewAverageValueHPA requires successful creation of hpa resource.
func MustCreateAverageValueHPA(t *testing.T, k8s *types.Cluster, namespace string, name string, minSize int32, maxSize int32, metricName string, value int64) *autoscalingv2beta2.HorizontalPodAutoscaler {
	hpa := e2espec.NewAverageValueHPA(name, minSize, maxSize, metricName, value)
	hpa, err := k8s.AutoscaleClient.HorizontalPodAutoscalers(k8s.Namespace).Create(hpa)

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
		_ = k8s.APIRegClient.APIServices().Delete(serviceMetric.Name, metav1.NewDeleteOptions(0))
		_ = k8s.KubeClient.RbacV1().ClusterRoleBindings().Delete(authClusterRoleBinding.Name, metav1.NewDeleteOptions(0))
		_ = k8s.KubeClient.RbacV1().RoleBindings("kube-system").Delete(extensionRoleBinding.Name, metav1.NewDeleteOptions(0))
	}

	// create resources
	if _, err := k8s.KubeClient.CoreV1().ServiceAccounts(namespace).Create(serviceAccount); err != nil {
		return cleanup, err
	}

	if _, err := k8s.KubeClient.RbacV1().Roles(namespace).Create(deploymentRole); err != nil {
		return cleanup, err
	}

	if _, err := k8s.KubeClient.RbacV1().RoleBindings(namespace).Create(metricRoleBinding); err != nil {
		return cleanup, err
	}

	// NOTE: see func comment about why this is being created in kube-system namespace
	if _, err := k8s.KubeClient.RbacV1().RoleBindings("kube-system").Create(extensionRoleBinding); err != nil {
		return cleanup, err
	}

	if _, err := k8s.KubeClient.RbacV1().ClusterRoleBindings().Create(authClusterRoleBinding); err != nil {
		return cleanup, err
	}

	if _, err := k8s.KubeClient.AppsV1().Deployments(namespace).Create(deployment); err != nil {
		return cleanup, err
	}

	if _, err := k8s.KubeClient.CoreV1().Services(namespace).Create(deploymentService); err != nil {
		return cleanup, err
	}

	if _, err := k8s.APIRegClient.APIServices().Create(serviceMetric); err != nil {
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
	scale, err := k8s.CRClient.CouchbaseV2().CouchbaseAutoscalers(namespace).GetScale(name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	scale.Spec.Replicas = size

	return k8s.CRClient.CouchbaseV2().CouchbaseAutoscalers(namespace).UpdateScale(name, scale)
}

// MustUpdateScale requires successful scale update.
func MustUpdateScale(t *testing.T, k8s *types.Cluster, namespace string, name string, size int32) *autoscalingv1.Scale {
	scale, err := UpdateScale(t, k8s, namespace, name, size)
	if err != nil {
		Die(t, err)
	}

	return scale
}
