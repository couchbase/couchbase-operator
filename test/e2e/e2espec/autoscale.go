package e2espec

import (
	appsv1 "k8s.io/api/apps/v1"
	v2beta2 "k8s.io/api/autoscaling/v2beta2"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
)

const (
	apiServerApp      = "custom-metrics-apiserver"
	apiRBACRoleName   = "custom-metrics-resource-reader"
	apiServerAppImage = "tahmmee/k8s-test-metrics-adapter-amd64:latest"
)

// NewHorizontalPodAutoscaler returns spec of an HPA resource.
func NewHorizontalPodAutoscaler(name string, minSize int32, maxSize int32, metrics []v2beta2.MetricSpec) *v2beta2.HorizontalPodAutoscaler {
	return &v2beta2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v2beta2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: v2beta2.CrossVersionObjectReference{
				Name:       name,
				Kind:       "CouchbaseAutoscaler",
				APIVersion: "couchbase.com/v2",
			},
			MinReplicas: &minSize,
			MaxReplicas: maxSize,
			Metrics:     metrics,
		},
	}
}

// NewAverageValueHPA is a HPA with a single metric
// targeting an average value among Pods.
func NewAverageValueHPA(name string, minSize int32, maxSize int32, metricName string, value int64) *v2beta2.HorizontalPodAutoscaler {
	metrics := v2beta2.MetricSpec{
		Type: v2beta2.PodsMetricSourceType,
		Pods: &v2beta2.PodsMetricSource{
			Metric: v2beta2.MetricIdentifier{
				Name: metricName,
			},
			Target: v2beta2.MetricTarget{
				Type:         v2beta2.AverageValueMetricType,
				AverageValue: resource.NewQuantity(value, resource.DecimalSI),
			},
		},
	}
	hpa := NewHorizontalPodAutoscaler(name, minSize, maxSize, []v2beta2.MetricSpec{metrics})

	return hpa
}

// TODO: in k8s 1.18 we can test HPA with scale behaviors.
func NewBehaviorAutoscaler() {
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
//       kube-system namespace.  Pretty sure this can be changed.
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
