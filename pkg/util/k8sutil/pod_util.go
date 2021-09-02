package k8sutil

import (
	"context"
	"encoding/json"
	goerrors "errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/diff"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/pkg/util/scheduler"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	couchbaseTLSVolumeMountDir                = "/opt/couchbase/var/lib/couchbase/inbox"
	couchbaseVolumeDefaultConfigDir           = "/opt/couchbase/var/lib/couchbase"
	CouchbaseVolumeMountLogsDir               = "/opt/couchbase/var/lib/couchbase/logs"
	couchbaseVolumeDefaultEtcDir              = "/opt/couchbase/etc"
	CouchbaseVolumeMountDataDir               = "/mnt/data"
	CouchbaseVolumeMountIndexDir              = "/mnt/index"
	defaultSubPathName                        = "default"
	etcSubPathName                            = "etc"
	prometheusPort                            = 9091
	prometheusPath                            = "/metrics"
	serverSecretMountPath                     = "/var/run/secrets/couchbase.com/couchbase-server-tls"
	operatorSecretMountPath                   = "/var/run/secrets/couchbase.com/couchbase-operator-tls"
	metricsTokenMountPath                     = "/var/run/secrets/couchbase.com/metrics-token"
	MetricsContainerName                      = "metrics"
	podReadinessCondition                     = v1.PodConditionType("pod.couchbase.com/readiness")
	CouchbaseLogSidecarContainerName          = "logging"
	CouchbaseAuditCleanupSidecarContainerName = "audit-cleanup"
	loggingSidecarMetadataMountDir            = "/etc/podinfo"
	loggingSidecarMetadataMountName           = "podinfo"
	loggingPort                               = 2020
	LoggingConfigurationFile                  = "fluent-bit.conf"
)

// Creates pods with any PersistentVolumeClaims (PVCs)
// necessary for the Pod prior to creating the Pod.
func CreateCouchbasePod(ctx context.Context, client *client.Client, scheduler scheduler.Scheduler, cluster *couchbasev2.CouchbaseCluster, m couchbaseutil.Member, config couchbasev2.ServerConfig) (*v1.Pod, error) {
	// First work out what persistent volumes we need.
	pvcState, err := GetPodVolumes(client, m, cluster, config)
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

	serverGroup, err = scheduler.Create(config.Name, m.Name(), serverGroup)
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
		}

		// If we are creating a default mount, then also create an init container to
		// copy Couchbase's etc directory onto the PVC.  Do this always to avoid surprises
		// the init command is idempotent.
		for _, pvc := range pvcState.pvcs {
			if pvc.Annotations[constants.AnnotationVolumeMountPath] == couchbaseVolumeDefaultConfigDir {
				initContainer := couchbaseInitContainer(cluster, pvc.Name, config)
				pod.Spec.InitContainers = append(pod.Spec.InitContainers, initContainer)
			}
		}
	}

	// Add ownership information only if we are going to create the resource.
	addOwnerRefToObject(pod, cluster.AsOwner())

	return CreatePod(client, cluster.Namespace, pod)
}

// PersistentVolumeClaimState contains all the information we could ever need about
// persistent volumes relating to a pod.
type PersistentVolumeClaimState struct {
	// pvcs is an ordered list of all PVCs.
	pvcs []*v1.PersistentVolumeClaim

	// create is a list of PVCs that needs creating.
	create []*v1.PersistentVolumeClaim

	// update is a list of PVCs that need updating.
	update []*v1.PersistentVolumeClaim

	// PVCs that are currently expanding.
	expanding []*v1.PersistentVolumeClaim

	// PVCs that are currently in failed resize state.
	resizeFailed []*v1.PersistentVolumeClaim

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
// A PVC is an update candidate if its requested spec
// differs from currently deployed spec.
func (p *PersistentVolumeClaimState) NeedsUpdate() bool {
	return len(p.update) != 0 || len(p.create) != 0 || len(p.expanding) != 0 || len(p.resizeFailed) != 0
}

// IsUpdated indicates whether specific PVC spec has been updated.
func (p *PersistentVolumeClaimState) IsUpdated(name string) bool {
	return p.lookup(name, p.update) != nil
}

// IsExpanding indicates whether PVC is expanding.
func (p *PersistentVolumeClaimState) IsExpanding(name string) bool {
	return p.lookup(name, p.expanding) != nil
}

// IsResizeFailed indicates whether PVC failed to resize.
func (p *PersistentVolumeClaimState) IsResizeFailed(name string) bool {
	return p.lookup(name, p.resizeFailed) != nil
}

// Update fetches updated version of PVC and applies change.
func (p *PersistentVolumeClaimState) Update(client *client.Client, name string) (*v1.PersistentVolumeClaim, error) {
	if claim := p.lookup(name, p.update); claim != nil {
		return updatePersistentVolumeClaim(client, claim)
	}

	return nil, fmt.Errorf("%w: refusing to update claim (%s) since it does not exist", errors.NewStackTracedError(errors.ErrResourceAttributeRequired), name)
}

func (p *PersistentVolumeClaimState) lookup(name string, pvcs []*v1.PersistentVolumeClaim) *v1.PersistentVolumeClaim {
	for _, pvc := range pvcs {
		if pvc.Name == name {
			return pvc
		}
	}

	return nil
}

// Diff returns a diff of changes when PVCs are created or updated.
func (p *PersistentVolumeClaimState) Diff() string {
	return p.diff
}

// List returns list of PersistentVolumeClaims.
func (p *PersistentVolumeClaimState) List() []*v1.PersistentVolumeClaim {
	return p.pvcs
}

// addVolume takes a requested volume for a pod and interrogates Kubernetes
// in order to work out how to process it e.g. does it need to be created,
// updated, resized etc.
func (p *PersistentVolumeClaimState) addVolume(client *client.Client, required *v1.PersistentVolumeClaim, member couchbaseutil.Member, mountMapping volumeMount) error {
	// BUG: this returns an error we ignore, the PVC may actually exist but
	// be in a bad state.
	pvc, _ := findMemberPVC(client, member.Name(), mountMapping.mountPath)

	// If a PVC doesn't exist mark it for creation.
	if pvc == nil {
		p.pvcs = append(p.pvcs, required)
		p.create = append(p.create, required)

		d, err := diff.Diff(nil, required.Spec)
		if err == nil {
			p.diff += d
		}

		return nil
	}

	p.pvcs = append(p.pvcs, pvc)

	// Set any scheduling hints.
	if group, ok := pvc.Annotations[constants.ServerGroupLabel]; ok {
		p.availabilityZone = group
	}

	existingSpec := v1.PersistentVolumeClaimSpec{}

	if annotation, ok := pvc.Annotations[constants.PVCSpecAnnotation]; ok {
		if err := json.Unmarshal([]byte(annotation), &existingSpec); err != nil {
			return errors.NewStackTracedError(err)
		}
	}

	// Determine if volume is in one of the following states.
	// update: due to mis-match between requested and existing specs.
	// expanding: due to mis-match between requested and existing status.
	// resizeFailed: volume is reporting resize failure events.
	if !reflect.DeepEqual(existingSpec, required.Spec) {
		// Applying required attributes to list of update PVC's to allow in place updates.
		// BUG: what if I change my storage class???
		updatedClaim := pvc.DeepCopy()
		updatedClaim.Spec.Resources = required.Spec.Resources
		updatedClaim.Annotations = required.Annotations
		p.update = append(p.update, updatedClaim)

		d, err := diff.Diff(existingSpec, required.Spec)
		if err == nil {
			p.diff += d
		}

		return nil
	}

	// Even when the annotations of existingSpec and required spec are the same,
	// it is possible that the requested storage capacity is not yet applied either
	// due to expansion being in progress, or user manually changing pvc request.
	actualSize := pvc.Status.Capacity[v1.ResourceStorage]
	requestedSize := pvc.Spec.Resources.Requests[v1.ResourceStorage]

	if actualSize.Equal(requestedSize) {
		return nil
	}

	if err := checkVolumeResizeFailure(client, pvc); err != nil {
		if !goerrors.Is(err, errors.ErrVolumeResizeError) {
			return err
		}

		p.resizeFailed = append(p.resizeFailed, pvc)

		return nil
	}

	// failure event isn't reported so volume is still expanding
	p.expanding = append(p.expanding, required)

	return nil
}

// addVolumeMounts accepts a set of volume mappings for a pod and uses this
// to generate the required set of volume mounts to apply to the containers.
func (p *PersistentVolumeClaimState) addVolumeMounts(mountMappings volumeMountList) {
	for _, mountMapping := range mountMappings {
		p.volumes = append(p.volumes, podVolumeSpecForClaim(mountMapping.persistentVolumeClaimName))

		// Mount point for Pod Container spec to reference volume by name.
		if mountMapping.name == defaultVolumeMount {
			// Default mount consists of 2 mounts for default(config) and etc data
			configMount := v1.VolumeMount{
				Name:      mountMapping.persistentVolumeClaimName,
				MountPath: mountMapping.mountPath,
				SubPath:   defaultSubPathName,
			}
			etcMount := v1.VolumeMount{
				Name:      mountMapping.persistentVolumeClaimName,
				MountPath: couchbaseVolumeDefaultEtcDir,
				SubPath:   etcSubPathName,
			}

			p.volumeMounts = append(p.volumeMounts, configMount)
			p.volumeMounts = append(p.volumeMounts, etcMount)

			continue
		}

		p.volumeMounts = append(p.volumeMounts, v1.VolumeMount{
			Name:      mountMapping.persistentVolumeClaimName,
			MountPath: mountMapping.mountPath,
		})
	}
}

// generatePVC consumes the member, its server class configuration and generates the required
// PVC for a specific mount mapping for that member.
func generatePVC(cluster *couchbasev2.CouchbaseCluster, member couchbaseutil.Member, mount volumeMount, config couchbasev2.ServerConfig) (*v1.PersistentVolumeClaim, error) {
	version, err := CouchbaseVersion(cluster.Spec.CouchbaseImage())
	if err != nil {
		return nil, err
	}

	// every volume mount must have associated claim template
	// within the spec before we can add it to the pod
	pvc := cluster.Spec.GetVolumeClaimTemplate(mount.persistentVolumeClaimTemplateName)
	if pvc == nil {
		return nil, fmt.Errorf("%w: claim (%s) does not map to any claimTemplates", errors.NewStackTracedError(errors.ErrResourceAttributeRequired), mount.persistentVolumeClaimTemplateName)
	}

	labels := map[string]string{
		constants.LabelApp:        constants.App,
		constants.LabelNode:       member.Name(),
		constants.LabelCluster:    cluster.Name,
		constants.LabelVolumeName: mount.persistentVolumeClaimTemplateName,
	}

	annotations := map[string]string{
		constants.AnnotationVolumeMountPath:     mount.mountPath,
		constants.AnnotationVolumeNodeConf:      config.Name,
		constants.CouchbaseVersionAnnotationKey: version,
	}

	// Merge our labels/annotations on top of any user defined ones.  We take
	// precedence.
	pvc.Labels = mergeLabels(pvc.Labels, labels)
	pvc.Annotations = mergeLabels(pvc.Annotations, annotations)

	ApplyBaseAnnotations(pvc)

	if gid := cluster.Spec.GetFSGroup(); gid != nil {
		pvc.Annotations["pv.beta.kubernetes.io/gid"] = fmt.Sprintf("%d", *gid)
	}

	pvc.Name = mount.persistentVolumeClaimName

	specJSON, err := json.Marshal(pvc.Spec)
	if err != nil {
		return nil, errors.NewStackTracedError(err)
	}

	pvc.Annotations[constants.PVCSpecAnnotation] = string(specJSON)

	return pvc, nil
}

// Add a persistent volume to the pod spec for each volumeMount.
// The volumes are first created via persistentVolumeClaims
// Volumes that already exist are reused.
func GetPodVolumes(client *client.Client, member couchbaseutil.Member, cluster *couchbasev2.CouchbaseCluster, config couchbasev2.ServerConfig) (*PersistentVolumeClaimState, error) {
	// No mounts are required, do nothing
	if config.GetVolumeMounts() == nil {
		return nil, nil
	}

	state := &PersistentVolumeClaimState{}

	mountMappings, err := getPathsToPersist(member, config.VolumeMounts)
	if err != nil {
		return nil, err
	}

	for _, mountMapping := range mountMappings {
		required, err := generatePVC(cluster, member, mountMapping, config)
		if err != nil {
			return nil, err
		}

		if err := state.addVolume(client, required, member, mountMapping); err != nil {
			return nil, err
		}
	}

	state.addVolumeMounts(mountMappings)

	return state, nil
}

// volumeMountName is our internal, short name for a volume mount.
type volumeMountName string

const (
	defaultVolumeMount volumeMountName = "default"
	dataVolumeMount    volumeMountName = "data"
	indexVolumeMount   volumeMountName = "index"
	logsVolumeMount    volumeMountName = "logs"

	// Note: analytics names are dynamically generated as there can be more than one :/
	// This is just a prefix.
	analyticsVolumeMount volumeMountName = "analytics"
)

// volumeMount describes a volume mount for a specific service, for a specific pod.
type volumeMount struct {
	// nane is the human readable name of the service this mount belongs to.
	name volumeMountName

	// persistentVolumeClaimTemplateName is the template to use for generating
	// the persistent volume, this contains the size, and optionally the storage
	// class etc.
	persistentVolumeClaimTemplateName string

	// mountPath is where in the pod this could be mounted.
	mountPath string

	// persistentVolumeClaimName is the name of the PVC for this mount.
	persistentVolumeClaimName string
}

func newVolumeMount(member couchbaseutil.Member, name volumeMountName, persistentVolumeClaimTemplateName string) volumeMount {
	return volumeMount{
		name:                              name,
		persistentVolumeClaimTemplateName: persistentVolumeClaimTemplateName,
		mountPath:                         pathForVolumeMountName(name),
		persistentVolumeClaimName:         NameForPersistentVolumeClaim(member.Name(), 0, name),
	}
}

// volumeMountList holds all the volume mounts for a pod, dependent on what volumes
// for what services are configured.  This is ORDERED to facilitate deterministic
// generation (i.e. don't use a map).
type volumeMountList []volumeMount

func (l volumeMountList) Len() int {
	return len(l)
}

func (l volumeMountList) Less(i, j int) bool {
	return strings.Compare(string(l[i].name), string(l[j].name)) < 0
}

func (l volumeMountList) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}

// Get all paths to that should be persisted within pod.
func getPathsToPersist(member couchbaseutil.Member, mounts *couchbasev2.VolumeMounts) (volumeMountList, error) {
	mountPaths := volumeMountList{}

	defaultClaim := mounts.DefaultClaim
	dataClaim := mounts.DataClaim
	indexClaim := mounts.IndexClaim
	analyticsClaims := mounts.AnalyticsClaims
	logsClaim := mounts.LogsClaim

	// var to test existence of non default/logs mounts
	hasSecondaryMounts := dataClaim != "" || indexClaim != "" || analyticsClaims != nil

	// If no default claim is given (this persists /etc) and other data are persisted,
	// that's going to lead to broken.  This *should* be handled by the DAC, however
	// there is no way of checking this at present without the DAC, which may not be
	// deployed by some users.
	if defaultClaim == "" && hasSecondaryMounts {
		return nil, fmt.Errorf("%w: other mounts cannot be used in without `default` mount", errors.NewStackTracedError(errors.ErrConfigurationInvalid))
	}

	// When logs volumes are specified, these need to be mututally exclusive with
	// all other volume claims.
	if logsClaim != "" {
		if defaultClaim != "" || hasSecondaryMounts {
			return nil, fmt.Errorf("%w: other mounts cannot be used in with `logs` mount", errors.NewStackTracedError(errors.ErrConfigurationInvalid))
		}

		mountPaths = append(mountPaths, newVolumeMount(member, logsVolumeMount, logsClaim))

		return mountPaths, nil
	}

	mountPaths = append(mountPaths, newVolumeMount(member, defaultVolumeMount, defaultClaim))

	if dataClaim != "" {
		mountPaths = append(mountPaths, newVolumeMount(member, dataVolumeMount, dataClaim))
	}

	if indexClaim != "" {
		mountPaths = append(mountPaths, newVolumeMount(member, indexVolumeMount, indexClaim))
	}

	for index, template := range analyticsClaims {
		mountPaths = append(mountPaths, newVolumeMount(member, volumeMountName(fmt.Sprintf("%s-%02d", analyticsVolumeMount, index)), template))
	}

	// Important note... this used to be a map map, not a list map.  As a result the
	// ordering of iteration was non-detemrinistic, and thus the pods were upgraded
	// all the time by accident.  A legacy hangover (read as hack) was we ordered the
	// mount names and iterated using that, so we need to maintain this behaviour in
	// order to prevent upgrading everyone's clusters for them.
	sort.Stable(mountPaths)

	return mountPaths, nil
}

func GetAnalyticsVolumePaths(mounts *couchbasev2.VolumeMounts) []string {
	paths := []string{}

	if mounts.AnalyticsClaims == nil {
		return paths
	}

	for index := range mounts.AnalyticsClaims {
		paths = append(paths, fmt.Sprintf("/mnt/%s-%02d", analyticsVolumeMount, index))
	}

	return paths
}

func pathForVolumeMountName(id volumeMountName) string {
	var path string

	switch id {
	case defaultVolumeMount:
		return couchbaseVolumeDefaultConfigDir
	case dataVolumeMount:
		path = CouchbaseVolumeMountDataDir
	case indexVolumeMount:
		path = CouchbaseVolumeMountIndexDir
	case logsVolumeMount:
		path = CouchbaseVolumeMountLogsDir
	default:
		if strings.Contains(string(id), string(analyticsVolumeMount)) {
			// path resolves to /mnt/analytics-00 when matching on analytics volume
			path = fmt.Sprintf("/mnt/%s", id)
		}
	}

	return path
}

// Creates custom PVC from the generic spec.
func createPersistentVolumeClaim(client *client.Client, claim *v1.PersistentVolumeClaim, namespace string, owner metav1.OwnerReference) (*v1.PersistentVolumeClaim, error) {
	// can be mounted read/write mode to exactly 1 host
	addOwnerRefToObject(claim, owner)

	claim.Spec.AccessModes = []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}

	pvc, err := client.KubeClient.CoreV1().PersistentVolumeClaims(namespace).Create(context.Background(), claim, metav1.CreateOptions{})
	if err != nil {
		return nil, errors.NewStackTracedError(err)
	}

	return pvc, nil
}

// Updates existing PersistentVolumeClaim from required Spec.
func updatePersistentVolumeClaim(client *client.Client, claim *v1.PersistentVolumeClaim) (*v1.PersistentVolumeClaim, error) {
	// Only resources attribute of PersistentVolumeClaimSpec can be updated
	claim.Spec.AccessModes = []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}

	pvc, err := client.KubeClient.CoreV1().PersistentVolumeClaims(claim.Namespace).Update(context.Background(), claim, metav1.UpdateOptions{})
	if err != nil {
		return nil, errors.NewStackTracedError(err)
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
// when removeVolumes is 'true'.
func DeleteCouchbasePod(client *client.Client, namespace, name string, opts metav1.DeleteOptions, removeVolumes bool) error {
	if err := DeletePod(client, namespace, name, opts); err != nil {
		return err
	}

	if removeVolumes {
		if err := deletePodVolumes(client, name); err != nil {
			return err
		}
	}

	return nil
}

// list and delete persistent volumes associated with the member.
func deletePodVolumes(client *client.Client, memberName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	for _, pvc := range listMemberPVCS(client, memberName) {
		if err := client.KubeClient.CoreV1().PersistentVolumeClaims(pvc.Namespace).Delete(context.Background(), pvc.Name, CascadeDeleteOptions(0)); err != nil {
			return errors.NewStackTracedError(err)
		}

		name := pvc.Name

		// The PVC should be seen to disappear, or the member may be
		// 'brought back to life' by other code.
		callback := func() error {
			pvc, ok := client.PersistentVolumeClaims.Get(name)
			if !ok {
				return nil
			}

			if pvc.DeletionTimestamp != nil {
				return nil
			}

			return fmt.Errorf("%w: pvc %s not deleted", errors.NewStackTracedError(errors.ErrResourceExists), name)
		}

		if err := retryutil.Retry(ctx, time.Second, callback); err != nil {
			return err
		}
	}

	return nil
}

// list all PVC's belonging to the member.
func listMemberPVCS(client *client.Client, memberName string) (pvcs []*v1.PersistentVolumeClaim) {
	for _, pvc := range client.PersistentVolumeClaims.List() {
		if name, ok := pvc.Labels[constants.LabelNode]; ok && name == memberName {
			pvcs = append(pvcs, pvc)
		}
	}

	return
}

// MemberHasLogVolumes gets volumes for the named members and returns true
// if it has a log-only volume.
func MemberHasLogVolumes(client *client.Client, name string) bool {
	for _, pvc := range listMemberPVCS(client, name) {
		if pvc.Annotations == nil {
			return false
		}

		for key, value := range pvc.Annotations {
			if key == constants.AnnotationVolumeMountPath && value == CouchbaseVolumeMountLogsDir {
				return true
			}
		}
	}

	return false
}

// Names of persistent volume claims are combinations of
// Member name, mount type, and mount index.
// ie...: cb-example-0000-default-00, pvc-data-cb-example-0000-01-index.
func NameForPersistentVolumeClaim(memberName string, index int, mountName volumeMountName) string {
	return fmt.Sprintf("%s-%s-%02d", memberName, mountName, index)
}

// MaintainMutablePodConfiguration will preserve any annotations or labels that describe the metadata.
// Preserve any labels or annotations that are updated by other means, typically
// for pods this is only upgrades but also for Istio and similar third parties.
func MaintainMutablePodConfiguration(actual, requested *v1.Pod) {
	// Any duplicates are set based on what is in requested (overwriting actual/current)
	requested.Labels = mergeLabels(actual.Labels, requested.Labels)

	// We need to preserve some annotations particularly for upgrade so create a copy
	newAnnotations := mergeLabels(actual.Annotations, requested.Annotations)

	// Preserve the version as this dictates what labels and annotations are valid
	// for a specific operator version.  In particular for 2.2+ there is a label
	// that is added to say that Couchbase has been initialized, and thus cannot be
	// safely considered for deletion (this solves a deadlock where server doesn't
	// get fully configured and any attempts to get the cluster state are rejected
	// by server because it refuses to work properly until it has been configured).
	newAnnotations[constants.ResourceVersionAnnotation] = actual.Annotations[constants.ResourceVersionAnnotation]

	// The specification and couchbase server version are retained so that any
	// subsequent pod changes can be detected by the topology reconciler.  Not
	// doing this means that upgrades won't happen as they should.
	newAnnotations[constants.PodSpecAnnotation] = actual.Annotations[constants.PodSpecAnnotation]
	newAnnotations[constants.CouchbaseVersionAnnotationKey] = actual.Annotations[constants.CouchbaseVersionAnnotationKey]

	// Now use the copy as the version to set
	requested.Annotations = newAnnotations
}

// CreateCouchbasePodSpec creates an "idealized" pod specification.  This must be invariant
// across creations e.g. init containers are only needed during the initial creation and not
// required for recovery, therefore should not be done here.  We use this invariance property
// in order to trigger Couchbase upgrade sequences.  Pods are immutable so we use swap
// rebalances to upgrade not only the container version, but other attributes that are configurable
// in the server class pod policy, e.g. adding PVCs, scheduling constraints etc.
func CreateCouchbasePodSpec(client *client.Client, m couchbaseutil.Member, cluster *couchbasev2.CouchbaseCluster, config couchbasev2.ServerConfig, serverGroup string, pvcState *PersistentVolumeClaimState) (*v1.Pod, error) {
	// Create the standard Couchbase container image.
	container := couchbaseContainer(cluster, &config)
	container.ReadinessProbe = &v1.Probe{
		Handler: v1.Handler{
			TCPSocket: &v1.TCPSocketAction{
				Port: intstr.FromInt(8091),
			},
		},
		InitialDelaySeconds: 10,
		TimeoutSeconds:      5,
		PeriodSeconds:       20,
		FailureThreshold:    1,
	}

	applyContainerStorage(&container, pvcState)

	// Use the user provided Pod template if provided.
	pod := &v1.Pod{}

	if config.Pod != nil {
		// Always copy from cached data, don't modify the cache.
		// Cache data is only updated when something has changed,
		// any changes you make will linger and cause unexpected
		// things to happen...
		podTemplate := config.Pod.DeepCopy()

		pod.ObjectMeta = podTemplate.ObjectMeta.ToObjectMeta()
		pod.Spec = podTemplate.Spec
	}

	// For metadata, override the name and merge the labels and annotations.
	pod.Name = m.Name()
	pod.Labels = mergeLabels(pod.Labels, createCouchbasePodLabels(m.Name(), cluster.Name, config))
	ApplyBaseAnnotations(pod)

	// Populate the main specification, overriding whatever the template specified.
	pod.Spec.Containers = []v1.Container{
		container,
	}
	pod.Spec.RestartPolicy = v1.RestartPolicyNever
	pod.Spec.Hostname = m.Name()
	pod.Spec.Subdomain = cluster.Name
	pod.Spec.SecurityContext = cluster.Spec.SecurityContext
	pod.Spec.ReadinessGates = []v1.PodReadinessGate{
		{
			ConditionType: podReadinessCondition,
		},
	}

	applyPodScheduling(cluster, pod, serverGroup)
	applyPodNetworking(cluster, pod, m)
	applyPodStorage(pod, pvcState)
	applyPodLogging(cluster, pod)

	// Note: anything using clients to look up state at this point, is probably doing
	// it wrong, gut feeling.
	if err := applyPodMonitoring(client, cluster, pod); err != nil {
		return nil, err
	}

	// Break out the detection and application of monitoring labels/annotations based on
	// what is enabled, server version, etc.
	applyMetadata(cluster, pod)

	// If TLS is specified then add the certificate volume.
	if err := applyPodTLSConfiguration(cluster, pod); err != nil {
		return nil, err
	}

	// Set the Couchbase version metadata.
	if err := SetCouchbaseVersion(pod, cluster.Spec.CouchbaseImage()); err != nil {
		return nil, err
	}

	// Add the original specification to the annotations, we will use this to upgrade
	// the pods when the specification differs.  We cannot rely on the Pod specification
	// being immutable once committed to Kubernetes as it may be subject to defaulting.
	// We work in exactly the same way as a `kubectl apply command`.
	specJSON, err := json.Marshal(pod.Spec)
	if err != nil {
		return nil, errors.NewStackTracedError(err)
	}

	pod.Annotations[constants.PodSpecAnnotation] = string(specJSON)

	return pod, nil
}

func applyContainerStorage(container *v1.Container, pvcState *PersistentVolumeClaimState) {
	if pvcState == nil {
		return
	}

	container.VolumeMounts = pvcState.volumeMounts
}

// applyPodScheduling adds any scheduling to place the pods in the right place.
func applyPodScheduling(cluster *couchbasev2.CouchbaseCluster, pod *v1.Pod, serverGroup string) {
	// If anti-affinity is set then ensure no two pods from the same cluster
	// run on the same hosts.
	if cluster.Spec.AntiAffinity {
		pod.Spec.Affinity = AntiAffinityForCluster(cluster.Name)
	}

	// Add or override any scheduling operation.  Server group will be set when
	// we are recovering from a pod deletion, and we have to place the pod in the
	// same AZ as the volume from which it is recovering.  Pods are scheduled where
	// they want, irrespective of existing volumes (is this still the case??)
	if serverGroup != "" {
		if pod.Spec.NodeSelector == nil {
			pod.Spec.NodeSelector = map[string]string{}
		}

		pod.Spec.NodeSelector[constants.ServerGroupLabel] = serverGroup
	}
}

// applyPodNetworking adds any network specific hacks required to work.
func applyPodNetworking(cluster *couchbasev2.CouchbaseCluster, pod *v1.Pod, m couchbaseutil.Member) {
	// If we are in istio mode, add in DNS configuration to avoid hairpinning
	// which causes death with mTLS enabled.  Also note that Analytics is broke
	// with this until 6.5.1.
	if cluster.Spec.Networking.NetworkPlatform == nil {
		return
	}

	if *cluster.Spec.Networking.NetworkPlatform != couchbasev2.NetworkPlatformIstio {
		return
	}

	pod.Spec.HostAliases = []v1.HostAlias{
		{
			IP: "127.0.0.1",
			Hostnames: []string{
				"localhost",
				m.GetDNSName(),
			},
		},
	}
}

// applyPodStorage adds any storage options to the pod.
func applyPodStorage(pod *v1.Pod, pvcState *PersistentVolumeClaimState) {
	if pvcState == nil {
		return
	}

	pod.Spec.Volumes = append(pod.Spec.Volumes, pvcState.volumes...)
}

// applyPodMonitoring adds any monitoring related hacks required to work.
func applyPodMonitoring(client *client.Client, cluster *couchbasev2.CouchbaseCluster, pod *v1.Pod) error {
	// If monitoring is enabled add the necessary side cars.
	if cluster.Spec.Monitoring == nil {
		return nil
	}

	if cluster.Spec.Monitoring.Prometheus == nil {
		return nil
	}

	if !cluster.Spec.Monitoring.Prometheus.Enabled {
		return nil
	}

	metricsContainer := createMetricsContainer(cluster.Spec)

	if err := applyMetricsPodSecurity(client, cluster.Spec, &metricsContainer, pod); err != nil {
		return err
	}

	applyMetricsPodTLS(cluster.Spec, &metricsContainer, pod)
	pod.Spec.Containers = append(pod.Spec.Containers, metricsContainer)

	return nil
}

func applyMetadata(cluster *couchbasev2.CouchbaseCluster, pod *v1.Pod) {
	// Are we using a version of Server that has its own metrics exporter
	serverVersionPrometheus := false

	tag, err := CouchbaseVersion(cluster.Spec.Image)
	if err != nil {
		serverVersionPrometheus, _ = couchbaseutil.VersionAfter(tag, "7.0.0")
	}

	// Do we have the Prometheus exporter enabled
	exporterEnabled := cluster.Spec.Monitoring != nil && cluster.Spec.Monitoring.Prometheus != nil && cluster.Spec.Monitoring.Prometheus.Enabled
	// Do we have the logging side car enabled
	loggingEnabled := cluster.Spec.Logging.Server != nil && cluster.Spec.Logging.Server.Enabled

	// Set up the Prometheus exporter values as the defaults
	metricsPath := prometheusPath
	metricsPort := strconv.Itoa(int(prometheusPort))

	// If we're using CBS 7 then assume it takes precedence over exporter
	if serverVersionPrometheus {
		// As always documentation is hard to come across but this appears to be the metrics path for CBS 7+
		// :8091/metrics
		metricsPort = strconv.Itoa(AdminServicePort)
	} else if loggingEnabled && !exporterEnabled {
		// logging metrics are only used if exporter and CBS 7 are not in place
		metricsPath = "/api/v1/metrics/prometheus"
		metricsPort = strconv.Itoa(loggingPort)
	}

	// Set up the annotations to apply by default
	annotations := map[string]string{
		// If we have at least one then we want to scrape, if not we want to make sure we disable scraping
		constants.AnnotationPrometheusScrape: strconv.FormatBool(serverVersionPrometheus || exporterEnabled || loggingEnabled),
		constants.AnnotationPrometheusPath:   metricsPath,
		constants.AnnotationPrometheusPort:   metricsPort,
	}

	if loggingEnabled {
		// Add a suggested parser to help with Fluent Bit usage as a daemonset
		// https://docs.fluentbit.io/manual/pipeline/filters/kubernetes#kubernetes-annotations
		annotations["fluentbit.io/parser_stdout-"+CouchbaseLogSidecarContainerName] = "couchbase_sidecar"
		// Add default logging annotation: https://github.com/kubernetes/kubernetes/pull/87809
		annotations["kubectl.kubernetes.io/default-logs-container"] = CouchbaseLogSidecarContainerName
	}

	pod.Annotations = mergeLabels(pod.Annotations, annotations)
}

func getLoggingMount(container *v1.Container) *v1.VolumeMount {
	for i, mount := range container.VolumeMounts {
		if mount.MountPath != CouchbaseVolumeMountLogsDir && mount.MountPath != couchbaseVolumeDefaultConfigDir {
			continue
		}

		return &container.VolumeMounts[i]
	}

	return nil
}

func applyPodLogging(cluster *couchbasev2.CouchbaseCluster, pod *v1.Pod) {
	fbs := cluster.Spec.Logging.Server

	if fbs == nil || !fbs.Enabled {
		return
	}

	// Iterate over mounts to find the one we need for logs
	container, _ := getCouchbaseContainer(pod)
	mount := getLoggingMount(container)

	sidecarConfig := fbs.Sidecar

	// Set up the volume containing the Secret contents to configure the sidecar
	configVolumeMount := v1.VolumeMount{
		Name:      fbs.ConfigurationName,
		MountPath: sidecarConfig.ConfigurationMountPath,
		ReadOnly:  true,
	}

	// Ensure we specify the configuration file to use - fixed currently.
	configFile := filepath.Join(sidecarConfig.ConfigurationMountPath, LoggingConfigurationFile)

	// Set up a duplicate volume mount but make sure to set it read-only
	readonlyLogsMount := mount.DeepCopy()
	readonlyLogsMount.ReadOnly = true

	// If we have the default mount then extend further to only provide the logs sub-path
	if mount.MountPath == couchbaseVolumeDefaultConfigDir {
		readonlyLogsMount.SubPath = defaultSubPathName + "/logs"
		readonlyLogsMount.MountPath = CouchbaseVolumeMountLogsDir
	}

	// Optional resource requirements - either left nil or set to what is provided.
	var loggingResources v1.ResourceRequirements

	if sidecarConfig.Resources != nil {
		loggingResources = *sidecarConfig.Resources
	}

	// The volume holding pod meta-data for use by the sidecar
	metaDataVolueMount := v1.VolumeMount{
		Name:      loggingSidecarMetadataMountName,
		MountPath: loggingSidecarMetadataMountDir,
		ReadOnly:  true,
	}

	// Create a side car container to retrieve the logs.
	logging := v1.Container{
		Name:  CouchbaseLogSidecarContainerName,
		Image: sidecarConfig.Image,
		VolumeMounts: []v1.VolumeMount{
			*readonlyLogsMount,
			configVolumeMount,
			metaDataVolueMount,
		},
		Env: []v1.EnvVar{
			// Where the logs are we want to consume.
			{
				Name:  "COUCHBASE_LOGS",
				Value: CouchbaseVolumeMountLogsDir,
			},
			// The area to watch for updates to restart on.
			{
				Name:  "COUCHBASE_LOGS_DYNAMIC_CONFIG",
				Value: sidecarConfig.ConfigurationMountPath,
			},
			// The actual location of the config file - required even though part of the Secret.
			{
				Name:  "COUCHBASE_LOGS_CONFIG_FILE",
				Value: configFile,
			},
			// The location of the metadata we provide from k8s.
			{
				Name:  "COUCHBASE_K8S_CONFIG_DIR",
				Value: loggingSidecarMetadataMountDir,
			},
			{
				Name: "POD_NAME",
				ValueFrom: &v1.EnvVarSource{
					FieldRef: &v1.ObjectFieldSelector{
						FieldPath: "metadata.name",
					},
				},
			},
			{
				Name: "POD_NAMESPACE",
				ValueFrom: &v1.EnvVarSource{
					FieldRef: &v1.ObjectFieldSelector{
						FieldPath: "metadata.namespace",
					},
				},
			},
			{
				Name: "POD_UID",
				ValueFrom: &v1.EnvVarSource{
					FieldRef: &v1.ObjectFieldSelector{
						FieldPath: "metadata.uid",
					},
				},
			},
		},
		Resources: loggingResources,
		Ports: []v1.ContainerPort{
			{
				Name:          "http",
				ContainerPort: loggingPort,
				Protocol:      v1.ProtocolTCP,
			},
		},
	}

	pod.Spec.Volumes = append(pod.Spec.Volumes,
		// Make sure we include the volume for the Secret as well
		v1.Volume{
			Name: fbs.ConfigurationName,
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: fbs.ConfigurationName,
				},
			},
		},
		// Add the pod meta-data as well from annotations and labels as files
		v1.Volume{
			Name: loggingSidecarMetadataMountName,
			VolumeSource: v1.VolumeSource{
				DownwardAPI: &v1.DownwardAPIVolumeSource{
					Items: []v1.DownwardAPIVolumeFile{
						{
							Path: "labels",
							FieldRef: &v1.ObjectFieldSelector{
								FieldPath: "metadata.labels",
							},
						},
						{
							Path: "annotations",
							FieldRef: &v1.ObjectFieldSelector{
								FieldPath: "metadata.annotations",
							},
						},
					},
				},
			},
		},
	)

	pod.Spec.Containers = append(pod.Spec.Containers, logging)

	// Deal with audit log cleanup if both auditing is enabled and then GC is also enabled.
	// Disgusting solution to remove all rotated audit logs after configurable amount of timeand output those deleted to stdout for reference.
	acs := cluster.Spec.Logging.Audit

	if acs == nil || !acs.Enabled || acs.GarbageCollection == nil {
		return
	}

	// Determine if GC is enabled
	gc := acs.GarbageCollection.Sidecar

	if gc == nil || !gc.Enabled {
		return
	}

	// Convert & truncate age to mmin units to handle more granularity than days with mtime
	age := int64(gc.Age.Duration.Minutes())
	if age < 0 {
		age = 0
	}

	// Now we want the interval to sleep for between runs
	interval := int64(gc.Interval.Duration.Seconds())
	if interval < 0 {
		interval = 0
	}

	var auditResources v1.ResourceRequirements
	if gc.Resources != nil {
		auditResources = *gc.Resources
	}

	auditMount := mount.DeepCopy()

	// This is a significant security concern but is required to delete
	auditMount.ReadOnly = false

	// If we have the default mount then extend further to only provide the logs sub-path.
	// An attempt at mitigating some security concerns with full access to a volume from an arbitrary shell.
	if mount.MountPath == couchbaseVolumeDefaultConfigDir {
		auditMount.SubPath = defaultSubPathName + "/logs"
	}

	auditcleaner := v1.Container{
		Name:  CouchbaseAuditCleanupSidecarContainerName,
		Image: gc.Image,
		VolumeMounts: []v1.VolumeMount{
			*auditMount,
		},
		Command: []string{
			"/bin/sh",
		},
		// Note that no support for relocation of audit logs - this should not ever be done with the operator.
		// We also provide the env vars for the various intervals in case someone wants to override things in the future.
		Env: []v1.EnvVar{
			{
				Name:  "AUDIT_LOG_DIR",
				Value: mount.MountPath,
			},
			{
				Name:  "AUDIT_CLEANUP_INTERVAL",
				Value: strconv.FormatInt(interval, 10),
			},
			{
				Name:  "AUDIT_CLEANUP_AGE",
				Value: strconv.FormatInt(age, 10),
			},
		},
		Args: []string{
			"-c",
			"while true; do sleep ${AUDIT_CLEANUP_INTERVAL} ; echo \"Cleaning audit logs every ${AUDIT_CLEANUP_INTERVAL}s, files older than ${AUDIT_CLEANUP_AGE}\"; find ${AUDIT_LOG_DIR} -mmin ${AUDIT_CLEANUP_AGE} -type f -name \"*-audit.log\" -delete -print; done",
		},
		Resources: auditResources,
	}

	pod.Spec.Containers = append(pod.Spec.Containers, auditcleaner)
}

func applyMetricsPodTLS(cs couchbasev2.ClusterSpec, container *v1.Container, pod *v1.Pod) {
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
			container.Args = append(container.Args,
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
			container.Args = append(container.Args,
				"--ca", operatorSecretMountPath+"/ca.crt")

			container.ReadinessProbe.Handler.HTTPGet.Scheme = v1.URISchemeHTTPS
		}

		if cs.Networking.TLS.ClientCertificatePolicy != nil {
			// add the TLS server flags to the couchbase-exporter binary
			container.Args = append(container.Args,
				"--client-cert", operatorSecretMountPath+"/couchbase-operator.crt",
				"--client-key", operatorSecretMountPath+"/couchbase-operator.key")
		}
	}
}

func applyMetricsPodSecurity(client *client.Client, cs couchbasev2.ClusterSpec, container *v1.Container, pod *v1.Pod) error {
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

		container.Args = append(container.Args, "--token", metricsTokenMountPath+"/token")

		secret, ok := client.Secrets.Get(*cs.Monitoring.Prometheus.AuthorizationSecret)
		if !ok {
			return errors.NewStackTracedError(fmt.Errorf("%w: unable to read monitoring token", errors.ErrResourceRequired))
		}

		token, ok := secret.Data["token"]
		if !ok {
			return errors.NewStackTracedError(fmt.Errorf("%w: monitoring token missing in secret", errors.ErrResourceAttributeRequired))
		}

		container.ReadinessProbe.Handler.HTTPGet.HTTPHeaders = []v1.HTTPHeader{
			{
				Name:  "Authorization",
				Value: "Bearer " + string(token),
			},
		}
	}

	return nil
}

func createCouchbasePodLabels(memberName, clusterName string, ns couchbasev2.ServerConfig) map[string]string {
	labels := map[string]string{
		constants.LabelApp:      constants.App,
		constants.LabelServer:   "true",
		constants.LabelNode:     memberName,
		constants.LabelNodeConf: ns.Name,
		constants.LabelCluster:  clusterName,
	}

	for _, s := range ns.Services {
		k := constants.LabelServicePrefix + s.String()
		labels[k] = constants.EnabledValue
	}

	return labels
}

func CouchbaseContainer(image string) v1.Container {
	cluster := &couchbasev2.CouchbaseCluster{
		Spec: couchbasev2.ClusterSpec{
			Image: image,
		},
	}

	return couchbaseContainer(cluster, nil)
}

func couchbaseContainerPorts() ([]v1.ContainerPort, error) {
	// Create a service which defines A records for all pods, we use this internally
	// to address nodes via stable names (IPs are not fixed)
	ports := []v1.ContainerPort{}

	for _, rule := range allTheThings {
		switch len(rule) {
		case 1:
			ports = append(ports, v1.ContainerPort{
				Name:          fmt.Sprintf("tcp-%v", rule[0]),
				ContainerPort: int32(rule[0]),
				Protocol:      v1.ProtocolTCP,
			})
		case 2:
			for i := rule[0]; i <= rule[1]; i++ {
				ports = append(ports, v1.ContainerPort{
					Name:          fmt.Sprintf("tcp-%v", i),
					ContainerPort: int32(i),
					Protocol:      v1.ProtocolTCP,
				})
			}
		default:
			return nil, fmt.Errorf("%w: illegal port rule: %v", errors.NewStackTracedError(errors.ErrInternalError), rule)
		}
	}

	return ports, nil
}

func couchbaseContainer(cluster *couchbasev2.CouchbaseCluster, config *couchbasev2.ServerConfig) v1.Container {
	ports, _ := couchbaseContainerPorts()

	c := v1.Container{
		Name:  constants.CouchbaseContainerName,
		Image: cluster.Spec.CouchbaseImage(),
		Ports: ports,
	}

	if config == nil {
		return c
	}

	c.Env = config.Env
	c.EnvFrom = config.EnvFrom
	c.Resources = config.Resources

	// Automatically configure resource memory requests, mainly for lazy users,
	// but also to prevent memory starvation and random OOM killings.  It must
	// be manually enabled to maintain current behaviour...
	autoAllocation := cluster.Spec.AutoResourceAllocation != nil && cluster.Spec.AutoResourceAllocation.Enabled

	if autoAllocation {
		memoryRequests := resource.Quantity{}

		for _, service := range config.Services {
			switch service {
			case couchbasev2.DataService:
				memoryRequests.Add(*cluster.Spec.ClusterSettings.DataServiceMemQuota)
			case couchbasev2.IndexService:
				memoryRequests.Add(*cluster.Spec.ClusterSettings.IndexServiceMemQuota)
			case couchbasev2.QueryService:
				if cluster.Spec.ClusterSettings.QueryServiceMemQuota != nil {
					memoryRequests.Add(*cluster.Spec.ClusterSettings.QueryServiceMemQuota)
				}
			case couchbasev2.SearchService:
				memoryRequests.Add(*cluster.Spec.ClusterSettings.SearchServiceMemQuota)
			case couchbasev2.EventingService:
				memoryRequests.Add(*cluster.Spec.ClusterSettings.EventingServiceMemQuota)
			case couchbasev2.AnalyticsService:
				memoryRequests.Add(*cluster.Spec.ClusterSettings.AnalyticsServiceMemQuota)
			}
		}

		overhead := resource.NewQuantity((memoryRequests.Value()*int64(cluster.Spec.AutoResourceAllocation.OverheadPercent))/100, resource.BinarySI)

		memoryRequests.Add(*overhead)

		// Add requests and limits maps if they don't exist.
		if c.Resources.Requests == nil {
			c.Resources.Requests = v1.ResourceList{}
		}

		if c.Resources.Limits == nil {
			c.Resources.Limits = v1.ResourceList{}
		}

		// If not already defined explicitly add in the implicit memory request.
		if _, ok := c.Resources.Requests[v1.ResourceMemory]; !ok {
			c.Resources.Requests[v1.ResourceMemory] = memoryRequests
		}

		// If not already defined explicitly add in the implicit cpu request.
		if cluster.Spec.AutoResourceAllocation.CPURequests != nil {
			if _, ok := c.Resources.Requests[v1.ResourceCPU]; !ok {
				c.Resources.Requests[v1.ResourceCPU] = *cluster.Spec.AutoResourceAllocation.CPURequests
			}
		}

		// If not already defined explicitly add in the implicit cpu limit.
		if cluster.Spec.AutoResourceAllocation.CPULimits != nil {
			if _, ok := c.Resources.Limits[v1.ResourceCPU]; !ok {
				c.Resources.Limits[v1.ResourceCPU] = *cluster.Spec.AutoResourceAllocation.CPULimits
			}
		}
	}

	return c
}

// Init container is same as runtime container except it used
// to copy the etc dir into a persisted volume which will be
// shared with with the Pod's main container.
func couchbaseInitContainer(cluster *couchbasev2.CouchbaseCluster, claimName string, config couchbasev2.ServerConfig) v1.Container {
	initContainer := couchbaseContainer(cluster, &config)
	initContainer.Name = fmt.Sprintf("%s-init", constants.CouchbaseContainerName)
	initContainer.Args = []string{"bash", "-c", "if [[ ! -e /mnt/etc ]]; then cp -a /opt/couchbase/etc /mnt/; fi"}
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
		Name:  MetricsContainerName,
		Image: cs.Monitoring.Prometheus.MetricsImage(),
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
		Ports: []v1.ContainerPort{
			{
				Name:          "prometheus",
				ContainerPort: int32(prometheusPort),
				Protocol:      v1.ProtocolTCP,
			},
		},
		ReadinessProbe: &v1.Probe{
			Handler: v1.Handler{
				HTTPGet: &v1.HTTPGetAction{
					Path: prometheusPath,
					Port: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: prometheusPort,
					},
					Scheme: v1.URISchemeHTTP,
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

// Given a pod, return a pointer to the couchbase container.
func getCouchbaseContainer(pod *v1.Pod) (*v1.Container, error) {
	for index := range pod.Spec.Containers {
		if pod.Spec.Containers[index].Name == constants.CouchbaseContainerName {
			return &pod.Spec.Containers[index], nil
		}
	}

	return nil, fmt.Errorf("%w: unable to locate couchbase container", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
}

// ShadowTLSSecretName generates a TLS secret name when shadowing is in use.
func ShadowTLSSecretName(cluster *couchbasev2.CouchbaseCluster) string {
	return cluster.Name + "-tls-shadow"
}

// Adds any necessary pod prerequisites before enabling TLS.
func applyPodTLSConfiguration(cluster *couchbasev2.CouchbaseCluster, pod *v1.Pod) error {
	// Static configuration:
	// * Defines a volume which contains the secrets necessary
	//   to explicitly define TLS certificates and keys
	// * Mounts the volume in in the correct location so that API
	//   calls to /node/controller/reloadCertificate succeed
	if cluster.IsTLSEnabled() {
		// Add the TLS secret volume to the pod
		volume := v1.Volume{
			Name: constants.CouchbaseTLSVolumeName,
		}

		var secretName string

		switch {
		case cluster.Spec.Networking.TLS.Static != nil:
			secretName = cluster.Spec.Networking.TLS.Static.ServerSecret
		case cluster.Spec.Networking.TLS.SecretSource != nil:
			secretName = ShadowTLSSecretName(cluster)
		default:
			return fmt.Errorf("%w: no TLS source configured", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
		}

		volume.VolumeSource.Secret = &v1.SecretVolumeSource{
			SecretName: secretName,
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
		pod.Annotations[constants.PodTLSAnnotation] = constants.EnabledValue
	}

	return nil
}

// IsPodReady returns false if the Pod Status is nil.
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
// It's not considered an error in the case that PVC cannot be found.
func findMemberPVC(client *client.Client, memberName, path string) (*v1.PersistentVolumeClaim, error) {
	for _, pvc := range listMemberPVCS(client, memberName) {
		if pvcPath, ok := pvc.Annotations[constants.AnnotationVolumeMountPath]; ok {
			if pvcPath == path {
				phase := pvc.Status.Phase
				switch phase {
				case v1.ClaimBound:
					return pvc, nil
				default:
					return nil, fmt.Errorf("%w: volume %s for %s is %s, expected Bound", errors.NewStackTracedError(errors.ErrKubernetesError), path, memberName, phase)
				}
			}
		}
	}

	return nil, fmt.Errorf("%w: volume %s for %s missing", errors.NewStackTracedError(errors.ErrResourceRequired), path, memberName)
}

// Recreate list of members from persistent volumes.
func PVCToMemberset(client *client.Client, cluster, namespace string, secure bool) couchbaseutil.MemberSet {
	ms := couchbaseutil.MemberSet{}

	for _, pvc := range client.PersistentVolumeClaims.List() {
		// Ignore deleting PVCs
		if pvc.DeletionTimestamp != nil {
			continue
		}

		// claim must be bound to a volume
		if pvc.Status.Phase != v1.ClaimBound {
			// BUG: tell me why you are ignoring it in the logs!
			continue
		}

		// reject log volumes, they cannot be brought back to life.
		if IsLogPVC(pvc) {
			continue
		}

		// require members to have path
		if _, ok := pvc.Annotations[constants.AnnotationVolumeMountPath]; !ok {
			// BUG: tell me why you are ignoring it in the logs!
			continue
		}

		name, ok := pvc.Labels[constants.LabelNode]
		if !ok {
			// BUG: tell me why you are ignoring it in the logs!
			continue
		}

		config, ok := pvc.Annotations[constants.AnnotationVolumeNodeConf]
		if !ok {
			// BUG: tell me why you are ignoring it in the logs!
			continue
		}

		version, ok := pvc.Annotations[constants.CouchbaseVersionAnnotationKey]
		if !ok {
			// BUG: tell me why you are ignoring it in the logs!
			continue
		}

		ms.Add(couchbaseutil.NewMember(namespace, cluster, name, version, config, secure))
	}

	return ms
}

// pod is recoverable if it has volume mounts with existing
// persistentVolumeClaims.  The claims must also be bound to
// backing volumes.  Every claim used by the pod must be bound
// to an underlying PersistentVolume.
func IsPodRecoverable(client *client.Client, config couchbasev2.ServerConfig, member couchbaseutil.Member) error {
	mounts := config.GetVolumeMounts()
	if mounts == nil || mounts.LogsOnly() {
		return errors.NewStackTracedError(errors.ErrNoVolumeMounts)
	}

	// default volume claim is required for recovery
	defaultClaim := mounts.DefaultClaim
	if defaultClaim == "" {
		return fmt.Errorf("%w: no claim defined for default volume", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	// all volume mounts must be healthy
	mountMappings, err := getPathsToPersist(member, mounts)
	if err != nil {
		return err
	}

	for _, mountMapping := range mountMappings {
		if _, err := findMemberPVC(client, member.Name(), mountMapping.mountPath); err != nil {
			return err
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

// CheckVolumeExpansionEvents checks PVC events for successful resize
// event to determine status when an expansion has occurred.
func checkVolumeResizeFailure(client *client.Client, claim *v1.PersistentVolumeClaim) error {
	// Check if Volume Claim to has "Resize" condition set
	var expansionTimestamp metav1.Time

	for _, condition := range claim.Status.Conditions {
		if condition.Type == v1.PersistentVolumeClaimResizing || condition.Type == v1.PersistentVolumeClaimFileSystemResizePending {
			if condition.Status == v1.ConditionTrue {
				expansionTimestamp = condition.LastTransitionTime
				break
			}
		}
	}

	// The volume is not yet have resize condition set
	if expansionTimestamp.IsZero() {
		return nil
	}

	events, err := GetEventsForResource(client.KubeClient, claim.Namespace, "PersistentVolumeClaim", claim.Name)
	if err != nil {
		return err
	}

	for _, event := range events {
		// Only consider events which occur after the resize condition is presented
		if expansionTimestamp.Before(&event.LastTimestamp) {
			if event.Reason == "VolumeResizeFailed" {
				return errors.ErrVolumeResizeError
			}
		}
	}

	return nil
}

// GetVolumeStorageSize returns requested storage size of a volume claim.
func GetVolumeStorageSize(claim *v1.PersistentVolumeClaim) string {
	// In the event that storage key does not exist an empty string is returned,
	// so caller should check against what is expected if necessary.
	var size string
	if val, ok := claim.Spec.Resources.Requests[v1.ResourceStorage]; ok {
		size = val.String()
	}

	return size
}

// FlagPodReady adds a readiness gate to the pod so we can have explicit control over
// pod eviction, when used in conjunction with a pod disruption budget, through the
// pod resource only.
func FlagPodReady(client *client.Client, name string) error {
	pod, found := client.Pods.Get(name)
	if !found {
		return fmt.Errorf("%w: pod %s not found", errors.NewStackTracedError(errors.ErrResourceRequired), name)
	}

	for _, condition := range pod.Status.Conditions {
		if condition.Type == podReadinessCondition {
			return nil
		}
	}

	now := metav1.Time{
		Time: time.Now(),
	}

	condition := v1.PodCondition{
		Type:               podReadinessCondition,
		Status:             v1.ConditionTrue,
		LastTransitionTime: now,
	}

	mergePatch, err := json.Marshal(condition)
	if err != nil {
		return errors.NewStackTracedError(err)
	}

	// Yes it's ugly, but efficient.
	mergePatch = []byte(`{"status":{"conditions":[` + string(mergePatch) + `]}}`)

	if _, err := client.KubeClient.CoreV1().Pods(pod.Namespace).Patch(context.Background(), pod.Name, apitypes.StrategicMergePatchType, mergePatch, metav1.PatchOptions{}, "status"); err != nil {
		return errors.NewStackTracedError(err)
	}

	return nil
}

func AntiAffinityForCluster(clusterName string) *v1.Affinity {
	return &v1.Affinity{
		PodAntiAffinity: &v1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							constants.LabelCluster: clusterName,
						},
					},
					TopologyKey: "kubernetes.io/hostname",
				},
			},
		},
	}
}

// SetPodInitialized sets the pod initialized annotation.  This means that the
// pod has either been fully set up (first node), or has successfully joined in
// to the cluster (all subsequent nodes).  Before this annotation is set, we haven't
// the faintest clue what state the pod is in, and more pertinently, it may not
// even respond to poking.
func SetPodInitialized(client *client.Client, name string) error {
	// Get the most recent cached version, just in case the generation has been
	// bumped asynchronously and we're out of sync.  Also give it a couple retries
	// to make this less error prone.
	callback := func() error {
		pod, ok := client.Pods.Get(name)
		if !ok {
			return fmt.Errorf("%w: unable to set initialized annotation", errors.NewStackTracedError(errors.ErrResourceRequired))
		}

		if pod.Annotations == nil {
			pod.Annotations = map[string]string{}
		}

		pod.Annotations[constants.PodInitializedAnnotation] = "true"

		if _, err := client.KubeClient.CoreV1().Pods(pod.Namespace).Update(context.Background(), pod, metav1.UpdateOptions{}); err != nil {
			return err
		}

		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	return retryutil.Retry(ctx, time.Second, callback)
}
