package k8sutil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	cberrors "github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/diff"
	"github.com/couchbase/couchbase-operator/pkg/util/scheduler"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	couchbaseTLSVolumeMountDir      = "/opt/couchbase/var/lib/couchbase/inbox"
	couchbaseVolumeDefaultConfigDir = "/opt/couchbase/var/lib/couchbase"
	CouchbaseVolumeMountLogsDir     = "/opt/couchbase/var/lib/couchbase/logs"
	couchbaseVolumeDefaultEtcDir    = "/opt/couchbase/etc"
	CouchbaseVolumeMountDataDir     = "/mnt/data"
	CouchbaseVolumeMountIndexDir    = "/mnt/index"
	defaultSubPathName              = "default"
	etcSubPathName                  = "etc"
	readinessFile                   = "/tmp/ready"
	prometheusPort                  = 9091
	serverSecretMountPath           = "/var/run/secrets/couchbase.com/couchbase-server-tls"
	operatorSecretMountPath         = "/var/run/secrets/couchbase.com/couchbase-operator-tls"
	metricsTokenMountPath           = "/var/run/secrets/couchbase.com/metrics-token"
)

// Creates pods with any PersistentVolumeClaims (PVCs)
// necessary for the Pod prior to creating the Pod.
func CreateCouchbasePod(client *client.Client, scheduler scheduler.Scheduler, cluster *couchbasev2.CouchbaseCluster, m *couchbaseutil.Member, config couchbasev2.ServerConfig, ctx context.Context) (*v1.Pod, error) {
	// First work out what persistent volumes we need.
	pvcState, err := GetPodVolumes(client, m.Name, cluster, config)
	if err != nil {
		return nil, err
	}

	// Next work out scheduling.  If an existing PVCs have been explicitly
	// scheduled to a specific server group we must reuse that.  If this isn't
	// set then we use the scheduler to balance the load across AZs.
	serverGroup := ""
	if pvcState != nil {
		serverGroup = pvcState.availabilityZone
	}
	serverGroup, err = scheduler.Create(config.Name, m.Name, serverGroup)
	if err != nil {
		return nil, err
	}

	// Create the actual pod specification.
	pod, err := CreateCouchbasePodSpec(client, m, cluster, config, serverGroup, pvcState)
	if err != nil {
		return nil, err
	}

	// Create PVCs if required.
	if pvcState != nil {
		for _, pvc := range pvcState.create {
			// If the pod was explicitly scheduled then we need to keep note of the
			// availability zone in the persistent volume claim so it can be copied
			// back to a reconstructed pod.  For example A:0,3 B:1 C:2.  Killing pods
			// 0 and 1 in A and B would result in the new pod for 0 being scheduled in
			// B to keep things in balance, however its volumes are still in A.
			if serverGroup != "" {
				pvc.Annotations[constants.ServerGroupLabel] = serverGroup
			}

			if _, err = createPersistentVolumeClaim(client, pvc, cluster.Namespace, cluster.AsOwner()); err != nil {
				return nil, err
			}

			// If we are creating a default mount, then also create an init container to
			// copy Couchbase's etc directory onto the PVC.
			if pvc.Annotations[constants.AnnotationVolumeMountPath] == couchbaseVolumeDefaultConfigDir {
				initContainer := couchbaseInitContainer(cluster.Spec.Image, pvc.Name, config)
				pod.Spec.InitContainers = append(pod.Spec.InitContainers, initContainer)
			}
		}
	}

	// Add ownership information only if we are going to create the resource.
	addOwnerRefToObject(pod, cluster.AsOwner())
	return CreatePod(client, cluster.Namespace, pod)
}

// persistentVolumeClaimState contains all the information we could ever need about
// persistent volumes relating to a pod.
type persistentVolumeClaimState struct {
	// pvcs is an ordered list of all PVCs.
	pvcs []*v1.PersistentVolumeClaim

	// create is a list of PVCs that needs creating.
	create []*v1.PersistentVolumeClaim

	// update is a list of PVCs that need updating.
	update []*v1.PersistentVolumeClaim

	// volumes is an ordered list of volumes to attach to the pod.
	volumes []v1.Volume

	// mounts is an ordered list of mounts to attach to the container.
	volumeMounts []v1.VolumeMount

	// availabilityZone is where any existing PVCs reside when using server groups.
	availabilityZone string

	// diff records any changes to the specification.
	diff string
}

// NeedsUpdate indicates whether any PVCs need updating.
func (p *persistentVolumeClaimState) NeedsUpdate() bool {
	return len(p.update) != 0 || len(p.create) != 0
}

// Diff returns a diff of changes when PVCs are created or updated.
func (p *persistentVolumeClaimState) Diff() string {
	return p.diff
}

// Add a persistent volume to the pod spec for each volumeMount.
// The volumes are first created via persistentVolumeClaims
// Volumes that already exist are reused
func GetPodVolumes(client *client.Client, memberName string, cluster *couchbasev2.CouchbaseCluster, config couchbasev2.ServerConfig) (*persistentVolumeClaimState, error) {
	// No mounts are required, do nothing
	if config.GetVolumeMounts() == nil {
		return nil, nil
	}

	state := &persistentVolumeClaimState{}

	mountPaths, err := getPathsToPersist(config.VolumeMounts)
	if err != nil {
		return nil, err
	}

	version, err := CouchbaseVersion(cluster.Spec.Image)
	if err != nil {
		return nil, err
	}

	// Keep track of volumes generated from the same claim
	claimUsageCnt := map[string]int{}

	// Order the mounts so that the pod spec is deterministic
	mountNames := []string{}
	for name := range mountPaths {
		mountNames = append(mountNames, string(name))
	}
	sort.Strings(mountNames)

	for _, name := range mountNames {
		mountName := couchbasev2.VolumeMountName(name)
		claimName := mountPaths[mountName]

		// Update PVC name
		claimUsageCnt[claimName]++

		// Find volumes that already exist for this mount path
		// to allow pod recovery. Otherwise, create a new PVC
		mountPath := pathForVolumeMountName(mountName)
		pvc, _ := findMemberPVC(client, memberName, mountPath)

		// every volume mount must have associated claim template
		// within the spec before we can add it to the pod
		required := cluster.Spec.GetVolumeClaimTemplate(claimName)
		if required == nil {
			return nil, fmt.Errorf("claim (%s) does not map to any claimTemplates", claimName)
		}

		// Label and Annotate so that volumes
		// can be easily targeted when recovering pods
		required.Labels = map[string]string{
			constants.LabelApp:        constants.App,
			constants.LabelNode:       memberName,
			constants.LabelCluster:    cluster.Name,
			constants.LabelVolumeName: claimName,
		}
		required.SetAnnotations(map[string]string{
			constants.AnnotationVolumeMountPath:     mountPath,
			constants.AnnotationVolumeNodeConf:      config.Name,
			constants.CouchbaseVersionAnnotationKey: version,
		})
		ApplyBaseAnnotations(required)
		if gid := cluster.Spec.GetFSGroup(); gid != nil {
			required.Annotations["pv.beta.kubernetes.io/gid"] = fmt.Sprintf("%d", *gid)
		}
		required.Name = NameForPersistentVolumeClaim(memberName, claimUsageCnt[claimName], mountName)

		specJSON, err := json.Marshal(required.Spec)
		if err != nil {
			return nil, err
		}
		required.Annotations[constants.PVCSpecAnnotation] = string(specJSON)

		// If a PVC does exist and differs, mark it for update.
		if pvc != nil {
			existingSpec := v1.PersistentVolumeClaimSpec{}
			if annotation, ok := pvc.Annotations[constants.PVCSpecAnnotation]; ok {
				if err := json.Unmarshal([]byte(annotation), &existingSpec); err != nil {
					return nil, err
				}
			}
			if !reflect.DeepEqual(existingSpec, required.Spec) {
				state.update = append(state.update, pvc)
				d, err := diff.Diff(existingSpec, required.Spec)
				if err == nil {
					state.diff += d
				}
			}
		}

		// If a PVC doesn't exist mark it for creation.
		if pvc == nil {
			pvc = required
			state.create = append(state.create, pvc)
			d, err := diff.Diff(nil, required.Spec)
			if err == nil {
				state.diff += d
			}
		}

		state.pvcs = append(state.pvcs, pvc)

		// Set any scheduling hints
		if group, ok := pvc.Annotations[constants.ServerGroupLabel]; ok {
			state.availabilityZone = group
		}

		// Volumes will be added to Pod spec
		volume := podVolumeSpecForClaim(pvc.Name)
		state.volumes = append(state.volumes, volume)

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
			state.volumeMounts = append(state.volumeMounts, configMount)
			state.volumeMounts = append(state.volumeMounts, etcMount)
		} else {
			state.volumeMounts = append(state.volumeMounts, v1.VolumeMount{Name: volume.Name, MountPath: mountPath})
		}
	}

	return state, nil
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
func createPersistentVolumeClaim(client *client.Client, claim *v1.PersistentVolumeClaim, namespace string, owner metav1.OwnerReference) (*v1.PersistentVolumeClaim, error) {

	// can be mounted read/write mode to exactly 1 host
	addOwnerRefToObject(claim, owner)
	claim.Spec.AccessModes = []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}
	pvc, err := client.KubeClient.CoreV1().PersistentVolumeClaims(namespace).Create(claim)
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
func DeleteCouchbasePod(client *client.Client, namespace, name string, opts *metav1.DeleteOptions, removeVolumes bool) error {

	var errs []string

	if err := DeletePod(client, namespace, name, opts); err != nil {
		errs = append(errs, err.Error())
	}

	if removeVolumes {
		if err := deletePodVolumes(client, name); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf(strings.Join(errs, ","))
	}
	return nil
}

// list and delete persistent volumes associated with the member
func deletePodVolumes(client *client.Client, memberName string) error {
	for _, pvc := range listMemberPVCS(client, memberName) {
		if err := client.KubeClient.CoreV1().PersistentVolumeClaims(pvc.Namespace).Delete(pvc.Name, CascadeDeleteOptions(0)); err != nil {
			return err
		}
	}
	return nil
}

// list all PVC's belonging to the member
func listMemberPVCS(client *client.Client, memberName string) (pvcs []*v1.PersistentVolumeClaim) {
	for _, pvc := range client.PersistentVolumeClaims.List() {
		if name, ok := pvc.Labels[constants.LabelNode]; ok && name == memberName {
			pvcs = append(pvcs, pvc)
		}
	}
	return
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
func CreateCouchbasePodSpec(client *client.Client, m *couchbaseutil.Member, cluster *couchbasev2.CouchbaseCluster, config couchbasev2.ServerConfig, serverGroup string, pvcState *persistentVolumeClaimState) (*v1.Pod, error) {
	// Create the standard Couchbase container image.
	container := couchbaseContainer(cluster.Spec.Image, &config)
	container.ReadinessProbe = couchbaseReadinessProbe()
	if pvcState != nil {
		container.VolumeMounts = pvcState.volumeMounts
	}

	// Use the user provided Pod template if provided.
	pod := &v1.Pod{}
	if config.Pod != nil {
		pod.ObjectMeta = config.Pod.ObjectMeta
		pod.Spec = config.Pod.Spec
	}

	// For metadata, override the name and merge the labels and annotations.
	pod.Name = m.Name
	pod.Labels = mergeLabels(pod.Labels, createCouchbasePodLabels(m.Name, cluster.Name, config))
	ApplyBaseAnnotations(pod)

	// Populate the main specification, overriding whatever the template specified.
	pod.Spec.Containers = []v1.Container{
		container,
	}
	pod.Spec.RestartPolicy = v1.RestartPolicyNever
	pod.Spec.Hostname = m.Name
	pod.Spec.Subdomain = cluster.Name
	pod.Spec.SecurityContext = cluster.Spec.SecurityContext

	// If anti-affinity is set then ensure no two pods from the same cluster
	// run on the same hosts.
	if cluster.Spec.AntiAffinity {
		pod.Spec.Affinity = &v1.Affinity{
			PodAntiAffinity: &v1.PodAntiAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								constants.LabelCluster: cluster.Name,
							},
						},
						TopologyKey: "kubernetes.io/hostname",
					},
				},
			},
		}
	}

	// If persistent volumes are specified then add them.
	if pvcState != nil {
		pod.Spec.Volumes = append(pod.Spec.Volumes, pvcState.volumes...)
	}

	// If TLS is specified then add the certificate volume.
	if err := applyPodTLSConfiguration(cluster.Spec, pod); err != nil {
		return nil, err
	}

	// If monitoring is enabled add the necessary side cars.
	if cluster.Spec.Monitoring != nil && cluster.Spec.Monitoring.Prometheus != nil {
		if cluster.Spec.Monitoring.Prometheus.Enabled {
			metricsContainer := createMetricsContainer(cluster.Spec)
			applyMetricsPodSecurity(cluster.Spec, &metricsContainer, pod)
			if err := applyMetricsPodTLS(cluster.Spec, &metricsContainer, pod); err != nil {
				return nil, err
			}
			pod.Spec.Containers = append(pod.Spec.Containers, metricsContainer)
		}
	}

	// Set the Couchbase version metadata.
	if err := SetCouchbaseVersion(pod, cluster.Spec.Image); err != nil {
		return nil, err
	}

	// Add or override any scheduling operation.
	if serverGroup != "" {
		if pod.Spec.NodeSelector == nil {
			pod.Spec.NodeSelector = map[string]string{}
		}
		pod.Spec.NodeSelector[constants.ServerGroupLabel] = serverGroup
	}

	// Add the original specification to the annotations, we will use this to upgrade
	// the pods when the specification differs.  We cannot rely on the Pod specification
	// being immutable once committed to Kubernetes as it may be subject to defaulting.
	// We work in exactly the same way as a `kubectl apply command`.
	specJSON, err := json.Marshal(pod.Spec)
	if err != nil {
		return nil, err
	}
	pod.Annotations[constants.PodSpecAnnotation] = string(specJSON)

	return pod, nil
}

func applyMetricsPodTLS(cs couchbasev2.ClusterSpec, container *v1.Container, pod *v1.Pod) error {
	if cs.Networking.TLS != nil {
		// Static configuration:
		// * Defines a (new) volume which contains the secrets necessary
		//   to explicitly define TLS certificates and keys
		// * K8S won't allow us to re-use the previous volume for Couchbase Pod TLS
		// * Mounts the volume in in the correct location so that API
		//   calls to /node/controller/reloadCertificate succeed
		if cs.Networking.TLS.Static != nil {
			// Add the TLS server secret volume to the metrics pod
			volume := v1.Volume{
				Name: constants.CouchbaseTLSVolumeName + "-metrics",
				VolumeSource: v1.VolumeSource{
					Secret: &v1.SecretVolumeSource{
						SecretName: cs.Networking.TLS.Static.ServerSecret,
					},
				},
			}
			pod.Spec.Volumes = append(pod.Spec.Volumes, volume)

			// Mount the secret volume
			volumeMount := v1.VolumeMount{
				Name:      constants.CouchbaseTLSVolumeName + "-metrics",
				ReadOnly:  true,
				MountPath: serverSecretMountPath,
			}
			container.VolumeMounts = append(container.VolumeMounts, volumeMount)

			// add the TLS server flags to the couchbase-exporter binary
			container.Command = append(container.Command,
				"--cert", serverSecretMountPath+"/chain.pem",
				"--key", serverSecretMountPath+"/pkey.key")

			// Add the TLS server secret volume to the metrics pod
			volume = v1.Volume{
				Name: "couchbase-operator-tls-metrics",
				VolumeSource: v1.VolumeSource{
					Secret: &v1.SecretVolumeSource{
						SecretName: cs.Networking.TLS.Static.OperatorSecret,
					},
				},
			}
			pod.Spec.Volumes = append(pod.Spec.Volumes, volume)

			// Mount the secret volume
			volumeMount = v1.VolumeMount{
				Name:      "couchbase-operator-tls-metrics",
				ReadOnly:  true,
				MountPath: operatorSecretMountPath,
			}
			container.VolumeMounts = append(container.VolumeMounts, volumeMount)

			// add the TLS server flags to the couchbase-exporter binary
			container.Command = append(container.Command,
				"--ca", operatorSecretMountPath+"/ca.crt")

		}

		if cs.Networking.TLS.ClientCertificatePolicy != nil {
			// add the TLS server flags to the couchbase-exporter binary
			container.Command = append(container.Command,
				"--client-cert", operatorSecretMountPath+"/couchbase-operator.crt",
				"--client-key", operatorSecretMountPath+"/couchbase-operator.key")
		}
	}

	return nil
}

func applyMetricsPodSecurity(cs couchbasev2.ClusterSpec, container *v1.Container, pod *v1.Pod) {
	// if bearer token is enabled for authorization, mount token as volume
	if cs.Monitoring.Prometheus.AuthorizationSecret != nil {
		volume := v1.Volume{
			Name: "metrics-token",
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: *cs.Monitoring.Prometheus.AuthorizationSecret,
				},
			},
		}
		pod.Spec.Volumes = append(pod.Spec.Volumes, volume)

		// Mount the secret volume
		volumeMount := v1.VolumeMount{
			Name:      "metrics-token",
			ReadOnly:  true,
			MountPath: metricsTokenMountPath,
		}
		container.VolumeMounts = append(container.VolumeMounts, volumeMount)

		container.Command = append(container.Command, "--token", metricsTokenMountPath+"/token")
	}
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
	return couchbaseContainer(image, nil)
}

func couchbaseContainer(image string, config *couchbasev2.ServerConfig) v1.Container {
	c := v1.Container{
		Name:  constants.CouchbaseContainerName,
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
	}

	if config != nil {
		c.Env = config.Env
		c.EnvFrom = config.EnvFrom
		c.Resources = config.Resources
	}

	return c
}

// Init container is same as runtime container except it used
// to copy the etc dir into a persisted volume which will be
// shared with with the Pod's main container
func couchbaseInitContainer(image, claimName string, config couchbasev2.ServerConfig) v1.Container {
	initContainer := couchbaseContainer(image, &config)
	initContainer.Name = fmt.Sprintf("%s-init", constants.CouchbaseContainerName)
	initContainer.Args = []string{"cp", "-a", "/opt/couchbase/etc", "/mnt/"}
	initContainer.VolumeMounts = []v1.VolumeMount{
		{Name: claimName,
			MountPath: "/mnt"},
	}
	return initContainer
}

func createMetricsContainer(cs couchbasev2.ClusterSpec) v1.Container {
	var resources v1.ResourceRequirements
	if cs.Monitoring.Prometheus.Resources != nil {
		resources = *cs.Monitoring.Prometheus.Resources
	}

	return v1.Container{
		Name:  "metrics",
		Image: cs.Monitoring.Prometheus.Image,
		Env: []v1.EnvVar{
			{
				Name: "COUCHBASE_OPERATOR_USER",
				ValueFrom: &v1.EnvVarSource{
					SecretKeyRef: &v1.SecretKeySelector{
						LocalObjectReference: v1.LocalObjectReference{
							Name: cs.Security.AdminSecret,
						},
						Key: "username",
					},
				},
			},
			{
				Name: "COUCHBASE_OPERATOR_PASS",
				ValueFrom: &v1.EnvVarSource{
					SecretKeyRef: &v1.SecretKeySelector{
						LocalObjectReference: v1.LocalObjectReference{
							Name: cs.Security.AdminSecret,
						},
						Key: "password",
					},
				},
			},
		},
		Command: []string{"couchbase-exporter"},
		Ports: []v1.ContainerPort{
			{
				Name:          "prometheus",
				ContainerPort: int32(prometheusPort),
				Protocol:      v1.ProtocolTCP,
			},
		},
		ReadinessProbe: &v1.Probe{
			Handler: v1.Handler{
				TCPSocket: &v1.TCPSocketAction{
					Port: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: prometheusPort,
					},
				},
			},
			InitialDelaySeconds: 10,
			TimeoutSeconds:      5,
			PeriodSeconds:       10,
			FailureThreshold:    3,
		},
		Resources: resources,
	}
}

// Given a pod, return a pointer to the couchbase container
func getCouchbaseContainer(pod *v1.Pod) (*v1.Container, error) {
	for index := range pod.Spec.Containers {
		if pod.Spec.Containers[index].Name == constants.CouchbaseContainerName {
			return &pod.Spec.Containers[index], nil
		}
	}
	return nil, fmt.Errorf("unable to locate couchbase container")
}

// Adds any necessary pod prerequisites before enabling TLS
func applyPodTLSConfiguration(cs couchbasev2.ClusterSpec, pod *v1.Pod) error {
	if cs.Networking.TLS != nil {
		// Static configuration:
		// * Defines a volume which contains the secrets necessary
		//   to explicitly define TLS certificates and keys
		// * Mounts the volume in in the correct location so that API
		//   calls to /node/controller/reloadCertificate succeed
		if cs.Networking.TLS.Static != nil {
			// Add the TLS secret volume to the pod
			volume := v1.Volume{
				Name: constants.CouchbaseTLSVolumeName,
			}
			volume.VolumeSource.Secret = &v1.SecretVolumeSource{
				SecretName: cs.Networking.TLS.Static.ServerSecret,
			}
			pod.Spec.Volumes = append(pod.Spec.Volumes, volume)

			// Mount the secret volume in Couchbase's inbox
			volumeMount := v1.VolumeMount{
				Name:      constants.CouchbaseTLSVolumeName,
				ReadOnly:  true,
				MountPath: couchbaseTLSVolumeMountDir,
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
func findMemberPVC(client *client.Client, memberName, path string) (*v1.PersistentVolumeClaim, error) {
	for _, pvc := range listMemberPVCS(client, memberName) {
		if pvcPath, ok := pvc.Annotations[constants.AnnotationVolumeMountPath]; ok {
			if pvcPath == path {
				phase := pvc.Status.Phase
				switch phase {
				case v1.ClaimBound:
					return pvc, nil
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
func PVCToMemberset(client *client.Client, namespace string, secure bool) (couchbaseutil.MemberSet, error) {
	ms := couchbaseutil.MemberSet{}
	for _, pvc := range client.PersistentVolumeClaims.List() {

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
func IsPodRecoverable(client *client.Client, config couchbasev2.ServerConfig, podName string) error {
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
			_, err := findMemberPVC(client, podName, mountPath)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// IsLogPVC returns whether this is a volume containing Couchbase logs.
func IsLogPVC(pvc *v1.PersistentVolumeClaim) bool {
	path, ok := pvc.Annotations[constants.AnnotationVolumeMountPath]
	if !ok {
		return false
	}
	return path == CouchbaseVolumeMountLogsDir
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
		Container: constants.CouchbaseContainerName,
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
func FlagPodReady(client *client.Client, name string) error {
	pod, found := client.Pods.Get(name)
	if !found {
		return fmt.Errorf("pod %s not found", name)
	}
	if err := exec(client.KubeClient, pod, []string{"touch", readinessFile}); err != nil {
		return err
	}
	return nil
}
