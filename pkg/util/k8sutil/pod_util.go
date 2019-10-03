package k8sutil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	cberrors "github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/scheduler"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	couchbaseContainerName          = "couchbase-server"
	couchbaseTlsVolumeName          = "couchbase-server-tls"
	couchbaseTlsVolumeMountDir      = "/opt/couchbase/var/lib/couchbase/inbox"
	couchbaseVolumeDefaultConfigDir = "/opt/couchbase/var/lib/couchbase"
	CouchbaseVolumeMountLogsDir     = "/opt/couchbase/var/lib/couchbase/logs"
	couchbaseVolumeDefaultEtcDir    = "/opt/couchbase/etc"
	CouchbaseVolumeMountDataDir     = "/mnt/data"
	CouchbaseVolumeMountIndexDir    = "/mnt/index"
	defaultSubPathName              = "default"
	etcSubPathName                  = "etc"
	readinessFile                   = "/tmp/ready"
)

// Creates pods with any PersistentVolumeClaims (PVCs)
// necessary for the Pod prior to creating the Pod.
func CreateCouchbasePod(kubeCli kubernetes.Interface, scheduler scheduler.Scheduler, cluster *couchbasev2.CouchbaseCluster, m *couchbaseutil.Member, config couchbasev2.ServerConfig, ctx context.Context) (*v1.Pod, error) {

	pod, pvcs, err := CreateCouchbasePodSpec(kubeCli, m, cluster, config)
	if err != nil {
		return nil, err
	}

	if err := scheduler.Create(pod); err != nil {
		return nil, err
	}

	// Create PVCs if required.
	for _, pvc := range pvcs {
		if _, err = createPersistentVolumeClaim(kubeCli, pvc, cluster.Namespace, cluster.AsOwner()); err != nil {
			return nil, err
		}

		// If we are creating a default mount, then also create an init container to
		// copy Couchbase's etc directory onto the PVC.
		if pvc.Annotations[constants.AnnotationVolumeMountPath] == couchbaseVolumeDefaultConfigDir {
			initContainer := couchbaseInitContainer(cluster.Spec.Image, pvc.Name)
			pod.Spec.InitContainers = append(pod.Spec.InitContainers, initContainer)
		}
	}

	// Add ownership information only if we are going to create the resource.
	addOwnerRefToObject(pod.GetObjectMeta(), cluster.AsOwner())
	return CreatePod(kubeCli, cluster.Namespace, pod)
}

// Add a persistent volume to the pod spec for each volumeMount.
// The volumes are first created via persistentVolumeClaims
// Volumes that already exist are reused
func addPodVolumes(kubeCli kubernetes.Interface, pod *v1.Pod, cluster *couchbasev2.CouchbaseCluster, config couchbasev2.ServerConfig) ([]*v1.PersistentVolumeClaim, error) {
	// No mounts are required, do nothing
	if config.GetVolumeMounts() == nil {
		return nil, nil
	}

	pvcs := []*v1.PersistentVolumeClaim{}

	mounts := []v1.VolumeMount{}
	mountPaths, err := getPathsToPersist(config.Pod.VolumeMounts)
	if err != nil {
		return nil, err
	}

	version, err := CouchbaseVersion(cluster.Spec.Image)
	if err != nil {
		return nil, err
	}

	// Keep track of volumes generated from the same claim
	claimUsageCnt := map[string]int{}

	for mountName, claimName := range mountPaths {
		// every volume mount must have associated claim template
		// within the spec before we can add it to the pod
		claim := cluster.Spec.GetVolumeClaimTemplate(claimName)
		if claim == nil {
			return nil, fmt.Errorf("claim (%s) does not map to any claimTemplates", claimName)
		}

		// Update PVC name
		claimUsageCnt[claimName]++

		// Find volumes that already exist for this mount path
		// to allow pod recovery. Otherwise, create a new PVC
		mountPath := pathForVolumeMountName(mountName)
		pvc, _ := findMemberPVC(kubeCli, pod.Name, cluster.Name, cluster.Namespace, mountPath)
		if pvc == nil {
			// Label and Annotate so that volumes
			// can be easily targeted when recovering pods
			claim.Labels = map[string]string{
				constants.LabelApp:        constants.App,
				constants.LabelNode:       pod.Name,
				constants.LabelCluster:    cluster.Name,
				constants.LabelVolumeName: claimName,
			}
			claim.SetAnnotations(map[string]string{
				constants.AnnotationVolumeMountPath:     mountPath,
				constants.AnnotationVolumeNodeConf:      config.Name,
				constants.CouchbaseVersionAnnotationKey: version,
			})
			if serverGroup, ok := pod.Spec.NodeSelector[constants.ServerGroupLabel]; ok {
				claim.Annotations[constants.ServerGroupLabel] = serverGroup
			}
			applyBaseAnnotations(claim.GetObjectMeta())
			if gid := cluster.Spec.GetFSGroup(); gid != nil {
				claim.Annotations["pv.beta.kubernetes.io/gid"] = fmt.Sprintf("%d", *gid)
			}
			claim.Name = NameForPersistentVolumeClaim(pod.Name, claimUsageCnt[claimName], mountName)
			pvcs = append(pvcs, claim)
			pvc = claim
		} else {
			// Get availability zone of the Volumes and apply to Pod
			// so that it is honored by the scheduler.
			// When group is an empty string then scheduler will decide best
			// group to use.
			if group, ok := pvc.Annotations[constants.ServerGroupLabel]; ok {
				pod.Spec.NodeSelector[constants.ServerGroupLabel] = group
			}
		}

		// Volumes will be added to Pod spec
		volume := podVolumeSpecForClaim(pvc.Name)
		pod.Spec.Volumes = append(pod.Spec.Volumes, volume)

		// Mount point for Pod Container spec to reference volume by name.
		if mountName == couchbasev2.DefaultVolumeMount {

			// Default mount consists of 2 mounts for default(config) and etc data
			configMount := v1.VolumeMount{
				Name:      volume.Name,
				MountPath: mountPath,
				SubPath:   defaultSubPathName,
			}
			etcMount := v1.VolumeMount{
				Name:      volume.Name,
				MountPath: couchbaseVolumeDefaultEtcDir,
				SubPath:   etcSubPathName,
			}
			mounts = append(mounts, configMount)
			mounts = append(mounts, etcMount)
		} else {
			mounts = append(mounts, v1.VolumeMount{Name: volume.Name, MountPath: mountPath})
		}
	}

	// Add volumes to the pod Spec stateful volumes
	container, err := getCouchbaseContainer(pod)
	if err != nil {
		return nil, err
	}

	container.VolumeMounts = append(container.VolumeMounts, mounts...)
	return pvcs, nil
}

// Get all paths to that should be persisted within pod
func getPathsToPersist(mounts *couchbasev2.VolumeMounts) (map[couchbasev2.VolumeMountName]string, error) {
	mountPaths := make(map[couchbasev2.VolumeMountName]string)
	defaultClaim := mounts.DefaultClaim
	dataClaim := mounts.DataClaim
	indexClaim := mounts.IndexClaim
	analyticsClaims := mounts.AnalyticsClaims

	// var to test existence of non default/logs mounts
	hasSecondaryMounts := dataClaim != "" || indexClaim != "" || analyticsClaims != nil

	if logsClaim := mounts.LogsClaim; logsClaim != "" {
		// When logsClaim is specified no other mounts are allowed.
		// Return error if validation didn't prevent this from occurring.
		if defaultClaim != "" || hasSecondaryMounts {
			return mountPaths, fmt.Errorf("other mounts cannot be used in with `logs` mount")
		}
		mountPaths[couchbasev2.LogsVolumeMount] = logsClaim
		return mountPaths, nil
	}
	if defaultClaim != "" {
		mountPaths[couchbasev2.DefaultVolumeMount] = defaultClaim
		if dataClaim != "" {
			mountPaths[couchbasev2.DataVolumeMount] = dataClaim
		}
		if indexClaim != "" {
			mountPaths[couchbasev2.IndexVolumeMount] = indexClaim
		}
		if analyticsClaims != nil {
			for mount, claim := range mounts.GetAnalyticsMountClaims() {
				mountPaths[couchbasev2.VolumeMountName(mount)] = claim
			}
		}
	} else if hasSecondaryMounts {
		// Reutrn error if other mount paths are specified without default volume
		return mountPaths, fmt.Errorf("other mounts cannot be used in without `default` mount")
	}
	return mountPaths, nil
}

func pathForVolumeMountName(id couchbasev2.VolumeMountName) string {
	var path string
	switch id {
	case couchbasev2.DefaultVolumeMount:
		return couchbaseVolumeDefaultConfigDir
	case couchbasev2.DataVolumeMount:
		path = CouchbaseVolumeMountDataDir
	case couchbasev2.IndexVolumeMount:
		path = CouchbaseVolumeMountIndexDir
	case couchbasev2.LogsVolumeMount:
		path = CouchbaseVolumeMountLogsDir
	default:
		if strings.Contains(string(id), string(couchbasev2.AnalyticsVolumeMount)) {
			// path resolves to /mnt/analytics-00 when matching on analytics volume
			path = fmt.Sprintf("/mnt/%s", id)
		}
	}
	return path
}

// Creates custom PVC from the generic spec
func createPersistentVolumeClaim(kubeCli kubernetes.Interface, claim *v1.PersistentVolumeClaim, namespace string, owner metav1.OwnerReference) (*v1.PersistentVolumeClaim, error) {

	// can be mounted read/write mode to exactly 1 host
	addOwnerRefToObject(claim.GetObjectMeta(), owner)
	claim.Spec.AccessModes = []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}
	pvc, err := kubeCli.CoreV1().PersistentVolumeClaims(namespace).Create(claim)
	if err != nil {
		return nil, err
	}
	return pvc, nil
}

func podVolumeSpecForClaim(claimName string) v1.Volume {
	return v1.Volume{
		Name: claimName,
		VolumeSource: v1.VolumeSource{
			PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
				ClaimName: claimName},
		},
	}
}

// Delete pod and any associated persisted volumes
// when removeVolumes is 'true'
func DeleteCouchbasePod(kubeCli kubernetes.Interface, namespace, clusterName, name string, opts *metav1.DeleteOptions, removeVolumes bool) error {

	var errs []string

	if err := DeletePod(kubeCli, namespace, name, opts); err != nil {
		errs = append(errs, err.Error())
	}

	if removeVolumes {
		if err := deletePodVolumes(kubeCli, namespace, clusterName, name); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf(strings.Join(errs, ","))
	}
	return nil
}

// list and delete persistent volumes associated with the member
func deletePodVolumes(kubeCli kubernetes.Interface, namespace, clusterName, memberName string) error {

	pvcList, err := listMemberPVCS(kubeCli, memberName, clusterName, namespace)
	if err != nil {
		return err
	}
	if len(pvcList.Items) > 0 {
		for _, pvc := range pvcList.Items {
			if err := kubeCli.CoreV1().PersistentVolumeClaims(namespace).Delete(pvc.Name, CascadeDeleteOptions(0)); err != nil {
				return err
			}
		}
	}
	return nil
}

// list all PVC's belonging to the member
func listMemberPVCS(kubeCli kubernetes.Interface, memberName, clusterName, namespace string) (*v1.PersistentVolumeClaimList, error) {
	labelSelector := fmt.Sprintf("couchbase_node=%s,couchbase_cluster=%s", memberName, clusterName)
	opts := metav1.ListOptions{
		LabelSelector: labelSelector,
	}
	return listPersistentVolumeClaims(kubeCli, namespace, opts)
}

func listPersistentVolumeClaims(kubeCli kubernetes.Interface, namespace string, opts metav1.ListOptions) (*v1.PersistentVolumeClaimList, error) {
	return kubeCli.CoreV1().PersistentVolumeClaims(namespace).List(opts)
}

// Names of persistent volume claims are combinations of
// Member name, mount type, and mount index.
// ie...: cb-example-0000-default-00, pvc-data-cb-example-0000-01-index
func NameForPersistentVolumeClaim(memberName string, index int, mountName couchbasev2.VolumeMountName) string {
	return fmt.Sprintf("%s-%s-%02d", memberName, mountName, index)
}

// CreateCouchbasePodSpec creates an "idealized" pod specification.  This must be invariant
// across creations e.g. init containers are only needed during the initial creation and not
// required for recovery, therefore should not be done here.  We use this invaiance property
// in order to trigger Couchbase upgrade sequences.  Pods are immutable so we use swap
// rebalances to upgrade not only the container version, but other attributes that are configurable
// in the server class pod policy, e.g. adding PVCs, scheduling constraints etc.
func CreateCouchbasePodSpec(kubeCli kubernetes.Interface, m *couchbaseutil.Member, cluster *couchbasev2.CouchbaseCluster, config couchbasev2.ServerConfig) (*v1.Pod, []*v1.PersistentVolumeClaim, error) {

	// Create the standard Couchbase container image.
	container := couchbaseContainer(cluster.Spec.Image)
	container.ReadinessProbe = couchbaseReadinessProbe()
	if config.Pod != nil {
		container.Resources = config.Pod.Resources
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   m.Name,
			Labels: createCouchbasePodLabels(m.Name, cluster.Name, config),
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				container,
			},
			RestartPolicy:   v1.RestartPolicyNever,
			Hostname:        m.Name,
			Subdomain:       cluster.Name,
			NodeSelector:    map[string]string{},
			SecurityContext: cluster.Spec.SecurityContext,
		},
	}
	applyBaseAnnotations(pod.GetObjectMeta())

	if cluster.Spec.AntiAffinity {
		pod = PodWithAntiAffinity(pod, cluster.Name)
	}

	applyPodPolicy(pod, config.Pod)

	if err := applyPodTlsConfiguration(cluster.Spec, pod); err != nil {
		return nil, nil, err
	}

	if err := SetCouchbaseVersion(pod, cluster.Spec.Image); err != nil {
		return nil, nil, err
	}

	pvcs, err := addPodVolumes(kubeCli, pod, cluster, config)
	if err != nil {
		return nil, nil, err
	}

	// Add the original specification to the annotations, we will use this to upgrade
	// the pods when the specification differs.  We cannot rely on the Pod specification
	// being immutable once committed to Kubernetes as it may be subject to defaulting.
	// We work in exactly the same way as a `kubectl apply command`.
	specJSON, err := json.Marshal(pod.Spec)
	if err != nil {
		return nil, nil, err
	}
	pod.Annotations[constants.PodSpecAnnotation] = string(specJSON)

	return pod, pvcs, nil
}

func createCouchbasePodLabels(memberName, clusterName string, ns couchbasev2.ServerConfig) map[string]string {
	labels := map[string]string{
		constants.LabelApp:      constants.App,
		constants.LabelNode:     memberName,
		constants.LabelNodeConf: ns.Name,
		constants.LabelCluster:  clusterName,
	}

	for _, s := range ns.Services {
		k := "couchbase_service_" + s.String()
		labels[k] = "enabled"
	}

	return labels
}

func CouchbaseContainer(image string) v1.Container {
	return couchbaseContainer(image)
}

func couchbaseContainer(image string) v1.Container {
	c := v1.Container{
		Name:  couchbaseContainerName,
		Image: image,
		Ports: []v1.ContainerPort{
			{
				Name:          adminServicePortName,
				ContainerPort: int32(adminServicePort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          indexServicePortName,
				ContainerPort: int32(indexServicePort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          queryServicePortName,
				ContainerPort: int32(queryServicePort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          searchServicePortName,
				ContainerPort: int32(searchServicePort),
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
				Name:          indexerAdminPortName,
				ContainerPort: int32(indexerAdminPort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          indexerScanPortName,
				ContainerPort: int32(indexerScanPort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          indexerHTTPPortName,
				ContainerPort: int32(indexerHTTPPort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          indexerSTInitPortName,
				ContainerPort: int32(indexerSTInitPort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          indexerSTCatchupPortName,
				ContainerPort: int32(indexerSTCatchupPort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          indexerSTMainPortName,
				ContainerPort: int32(indexerSTMainPort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          analyticsAdminPortName,
				ContainerPort: int32(analyticsAdminPort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          analyticsCCHTTPPortName,
				ContainerPort: int32(analyticsCCHTTPPort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          analyticsCCClusterPortName,
				ContainerPort: int32(analyticsCCClusterPort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          analyticsCCClientPortName,
				ContainerPort: int32(analyticsCCClientPort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          analyticsConsolePortName,
				ContainerPort: int32(analyticsConsolePort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          analyticsClusterPortName,
				ContainerPort: int32(analyticsClusterPort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          analyticsDataPortName,
				ContainerPort: int32(analyticsDataPort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          analyticsResultPortName,
				ContainerPort: int32(analyticsResultPort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          analyticsMessagingPortName,
				ContainerPort: int32(analyticsMessagingPort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          analyticsAuthPortName,
				ContainerPort: int32(analyticsAuthPort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          analyticsReplicationPortName,
				ContainerPort: int32(analyticsReplicationPort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          analyticsMetadataPortName,
				ContainerPort: int32(analyticsMetadataPort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          analyticsMetadataCallbackPortName,
				ContainerPort: int32(analyticsMetadataCallbackPort),
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
				Name:          indexServicePortNameTLS,
				ContainerPort: int32(indexServicePortTLS),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          queryServicePortNameTLS,
				ContainerPort: int32(queryServicePortTLS),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          searchServicePortNameTLS,
				ContainerPort: int32(searchServicePortTLS),
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
		VolumeMounts: []v1.VolumeMount{},
	}

	return c
}

// Init container is same as runtime container except it used
// to copy the etc dir into a persisted volume which will be
// shared with with the Pod's main container
func couchbaseInitContainer(image, claimName string) v1.Container {
	initContainer := couchbaseContainer(image)
	initContainer.Name = fmt.Sprintf("%s-init", couchbaseContainerName)
	initContainer.Args = []string{"cp", "-a", "/opt/couchbase/etc", "/mnt/"}
	initContainer.VolumeMounts = []v1.VolumeMount{
		{Name: claimName,
			MountPath: "/mnt"},
	}
	return initContainer
}

func PodWithAntiAffinity(pod *v1.Pod, clusterName string) *v1.Pod {
	// set pod anti-affinity with the pods that belongs to the same couchbase cluster
	ls := &metav1.LabelSelector{MatchLabels: map[string]string{
		constants.LabelCluster: clusterName,
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

func applyPodPolicy(pod *v1.Pod, policy *couchbasev2.PodPolicy) {
	if policy == nil {
		return
	}

	if len(policy.NodeSelector) != 0 {
		pod.Spec.NodeSelector = policy.NodeSelector
	}
	if len(policy.Tolerations) != 0 {
		pod.Spec.Tolerations = policy.Tolerations
	}
	if policy.AutomountServiceAccountToken != nil {
		pod.Spec.AutomountServiceAccountToken = policy.AutomountServiceAccountToken
	}
	if len(policy.ImagePullSecrets) != 0 {
		pod.Spec.ImagePullSecrets = policy.ImagePullSecrets
	}

	mergeLabels(pod.Labels, policy.Labels)
	mergeLabels(pod.Annotations, policy.Annotations)

	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == couchbaseContainerName {
			pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, policy.Env...)
			pod.Spec.Containers[i].EnvFrom = append(pod.Spec.Containers[i].EnvFrom, policy.EnvFrom...)
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
func applyPodTlsConfiguration(cs couchbasev2.ClusterSpec, pod *v1.Pod) error {
	if cs.Networking.TLS != nil {
		// Static configuration:
		// * Defines a volume which contains the secrets necessary
		//   to explicitly define TLS certificates and keys
		// * Mounts the volume in in the correct location so that API
		//   calls to /node/controller/reloadCertificate succeed
		if cs.Networking.TLS.Static != nil {
			// Ensure the schema is correct
			// TODO: does this make sense not to be a pointer?
			if cs.Networking.TLS.Static.Member == nil {
				return fmt.Errorf("static tls member secret required")
			}

			// Add the TLS secret volume to the pod
			volume := v1.Volume{
				Name: couchbaseTlsVolumeName,
			}
			volume.VolumeSource.Secret = &v1.SecretVolumeSource{
				SecretName: cs.Networking.TLS.Static.Member.ServerSecret,
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

			// Annotate the pod as having TLS enabled
			pod.Annotations[constants.PodTLSAnnotation] = "enabled"
		}
	}
	return nil
}

func couchbaseReadinessProbe() *v1.Probe {
	return &v1.Probe{
		Handler: v1.Handler{
			Exec: &v1.ExecAction{
				Command: []string{"test", "-f", readinessFile},
			},
		},
		InitialDelaySeconds: 10,
		TimeoutSeconds:      5,
		PeriodSeconds:       20,
		FailureThreshold:    1,
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

// Find the PVC belonging to a member that was mounted at the specified path.
// It's not considered an error in the case that PVC cannot be found
func findMemberPVC(kubeCli kubernetes.Interface, memberName, clusterName, namespace, path string) (*v1.PersistentVolumeClaim, error) {
	pvcList, err := listMemberPVCS(kubeCli, memberName, clusterName, namespace)
	if err != nil {
		return nil, err
	}
	for _, pvc := range pvcList.Items {
		if pvcPath, ok := pvc.Annotations[constants.AnnotationVolumeMountPath]; ok {
			if pvcPath == path {
				phase := pvc.Status.Phase
				switch phase {
				case v1.ClaimBound:
					return &pvc, nil
				case v1.ClaimPending:
					return nil, cberrors.ErrVolumeClaimPending{Path: path, Phase: phase}
				case v1.ClaimLost:
					return nil, cberrors.ErrVolumeClaimLost{Path: path, Phase: phase}
				default:
					return nil, cberrors.ErrVolumeClaimUnknownPhase{Path: path, Phase: phase}
				}
			}
		}
	}

	return nil, cberrors.ErrVolumeClaimMissing{Path: path}
}

// Recreate list of members from persistent volumes
func PVCToMemberset(kubeCli kubernetes.Interface, namespace string, clusterName string, secure bool) (couchbaseutil.MemberSet, error) {
	labelSelector := fmt.Sprintf("couchbase_cluster=%s", clusterName)
	opts := metav1.ListOptions{
		LabelSelector: labelSelector,
	}
	ms := couchbaseutil.MemberSet{}
	pvcList, err := listPersistentVolumeClaims(kubeCli, namespace, opts)
	if err != nil {
		return ms, err
	}
	for _, pvc := range pvcList.Items {

		// claim must be bound to a volume
		if pvc.Status.Phase != v1.ClaimBound {
			continue
		}

		// reject log volumes that have been marked as detached
		if _, ok := pvc.Annotations[constants.VolumeDetachedAnnotation]; ok {
			continue
		}

		// require members to have path
		if _, ok := pvc.Annotations[constants.AnnotationVolumeMountPath]; !ok {
			continue
		}

		m := couchbaseutil.Member{
			Namespace:    namespace,
			SecureClient: secure,
		}
		var ok bool
		if m.Name, ok = pvc.Labels[constants.LabelNode]; !ok {
			continue
		}
		if m.ServerConfig, ok = pvc.Annotations[constants.AnnotationVolumeNodeConf]; !ok {
			continue
		}
		if m.Version, ok = pvc.Annotations[constants.CouchbaseVersionAnnotationKey]; !ok {
			continue
		}
		ms.Add(&m)
	}
	return ms, nil
}

// pod is recoverable if it has volume mounts with existing
// persistentVolumeClaims.  The claims must also be bound to
// backing volumes.  Every claim used by the pod must be bound
// to an underlying PersistentVolume
func IsPodRecoverable(kubeCli kubernetes.Interface, config couchbasev2.ServerConfig, podName, clusterName, namespace string) error {
	mounts := config.GetVolumeMounts()
	if mounts == nil || mounts.LogsOnly() {
		return cberrors.ErrNoVolumeMounts{}
	} else {
		// default volume claim is required for recovery
		defaultClaim := mounts.DefaultClaim
		if defaultClaim == "" {
			return fmt.Errorf("no claim defined for default volume")
		}
		// all volume mounts must be healthy
		mountPaths, err := getPathsToPersist(mounts)
		if err != nil {
			return err
		}
		for mountName := range mountPaths {
			mountPath := pathForVolumeMountName(mountName)
			_, err := findMemberPVC(kubeCli, podName, clusterName, namespace, mountPath)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// IsLogPVC returns whether this is a volume containing Couchbase logs.
func IsLogPVC(pvc *v1.PersistentVolumeClaim) (bool, error) {
	path, ok := pvc.Annotations[constants.AnnotationVolumeMountPath]
	if !ok {
		return false, fmt.Errorf("path annotation missing for pvc %s", pvc.Name)
	}
	return path == CouchbaseVolumeMountLogsDir, nil
}

// exec shells onto a pod and runs a command.
func exec(client kubernetes.Interface, pod *v1.Pod, command []string) error {
	config, err := InClusterConfig()
	if err != nil {
		return err
	}

	// Generate the REST request
	req := client.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(pod.Namespace).
		Name(pod.Name).
		SubResource("exec")
	req.VersionedParams(&v1.PodExecOptions{
		Container: couchbaseContainerName,
		Command:   command,
		Stdout:    true,
	}, scheme.ParameterCodec)

	// Create an executor running over HTTP2
	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("remote command on %s failed: %v", pod.Name, err)
	}

	// Finally run the command
	stdout := &bytes.Buffer{}
	if err := exec.Stream(remotecommand.StreamOptions{Stdout: stdout}); err != nil {
		return fmt.Errorf("remote command on %s failed: %v", pod.Name, err)
	}

	return nil
}

// FlagPodReady adds a file on the pod that flags the pod is ready and can be safely
// killed by Kubernetes.
func FlagPodReady(client kubernetes.Interface, namespace, name string) error {
	pod, err := GetPod(client, namespace, name)
	if err != nil {
		return err
	}
	if err := exec(client, pod, []string{"touch", readinessFile}); err != nil {
		return err
	}
	return nil
}
