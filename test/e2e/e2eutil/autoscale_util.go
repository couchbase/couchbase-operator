package e2eutil

import (
	"context"
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	autoscalingv1 "k8s.io/api/autoscaling/v1"
	autoscalingv2beta2 "k8s.io/api/autoscaling/v2beta2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// HPAManager creates a Couchbase Cluster and associates it with
// HorizontalPodAutoscalers (HPA). A HPA will be created for each
// couchbase config with autoscaling enable.
type HPAManager struct {
	CouchbaseCluster *couchbasev2.CouchbaseCluster

	HorizontalPodAutoscalers []*autoscalingv2beta2.HorizontalPodAutoscaler

	Cleanup func()
}

func MustNewHPAManager(t *testing.T, k8s *types.Cluster, couchbaseOptions *e2espec.ClusterOptions, hpaConfigs ...*e2espec.HPAConfig) *HPAManager {
	// Begin with basic autoscale enabled cluster
	clusterSpec := MustNewAutoscaleCluster(t, k8s, couchbaseOptions)

	// Create HPA to target specified metric
	autoscalers := []*autoscalingv2beta2.HorizontalPodAutoscaler{}

	for i, serverConfig := range clusterSpec.Spec.Servers {
		// HPA references CouchbaseAutoscaler reference which must exist
		autoscalerName := serverConfig.AutoscalerName(clusterSpec.Name)
		MustWaitUntilCouchbaseAutoscalerExists(t, k8s, clusterSpec, autoscalerName, 1*time.Minute)

		// HPA can be configured differently for each server config when multiple HPAConfigs are provided
		config := hpaConfigs[0]
		if len(hpaConfigs) > i {
			config = hpaConfigs[i]
		}

		hpa := MustCreateAverageValueHPA(t, k8s, clusterSpec.Namespace, autoscalerName, config)
		autoscalers = append(autoscalers, hpa)
	}

	// Create the metric server for metric generation
	cleanup := MustCreateCustomMetricServer(t, k8s, clusterSpec.Namespace, clusterSpec.Name)

	return &HPAManager{
		CouchbaseCluster:         clusterSpec,
		HorizontalPodAutoscalers: autoscalers,
		Cleanup:                  cleanup,
	}
}

// MustNewAutoscaleCluster creates a new Autoscale enabled
// basic cluster, retrying if an error is encountered.
func MustNewAutoscaleCluster(t *testing.T, k8s *types.Cluster, options *e2espec.ClusterOptions) *couchbasev2.CouchbaseCluster {
	clusterSpec := e2espec.NewBasicCluster(options)
	clusterSpec.Spec.EnablePreviewScaling = false

	for i := range clusterSpec.Spec.Servers {
		clusterSpec.Spec.Servers[i].AutoscaleEnabled = true
	}

	return MustNewClusterFromSpec(t, k8s, clusterSpec)
}

// MustNewAutoscaleClusterMDS creates new Autoscale enabled
// cluster with scaling enabled for specific servers.
func MustNewAutoscaleClusterMDS(t *testing.T, k8s *types.Cluster, options *e2espec.ClusterOptions, tls *TLSContext, policy *couchbasev2.ClientCertificatePolicy) *couchbasev2.CouchbaseCluster {
	// select only query config for autoscaling
	cluster := e2espec.NewBasicCluster(options)
	applyTLS(cluster, tls)
	applyMTLS(cluster, policy)

	// add query only config with autoscale enabled
	queryConfig := couchbasev2.ServerConfig{
		Size:             options.Topology[0].Size,
		Name:             constants.CouchbaseServerAltConfig,
		Services:         couchbasev2.ServiceList{couchbasev2.QueryService},
		AutoscaleEnabled: true,
	}
	cluster.Spec.Servers = append(cluster.Spec.Servers, queryConfig)

	return MustNewClusterFromSpec(t, k8s, cluster)
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

// MustCreateAverageValueHPA uses Averaging algorithm on target metrics to determine resize activity.
func MustCreateAverageValueHPA(t *testing.T, k8s *types.Cluster, namespace string, name string, config *e2espec.HPAConfig) *autoscalingv2beta2.HorizontalPodAutoscaler {
	hpa := e2espec.NewAverageValueHPA(name, config)
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

// MustValidateAutoscaleReadyStatus requires autoscale ready status to match the provided status argument.
func MustValidateAutoscaleReadyStatus(t *testing.T, k8s *types.Cluster, clusterNamespace string, clusterName string, status v1.ConditionStatus) {
	cluster, err := k8s.CRClient.CouchbaseV2().CouchbaseClusters(clusterNamespace).Get(context.Background(), clusterName, metav1.GetOptions{})
	if err != nil {
		Die(t, err)
	}

	readyCondition := cluster.Status.GetCondition(couchbasev2.ClusterConditionAutoscaleReady)
	if readyCondition == nil {
		err := fmt.Errorf("autoscale condition is undefined")
		Die(t, err)
	} else if readyCondition.Status != status {
		// Condition is not immediately set, let's wait for it
		MustWaitForClusterCondition(t, k8s, couchbasev2.ClusterConditionAutoscaleReady, status, cluster, 5*time.Minute)
	}
}

// MustValidateAutoscalerSize requires size of autoscaler spec to match the provided size argument.
func MustValidateAutoscalerSize(t *testing.T, k8s *types.Cluster, namespace string, name string, size int) {
	autoscaler := MustGetCouchbaseAutoscaler(t, k8s, namespace, name)
	if autoscaler.Spec.Size != size {
		Die(t, fmt.Errorf("expected autoscale size to be %d, but got %d", size, autoscaler.Spec.Size))
	}
}
