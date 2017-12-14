package k8sutil

import (
	"fmt"

	cbapi "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func imageName(baseImage, version string) string {
	return fmt.Sprintf("%s:%s", baseImage, version)
}

func couchbaseVolumeMounts() []v1.VolumeMount {
	return []v1.VolumeMount{
		{Name: couchbaseVolumeName, MountPath: couchbaseVolumeMountDir},
	}
}

func couchbaseContainer(commands, baseImage, version string) v1.Container {
	c := v1.Container{
		Name:  "couchbase-server",
		Image: imageName(baseImage, version),
		Ports: []v1.ContainerPort{
			{
				Name:          "cb-admin",
				ContainerPort: int32(8091),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          "cb-views",
				ContainerPort: int32(8092),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          "cb-query",
				ContainerPort: int32(8093),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          "cb-search",
				ContainerPort: int32(8094),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          "cb-index-admin",
				ContainerPort: int32(9100),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          "cb-data-ssl",
				ContainerPort: int32(11207),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          "cb-data",
				ContainerPort: int32(11210),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          "cb-moxi",
				ContainerPort: int32(11211),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          "cb-xdcr-ssl-1",
				ContainerPort: int32(11214),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          "cb-xdcr-ssl-2",
				ContainerPort: int32(11215),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          "cb-admin-ssl",
				ContainerPort: int32(18091),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          "cb-views-ssl",
				ContainerPort: int32(18092),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          "cb-query-ssl",
				ContainerPort: int32(18093),
				Protocol:      v1.ProtocolTCP,
			},
		},
		VolumeMounts: couchbaseVolumeMounts(),
	}

	return c
}

func PodWithAntiAffinity(pod *v1.Pod, clusterName string) *v1.Pod {
	// set pod anti-affinity with the pods that belongs to the same couchbase cluster
	ls := &metav1.LabelSelector{MatchLabels: map[string]string{
		"couchbase_cluster": clusterName,
	}}
	return podWithAntiAffinity(pod, ls)
}

func podWithAntiAffinity(pod *v1.Pod, ls *metav1.LabelSelector) *v1.Pod {
	affinity := &v1.Affinity{
		PodAntiAffinity: &v1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
				{
					LabelSelector: ls,
					TopologyKey:   "kubernetes.io/hostname",
				},
			},
		},
	}

	pod.Spec.Affinity = affinity
	return pod
}

func PodWithNodeSelector(p *v1.Pod, ns map[string]string) *v1.Pod {
	p.Spec.NodeSelector = ns
	return p
}

func applyPodPolicy(clusterName string, pod *v1.Pod, policy *cbapi.PodPolicy) {
	if policy == nil {
		return
	}

	if len(policy.NodeSelector) != 0 {
		pod = PodWithNodeSelector(pod, policy.NodeSelector)
	}
	if len(policy.Tolerations) != 0 {
		pod.Spec.Tolerations = policy.Tolerations
	}
	if policy.AutomountServiceAccountToken != nil {
		pod.Spec.AutomountServiceAccountToken = policy.AutomountServiceAccountToken
	}

	mergeLabels(pod.Labels, policy.Labels)

	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == "couchbase" {
			pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, policy.CouchbaseEnv...)
		}
	}
}

func containerWithLivenessProbe(c v1.Container, lp *v1.Probe) v1.Container {
	c.LivenessProbe = lp
	return c
}

func containerWithRequirements(c v1.Container, r v1.ResourceRequirements) v1.Container {
	c.Resources = r
	return c
}

func couchbaseLivenessProbe() *v1.Probe {
	cmd := "true" // TODO - Needs to be a real command
	return &v1.Probe{
		Handler: v1.Handler{
			Exec: &v1.ExecAction{
				Command: []string{"/bin/sh", "-ec", cmd},
			},
		},
		InitialDelaySeconds: 10,
		TimeoutSeconds:      10,
		PeriodSeconds:       60,
		FailureThreshold:    3,
	}
}

// IsPodReady returns false if the Pod Status is nil
func IsPodReady(pod *v1.Pod) bool {
	condition := getPodReadyCondition(&pod.Status)
	return condition != nil && condition.Status == v1.ConditionTrue
}

func getPodReadyCondition(status *v1.PodStatus) *v1.PodCondition {
	for i := range status.Conditions {
		if status.Conditions[i].Type == v1.PodReady {
			return &status.Conditions[i]
		}
	}
	return nil
}
