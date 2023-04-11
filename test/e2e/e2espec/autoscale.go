package e2espec

import (
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
)

const (
	apiServerApp      = "custom-metrics-apiserver"
	apiRBACRoleName   = "custom-metrics-resource-reader"
	apiServerAppImage = "aemerycb/k8s-test-metrics-adapter:v2"
)

const (
	// Metric incrementing from 0-100 by 10.
	TestMetricIncrement = "test-metric-increment"

	// Metric decrementing from 100-0 by 10.
	TestMetricDecrement = "test-metric-decrement"

	// Default is to avoid scaling below 1.
	defaultMinPods int32 = 1

	// Default is to avoid scaling above 6.
	defaultMaxPods int32 = 6

	// Default target threshold for metrics.
	// For incrementing metrics, scale up occurs as this value is exceeded.
	// For decrementing metrics, scale down occurs as this value is subceed(ed).
	DefaultMetricTarget int64 = 80

	// Default for period of time required between successive scaling requests.
	defaultPeriodSeconds int32 = 15

	// Default value for native HorizontalPodAutoscaler stabilization.
	// This defines how long HPA must wait to perform initial scaling
	// or to allow direction changes.
	DefaultHPAStabilizationPeriod int32 = 30

	// Low HPA stabilization allows for immediate scaling in any direction.
	LowHPAStabilizationPeriod int32 = 0

	// Default value of 30 seconds for the Couchbase AutoscaleStabilizationPeriod.
	// This defines how long after a rebalance the cluster must wait before
	// accepting new scaling requests.
	DefaultClusterStabilizationPeriod = 120

	// Low Cluster Stabilization allows for cluster to go into
	// maintenance mode only during rebalance without any wait after.
	LowClusterStabilizationPeriod = 0
)

// Configuration for a HorizontalPodAutoscaler.
type HPAConfig struct {
	// Minimum number of Couchbase Pods when scaling down
	MinSize int32

	// Minimum number of Couchbase Pods when scaling up
	MaxSize int32

	// Name of metric being targeted
	TargetMetricName string

	// Target value of metric which results in scaling as
	// stats are above/below target
	TargetMetricValue int64

	// Scale up and down behaviors for tuning the rate of autoscaling operations
	Behavior *autoscalingv2.HorizontalPodAutoscalerBehavior
}

func newHPAConfig(min int32, max int32, targetValue int64, targetName string) *HPAConfig {
	return &HPAConfig{
		MinSize:           min,
		MaxSize:           max,
		TargetMetricName:  targetName,
		TargetMetricValue: targetValue,
		Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
			ScaleUp:   &autoscalingv2.HPAScalingRules{},
			ScaleDown: &autoscalingv2.HPAScalingRules{},
		},
	}
}

func NewIncrementingHPAConfig(targetValue int64) *HPAConfig {
	return newHPAConfig(defaultMinPods, defaultMaxPods, targetValue, TestMetricIncrement)
}

func NewDecrementingHPAConfig(targetValue int64) *HPAConfig {
	return newHPAConfig(defaultMinPods, defaultMaxPods, targetValue, TestMetricDecrement)
}

// Sets scaling stabilization window which controls period of time required
// for HPA to suggest initial scale down or change direction from scale up.
func (h *HPAConfig) WithScaleDownStabilizationWindow(period int32) *HPAConfig {
	h.Behavior.ScaleDown.StabilizationWindowSeconds = &period
	return h
}

// Sets scaling stabilization window which controls period of time required
// for HPA to suggest initial scale up or change direction from scale down.
func (h *HPAConfig) WithScaleUpStabilizationWindow(period int32) *HPAConfig {
	h.Behavior.ScaleUp.StabilizationWindowSeconds = &period
	return h
}

// Sets scaling in any direction to at most 1 pod.
func (h *HPAConfig) WithSinglePodScalingPolicy() *HPAConfig {
	policy := autoscalingv2.HPAScalingPolicy{
		Type:          autoscalingv2.PodsScalingPolicy,
		Value:         1,
		PeriodSeconds: defaultPeriodSeconds,
	}
	h.Behavior.ScaleUp.Policies = []autoscalingv2.HPAScalingPolicy{policy}
	h.Behavior.ScaleDown.Policies = []autoscalingv2.HPAScalingPolicy{policy}

	return h
}

// NewHorizontalPodAutoscaler returns spec of an HPA resource.
func NewHorizontalPodAutoscaler(name string, config *HPAConfig, metrics []autoscalingv2.MetricSpec) *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Name:       name,
				Kind:       "CouchbaseAutoscaler",
				APIVersion: "couchbase.com/v2",
			},
			Behavior:    config.Behavior,
			MinReplicas: &config.MinSize,
			MaxReplicas: config.MaxSize,
			Metrics:     metrics,
		},
	}
}

// NewAverageValueHPA is a HPA with a single metric
// targeting an average value among Pods.
func NewAverageValueHPA(name string, config *HPAConfig) *autoscalingv2.HorizontalPodAutoscaler {
	metrics := autoscalingv2.MetricSpec{
		Type: autoscalingv2.PodsMetricSourceType,
		Pods: &autoscalingv2.PodsMetricSource{
			Metric: autoscalingv2.MetricIdentifier{
				Name: config.TargetMetricName,
			},
			Target: autoscalingv2.MetricTarget{
				Type:         autoscalingv2.AverageValueMetricType,
				AverageValue: resource.NewQuantity(config.TargetMetricValue, resource.DecimalSI),
			},
		},
	}
	hpa := NewHorizontalPodAutoscaler(name, config, []autoscalingv2.MetricSpec{metrics})

	return hpa
}

// Generate service account used by custom metrics deployment.
func GenerateCustomMetricServiceAccount(namespace string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      apiServerApp,
			Namespace: namespace,
		},
	}
}

// Generate Custom Metrics Deployment for APIService to fetch metrics.
func GenerateCustomMetricDeployment(namespace string, clusterName string) *appsv1.Deployment {
	replicas := int32(1)

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      apiServerApp,
			Namespace: namespace,
			Labels: map[string]string{
				"app": apiServerApp,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": apiServerApp,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": apiServerApp,
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: apiServerApp,
					Containers: []corev1.Container{
						{
							Name:            apiServerApp,
							Image:           apiServerAppImage,
							ImagePullPolicy: "IfNotPresent",
							Args: []string{
								"/adaptor",
								"--namespace=" + namespace,
								"--cluster=" + clusterName,
								"--secure-port=6443",
								"--logtostderr=true",
								"--v=10",
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 8080,
								},
								{
									Name:          "https",
									ContainerPort: 6443,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "temp-vol",
									MountPath: "/tmp",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "temp-vol",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}
}

// Generate role to used by custom metric deployment.
func GenerateCustomMetricDeploymentRole(namespace string) *rbacv1.Role {
	rules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"namespaces",
				"pods",
				"services",
			},
			Verbs: []string{
				"get",
				"list",
			},
		},
	}

	return &rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "Role",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      apiRBACRoleName,
			Namespace: namespace,
		},
		Rules: rules,
	}
}

// Generate Role Binding to allow custom metric deployment
// to access required resources.
func GenerateCustomMetricDeploymentRoleBinding(namespace string) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "RoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      apiRBACRoleName,
			Namespace: namespace,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     apiRBACRoleName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      apiServerApp,
				Namespace: namespace,
			},
		},
	}
}

// Generates cluster role binding from the service account
// to the system:auth-delegator cluster role to delegate
// auth decisions to the Kubernetes core API server.
func GenerateAuthClusterRoleBinding(namespace string) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "custom-metrics:system:auth-delegator",
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "system:auth-delegator",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      apiServerApp,
				Namespace: namespace,
			},
		},
	}
}

// Generates role binding from the service account
// to the extension-apiserver-authentication-reader role.
// This allows the custom api-server deployment to access the
// extension-apiserver-authentication configmap.
//
// NOTE: The custom metric service always looks for this in
// kube-system namespace.  Pretty sure this can be changed.
//
// Ref: https://kubernetes.io/docs/tasks/extend-kubernetes/setup-extension-api-server/
func GenerateAPIExtensionRoleBinding(subjectNamespace string) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "RoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "custom-metrics-auth-reader",
			Namespace: "kube-system",
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     "extension-apiserver-authentication-reader",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      apiServerApp,
				Namespace: subjectNamespace,
			},
		},
	}
}

// Generate k8s resource for exposing custom metrics app.
func GenerateMetricService(namespace string) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      apiServerApp,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": apiServerApp,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "https",
					Protocol:   corev1.ProtocolTCP,
					Port:       443,
					TargetPort: intstr.FromInt(6443),
				},
				{
					Name:       "http",
					Protocol:   corev1.ProtocolTCP,
					Port:       80,
					TargetPort: intstr.FromInt(8080),
				},
			},
		},
	}
}

// Generate MetricAPIService for extending the api-server with custom metrics.
func GenerateMetricAPIService(namespace string) *apiregistrationv1.APIService {
	return &apiregistrationv1.APIService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "v1beta2.custom.metrics.k8s.io",
			Namespace: namespace,
		},
		Spec: apiregistrationv1.APIServiceSpec{
			Service: &apiregistrationv1.ServiceReference{
				Name:      apiServerApp,
				Namespace: namespace,
			},
			Group:                 "custom.metrics.k8s.io",
			Version:               "v1beta2",
			InsecureSkipTLSVerify: true,
			GroupPriorityMinimum:  100,
			VersionPriority:       200,
		},
	}
}

// GenerateMetricsPeerAuthRule creates Istio Peer Authentication
// rule for custom metrics to run in PERMISSIVE mode when Istio enabled.
func GenerateMetricsPeerAuthRule(namespace string) (*unstructured.Unstructured, schema.GroupVersionResource) {
	policy := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "security.istio.io/v1beta1",
			"kind":       "PeerAuthentication",
			"metadata": map[string]interface{}{
				"name":      "autoscaler",
				"namespace": namespace,
			},
			"spec": map[string]interface{}{
				"selector": map[string]interface{}{
					"matchLabels": map[string]string{
						"app": "custom-metrics-apiserver",
					},
				},
				"mtls": map[string]interface{}{
					"mode": "PERMISSIVE",
				},
			},
		},
	}

	gvr := schema.GroupVersionResource{
		Group:    "security.istio.io",
		Version:  "v1beta1",
		Resource: "peerauthentications",
	}

	return policy, gvr
}
