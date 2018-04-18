package k8sutil

import (
	"fmt"

	cbapi "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	couchbaseContainerName     = "couchbase-server"
	couchbaseTlsVolumeName     = "couchbase-server-tls"
	couchbaseTlsVolumeMountDir = "/opt/couchbase/var/lib/couchbase/inbox"
)

// Creates pods with any PersistentVolumeClaims (PVCs)
// necessary for the Pod prior to creating the Pod.
func CreateCouchbasePod(kubeCli kubernetes.Interface, namespace string, clusterName string, m *couchbaseutil.Member, cs cbapi.ClusterSpec, version string, config cbapi.ServerConfig, owner metav1.OwnerReference) (*v1.Pod, error) {

	pod, err := createCouchbasePodSpec(m, clusterName, cs, version, config, owner)
	if err != nil {
		return nil, err
	}

	if config.Pod != nil {
		if len(config.Pod.VolumeMounts) > 0 {
			pod, err = addPodVolumes(kubeCli, pod, namespace, cs, config, owner)
			if err != nil {
				return nil, err
			}
		}
	}
	return CreatePod(kubeCli, namespace, pod)
}

// Add a persistent volume to the pod spec for each volumeMount.
// The volumes are first created via persistentVolumeClaims
func addPodVolumes(kubeCli kubernetes.Interface, pod *v1.Pod, namespace string, cs cbapi.ClusterSpec, config cbapi.ServerConfig, owner metav1.OwnerReference) (*v1.Pod, error) {

	volumes := []v1.Volume{}
	mounts := []v1.VolumeMount{}

	// Keep track of volumes generated from the same claim
	claimUsageCnt := make(map[string]int)

	for _, mount := range config.Pod.VolumeMounts {

		// every volume mount must have associated claim template
		// within the spec before we can add it to the pod
		if claim := cs.GetVolumeClaimTemplate(mount.Name); claim != nil {

			// Update PVC name
			if _, used := claimUsageCnt[claim.Name]; used {
				claimUsageCnt[claim.Name] += 1
			} else {
				claimUsageCnt[claim.Name] = 0
			}
			claim.Name = NameForPersistentVolumeClaim(claim.Name, pod.Name, claimUsageCnt[claim.Name])

			// Create PVC from the template
			pvc, err := createPersistentVolumeClaim(kubeCli, claim, namespace, owner)
			if err != nil {
				return nil, err
			}

			// Volumes will be added to Pod spec
			volume := podVolumeSpecForClaim(config.Name, pvc.Name)
			volumes = append(volumes, volume)

			// Mount point for Pod Container spec to reference volume by name
			mounts = append(mounts, v1.VolumeMount{Name: volume.Name, MountPath: mount.Path})
		} else {
			// It is invalid to have volumeMounts that do not
			// map to a volumeClaimTemplates
			return nil, fmt.Errorf("volume mounts (%s) does not map to any claimTemplates", mount.Name)
		}
	}

	// Add volumes to the pod Spec stateful volumes
	pod.Spec.Volumes = volumes
	for i, container := range pod.Spec.Containers {
		if container.Name == "couchbase-server" {
			pod.Spec.Containers[i].VolumeMounts = mounts
		}
	}
	return pod, nil
}

// Creates custom PVC from the generic spec
func createPersistentVolumeClaim(kubeCli kubernetes.Interface, claim *v1.PersistentVolumeClaim, namespace string, owner metav1.OwnerReference) (*v1.PersistentVolumeClaim, error) {

	addOwnerRefToObject(claim.GetObjectMeta(), owner)

	// can be mounted read/write mode to exactly 1 host
	claim.Spec.AccessModes = []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}
	return kubeCli.CoreV1().PersistentVolumeClaims(namespace).Create(claim)
}

func podVolumeSpecForClaim(configName, claimName string) v1.Volume {
	return v1.Volume{
		Name: claimName,
		VolumeSource: v1.VolumeSource{
			PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
				ClaimName: claimName},
		},
	}
}

// Deleting pods first deletes any persisted volume
// followed by the pod deletion itself.
func DeleteCouchbasePod(kubeCli kubernetes.Interface, namespace string, name string, opts *metav1.DeleteOptions) (rv_err error) {

	defer func() {
		if err := DeletePod(kubeCli, namespace, name, opts); err != nil {
			if rv_err != nil {
				rv_err = fmt.Errorf("error deleting persistent volumes: %v, and associated pod: %v", rv_err, err)
			} else {
				rv_err = err
			}
		}
	}()

	// Check if pod has any volumes to be deleted
	pod, err := kubeCli.Core().Pods(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		if !IsKubernetesResourceNotFoundError(err) {
			// It's unexpected to get an error other than
			// the pod already being deleted
			return err
		}
	}
	if len(pod.Spec.Volumes) > 0 {
		return deletePodVolumes(kubeCli, pod, namespace)
	}
	return nil
}

func deletePodVolumes(kubeCli kubernetes.Interface, pod *v1.Pod, namespace string) error {

	for _, volume := range pod.Spec.Volumes {
		if volume.VolumeSource.PersistentVolumeClaim != nil {
			err := kubeCli.Core().PersistentVolumeClaims(namespace).Delete(volume.Name, CascadeDeleteOptions(0))
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// Names of persistent volume claims are combinations of
// claim template, cluster name, and member.  An additional suffix
// is added to identify the claim as the member's Nth volume
// ie...: pvc-data-cb-example-0000-00, pvc-data-cb-example-0000-01
func NameForPersistentVolumeClaim(claimName string, memberName string, index int) string {
	return fmt.Sprintf("pvc-%s-%s-%02d", claimName, memberName, index)
}

// Couchbase pod spec with default configuration
func createCouchbasePodSpec(m *couchbaseutil.Member, clusterName string, cs cbapi.ClusterSpec, version string, ns cbapi.ServerConfig, owner metav1.OwnerReference) (*v1.Pod, error) {

	labels := createCouchbasePodLabels(m.Name, clusterName, ns)

	container := containerWithLivenessProbe(couchbaseContainer("", cs.BaseImage, version),
		couchbaseLivenessProbe())

	if ns.Pod != nil {
		container = containerWithRequirements(container, ns.Pod.Resources)
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        m.Name,
			Labels:      labels,
			Annotations: map[string]string{},
		},
		Spec: v1.PodSpec{
			Containers:    []v1.Container{container},
			RestartPolicy: v1.RestartPolicyNever,
			Hostname:      m.Name,
			Subdomain:     clusterName,
			Volumes: []v1.Volume{
				{Name: "couchbase-data",
					VolumeSource: v1.VolumeSource{EmptyDir: &v1.EmptyDirVolumeSource{}}},
			},
		},
	}
	if cs.AntiAffinity {
		pod = PodWithAntiAffinity(pod, clusterName)
	}

	applyPodPolicy(clusterName, pod, ns.Pod)

	if err := applyPodTlsConfiguration(cs, pod); err != nil {
		return nil, err
	}

	SetCouchbaseVersion(pod, cs.Version)

	addOwnerRefToObject(pod.GetObjectMeta(), owner)
	return pod, nil
}

func createCouchbasePodLabels(memberName, clusterName string, ns cbapi.ServerConfig) map[string]string {
	labels := map[string]string{
		labelApp:      "couchbase",
		labelNode:     memberName,
		labelNodeConf: ns.Name,
		labelCluster:  clusterName,
	}

	for _, s := range ns.Services {
		k := "couchbase_service_" + s
		labels[k] = "enabled"
	}

	return labels
}
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
		Name:  couchbaseContainerName,
		Image: imageName(baseImage, version),
		Ports: []v1.ContainerPort{
			{
				Name:          adminServicePortName,
				ContainerPort: int32(adminServicePort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          viewServicePortName,
				ContainerPort: int32(viewServicePort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          queryServicePortName,
				ContainerPort: int32(queryServicePort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          ftsServicePortName,
				ContainerPort: int32(ftsServicePort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          analyticsServicePortName,
				ContainerPort: int32(analyticsServicePort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          eventingServicePortName,
				ContainerPort: int32(eventingServicePort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          "cb-index-admin",
				ContainerPort: int32(9100),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          dataServicePortNameTLS,
				ContainerPort: int32(dataServicePortTLS),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          dataServicePortName,
				ContainerPort: int32(dataServicePort),
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
				Name:          adminServicePortNameTLS,
				ContainerPort: int32(adminServicePortTLS),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          viewServicePortNameTLS,
				ContainerPort: int32(viewServicePortTLS),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          queryServicePortNameTLS,
				ContainerPort: int32(queryServicePortTLS),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          ftsServicePortNameTLS,
				ContainerPort: int32(ftsServicePortTLS),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          analyticsServicePortNameTLS,
				ContainerPort: int32(analyticsServicePortTLS),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          eventingServicePortNameTLS,
				ContainerPort: int32(eventingServicePortTLS),
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
		labelCluster: clusterName,
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

// Given a pod, return a pointer to the couchbase container
func getCouchbaseContainer(pod *v1.Pod) (*v1.Container, error) {
	for index := range pod.Spec.Containers {
		if pod.Spec.Containers[index].Name == couchbaseContainerName {
			return &pod.Spec.Containers[index], nil
		}
	}
	return nil, fmt.Errorf("unable to locate couchbase container")
}

// Adds any necessary pod prerequisites before enabling TLS
func applyPodTlsConfiguration(cs cbapi.ClusterSpec, pod *v1.Pod) error {
	if cs.TLS != nil {
		// Static configuration:
		// * Defines a volume which contains the secrets necessary
		//   to explicitly define TLS certificates and keys
		// * Mounts the volume in in the correct location so that API
		//   calls to /node/controller/reloadCertificate succeed
		if cs.TLS.Static != nil {
			// Ensure the schema is correct
			// TODO: does this make sense not to be a pointer?
			if cs.TLS.Static.Member == nil {
				return fmt.Errorf("static tls member secret required")
			}

			// Add the TLS secret volume to the pod
			volume := v1.Volume{
				Name: couchbaseTlsVolumeName,
			}
			volume.VolumeSource.Secret = &v1.SecretVolumeSource{
				SecretName: cs.TLS.Static.Member.ServerSecret,
			}
			pod.Spec.Volumes = append(pod.Spec.Volumes, volume)

			// Mount the secret volume in Couchbase's inbox
			volumeMount := v1.VolumeMount{
				Name:      couchbaseTlsVolumeName,
				ReadOnly:  true,
				MountPath: couchbaseTlsVolumeMountDir,
			}
			container, err := getCouchbaseContainer(pod)
			if err != nil {
				return err
			}
			container.VolumeMounts = append(container.VolumeMounts, volumeMount)
		}
	}
	return nil
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
