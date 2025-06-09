package v2

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util"
	"github.com/couchbase/couchbase-operator/pkg/util/annotations"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("cluster")

func (c *CouchbaseCluster) AsOwner() metav1.OwnerReference {
	trueVar := true

	return metav1.OwnerReference{
		APIVersion:         SchemeGroupVersion.String(),
		Kind:               ClusterCRDResourceKind,
		Name:               c.Name,
		UID:                c.UID,
		Controller:         &trueVar,
		BlockOwnerDeletion: &trueVar,
	}
}

// Convert from typed to string.
func (s Service) String() string {
	return string(s)
}

// Len returns the ServiceList length.
func (l ServiceList) Len() int {
	return len(l)
}

// Less compares two ServiceList items and returns true if a is less than b.
func (l ServiceList) Less(a, b int) bool {
	return l[a].String() < l[b].String()
}

// Swap swaps the position of two ServiceList elements.
func (l ServiceList) Swap(a, b int) {
	l[a], l[b] = l[b], l[a]
}

// Contains returns true if a service is part of a service list.
func (l ServiceList) Contains(service Service) bool {
	for _, s := range l {
		if s == service {
			return true
		}
	}

	return false
}

// ContainsAny returns true if any service is part of a service list.
func (l ServiceList) ContainsAny(services ...Service) bool {
	for _, service := range services {
		if l.Contains(service) {
			return true
		}
	}

	return false
}

// Sub removes members from 'other' from a ServiceList.
func (l ServiceList) Sub(other ServiceList) ServiceList {
	out := ServiceList{}

	for _, service := range l {
		if other.Contains(service) {
			continue
		}

		out = append(out, service)
	}

	return out
}

func NewServiceList(services []string) ServiceList {
	// TODO: Once the reflection stuff makes it in we can bin this
	// as things will be happily type safe and use the enumerations
	l := make(ServiceList, len(services))

	for i, s := range services {
		l[i] = Service(s)
	}

	return l
}

// Convert from a typed array to plain string array.
func (l ServiceList) StringSlice() []string {
	slice := make([]string, len(l))

	for i, s := range l {
		slice[i] = s.String()
	}

	return slice
}

var SupportedFeatures = []ExposedFeature{
	FeatureAdmin,
	FeatureXDCR,
	FeatureClient,
}

// Contains returns true if a requested feature is enabled.
func (l ExposedFeatureList) Contains(feature string) bool {
	for _, f := range l {
		if string(f) == feature {
			return true
		}
	}

	return false
}

// HasVolumeMounts returns true if volume mounts are defined.
// This does not check the existence of individual fields.
func (v *VolumeMounts) HasVolumeMounts() bool {
	return v != nil
}

// HasDefaultMount return true if the default mount is specified.
func (v *VolumeMounts) HasDefaultMount() bool {
	if !v.HasVolumeMounts() {
		return false
	}

	return v.DefaultClaim != ""
}

// LogsOnly returns true if logs will be the only mounts applied to cluster.
func (v *VolumeMounts) LogsOnly() bool {
	return v.LogsClaim != ""
}

func (v *VolumeMounts) HasSubMounts() bool {
	return v.DataClaim != "" || v.IndexClaim != "" || v.AnalyticsClaims != nil
}

func (sc *ServerConfig) GetVolumeMounts() *VolumeMounts {
	if sc != nil {
		return sc.VolumeMounts
	}

	return nil
}

func (sc *ServerConfig) GetDefaultVolumeClaim() string {
	if sc != nil {
		if mounts := sc.GetVolumeMounts(); mounts != nil {
			return mounts.DefaultClaim
		}
	}

	return ""
}

// Autoscale resources are named after cluster
// and config to ensure uniqueness.
func (sc *ServerConfig) AutoscalerName(cluster string) string {
	// Name must be valid dns subdomain.
	// Just going to remove potentially bad chars and replace with '-'
	// instead of imposing restrictions on server config.
	// Validation will warn of duplicates.
	reg := regexp.MustCompile("[^A-Za-z0-9]+")
	name := reg.ReplaceAllString(sc.Name, "-")

	return name + "." + cluster
}

func (cs *ClusterSpec) Cleanup() {
}

func (cs *ClusterSpec) TotalSize() int {
	size := 0
	for _, server := range cs.Servers {
		size += server.Size
	}

	return size
}

func (o ObjectMeta) ToObjectMeta() metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Labels:      o.Labels,
		Annotations: o.Annotations,
	}
}

func (o NamedObjectMeta) ToObjectMeta() metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:        o.Name,
		Labels:      o.Labels,
		Annotations: o.Annotations,
	}
}

// Get the volumeClaimTemplate with specified name.
func (cs *ClusterSpec) GetVolumeClaimTemplate(name string) *v1.PersistentVolumeClaim {
	for _, claim := range cs.VolumeClaimTemplates {
		if name == claim.ObjectMeta.Name {
			pvc := &v1.PersistentVolumeClaim{
				ObjectMeta: claim.ObjectMeta.ToObjectMeta(),
				Spec:       *claim.Spec.DeepCopy(),
			}

			return pvc
		}
	}

	return nil
}

// Get GetVolumeClaimTemplateNames returns all template names defined.
func (cs *ClusterSpec) GetVolumeClaimTemplateNames() []string {
	names := []string{}

	for _, template := range cs.VolumeClaimTemplates {
		names = append(names, template.ObjectMeta.Name)
	}

	return names
}

// ServerGroupsEnabled returns true if any server config contains server group
// settings or it is defined globally.
func (cs *ClusterSpec) ServerGroupsEnabled() bool {
	for _, setting := range cs.Servers {
		if len(setting.ServerGroups) > 0 {
			return true
		}
	}

	return len(cs.ServerGroups) > 0
}

func (cs *ClusterSpec) GetFSGroup() *int64 {
	if cs.SecurityContext != nil {
		return cs.SecurityContext.FSGroup
	}

	return nil
}

// check whether item exists within array.
func HasItem(itm string, arr []string) (int, bool) {
	for i, a := range arr {
		if a == itm {
			return i, true
		}
	}

	return -1, false
}

// Get the server specification or nil if it doesn't exist.
func (cs *ClusterSpec) GetServerConfigByName(name string) *ServerConfig {
	for _, spec := range cs.Servers {
		if spec.Name == name {
			return &spec
		}
	}

	return nil
}

// Couchbase Image represents the image that will actually be deployed by cluster.
// Defaults to Spec.Image unless image is provided by operator environment variable
// and environment image precedence is also enabled.
func (cs *ClusterSpec) CouchbaseImage() string {
	image := cs.Image

	if annotatedImage, ok := os.LookupEnv(constants.EnvCouchbaseImageName); ok {
		if cs.EnvImagePrecedence && annotatedImage != "" {
			image = annotatedImage
		}
	}

	return image
}

// ServerClassCouchbaseImage selects the image to use for a server class. The priority
// order is:
// * Operator Environment image if EnvImagePrecedence is set
// * Server class image
// * Cluster image.
func (cs *ClusterSpec) ServerClassCouchbaseImage(server *ServerConfig) string {
	if annotatedImage, ok := os.LookupEnv(constants.EnvCouchbaseImageName); ok {
		if cs.EnvImagePrecedence && annotatedImage != "" {
			return annotatedImage
		}
	}

	if server != nil {
		if classImage := server.Image; classImage != "" {
			return classImage
		}
	}

	return cs.Image
}

func (cs *ClusterSpec) getInUseCouchbaseImages() ([]string, error) {
	if annotatedImage, ok := os.LookupEnv(constants.EnvCouchbaseImageName); ok {
		if cs.EnvImagePrecedence && annotatedImage != "" {
			return []string{annotatedImage}, nil
		}
	}

	clusterImage := cs.Image

	serverConfImage := ""
	allServerClassesOverriding := true

	for _, serverConf := range cs.Servers {
		image := serverConf.Image

		if image == "" || image == clusterImage {
			allServerClassesOverriding = false
			continue
		}

		if serverConfImage == "" {
			serverConfImage = image
			continue
		}

		if serverConfImage != image {
			return []string{""}, errors.ErrTooManyServerImages
		}
	}

	if serverConfImage == "" {
		return []string{clusterImage}, nil
	}

	if allServerClassesOverriding {
		return []string{serverConfImage}, nil
	}

	return []string{clusterImage, serverConfImage}, nil
}

// LowestInUseCouchbaseVersionImage will get the lowest version couchbase image in the cluster
// that is in use. Operator Environment image takes the highest priority
// If all server classes have their image set to a higher version image than the cluster image
// then the higher server class image will be returned as the cluster image isn't in use.
func (cs *ClusterSpec) LowestInUseCouchbaseVersionImage() (string, error) {
	images, err := cs.getInUseCouchbaseImages()
	if err != nil {
		return "", err
	}

	switch len(images) {
	case 1:
		return images[0], nil
	case 2:
		break
	default:
		return "", errors.NewStackTracedError(fmt.Errorf("%w: got more images from cluster spec than expected", errors.ErrInternalError))
	}

	image1, image2 := images[0], images[1]

	image1Version, err := couchbaseutil.NewVersionFromImage(image1)
	if err != nil {
		return "", err
	}

	image2Version, err := couchbaseutil.NewVersionFromImage(image2)
	if err != nil {
		return "", err
	}

	if image1Version.Less(image2Version) {
		return image1, nil
	}

	return image2, nil
}

// HighestInUseCouchbaseVersionImage does the inverse of LowestInUseCouchbaseVersionImage.
func (cs *ClusterSpec) HighestInUseCouchbaseVersionImage() (string, error) {
	images, err := cs.getInUseCouchbaseImages()
	if err != nil {
		return "", err
	}

	switch len(images) {
	case 1:
		return images[0], nil
	case 2:
		break
	default:
		return "", errors.NewStackTracedError(fmt.Errorf("%w: got more images from cluster spec than expected", errors.ErrInternalError))
	}

	image1, image2 := images[0], images[1]

	image1Version, err := couchbaseutil.NewVersionFromImage(image1)
	if err != nil {
		return "", err
	}

	image2Version, err := couchbaseutil.NewVersionFromImage(image2)
	if err != nil {
		return "", err
	}

	if image1Version.Less(image2Version) {
		return image2, nil
	}

	return image1, nil
}

// Backup Image represents the image to use for backup.
// defaults to Backup.Image when provided then falls back to
// relatedImage env variable.
func (cs *ClusterSpec) BackupImage() string {
	image := cs.Backup.Image

	if annotatedImage, ok := os.LookupEnv(constants.EnvBackupImageName); ok {
		if cs.EnvImagePrecedence && annotatedImage != "" {
			image = annotatedImage
		}
	}

	return image
}

// ConfigHasDataService returns whether server config specifies data service.
func (cs *ClusterSpec) ConfigHasDataService(name string) bool {
	if config := cs.GetServerConfigByName(name); config != nil {
		for _, service := range config.Services {
			if service == DataService {
				return true
			}
		}
	}

	return false
}

// ConfigHasStatefulService returns whether server config specifies data or index service.
func (cs *ClusterSpec) ConfigHasStatefulService(name string) bool {
	if config := cs.GetServerConfigByName(name); config != nil {
		for _, service := range config.Services {
			if service == DataService || service == IndexService {
				return true
			}
		}
	}

	return false
}

// Monitoring Image represents the image to use for metrics exporter for monitoring.
// defaults to Spec.Image when provided then falls back to relatedImage env variable.
func (cs *ClusterSpec) MetricsImage() string {
	image := cs.Monitoring.Prometheus.Image

	if annotatedImage, ok := os.LookupEnv(constants.EnvMetricsImageName); ok {
		if cs.EnvImagePrecedence && annotatedImage != "" {
			image = annotatedImage
		}
	}

	return image
}

// CloudNativeGatewayImage represents the image to use for Cloud Native Gateway to access CB cluster.
// defaults to Spec.Networking.CloudNativeGateway.Image when provided then falls back to relatedImage env variable.
func (cs *ClusterSpec) CloudNativeGatewayImage() string {
	image := cs.Networking.CloudNativeGateway.Image

	if annotatedImage, ok := os.LookupEnv(constants.EnvCloudNativeGatewayImageName); ok {
		if cs.EnvImagePrecedence && annotatedImage != "" {
			image = annotatedImage
		}
	}

	return image
}

// get list of items which are in first array but not in second.
func MissingItems(a1, a2 []string) []string {
	missingItems := []string{}

	for _, a := range a1 {
		// checking if item from a1 is missing from a2
		if _, ok := HasItem(a, a2); !ok {
			// add to missing
			missingItems = append(missingItems, a)
		}
	}

	return missingItems
}

// StringSlice strips an IPv4 prefix list of type.
func (l IPV4PrefixList) StringSlice() []string {
	s := make([]string, len(l))
	for i := range l {
		s[i] = string(l[i])
	}

	return s
}

// StringSlice strips an exposed feature list of type.
func (l ExposedFeatureList) StringSlice() []string {
	s := make([]string, len(l))
	for i := range l {
		s[i] = string(l[i])
	}

	return s
}

// HasExposedFeatures returns whether we need to expose ports and update the
// alternate addresses in server.
func (cs *ClusterSpec) HasExposedFeatures() bool {
	return len(cs.Networking.ExposedFeatures) != 0
}

// IsExposedFeatureServiceTypePublic returns whether exposed ports will be public and
// therefore need to be TLS protected and may have DDNS entries created.
func (cs *ClusterSpec) IsExposedFeatureServiceTypePublic() bool {
	if cs.Networking.ExposedFeatureServiceType == v1.ServiceTypeLoadBalancer {
		return true
	}

	if cs.Networking.ExposedFeatureServiceTemplate != nil && cs.Networking.ExposedFeatureServiceTemplate.Spec != nil && cs.Networking.ExposedFeatureServiceTemplate.Spec.Type == v1.ServiceTypeLoadBalancer {
		return true
	}

	return false
}

// IsAdminConsoleServiceTypePublic returns whether exposed ports will be public and
// therefore need to be TLS protected and may have DDNS entries created.
func (cs *ClusterSpec) IsAdminConsoleServiceTypePublic() bool {
	if cs.Networking.AdminConsoleServiceType == v1.ServiceTypeLoadBalancer {
		return true
	}

	if cs.Networking.AdminConsoleServiceTemplate != nil && cs.Networking.AdminConsoleServiceTemplate.Spec != nil && cs.Networking.AdminConsoleServiceTemplate.Spec.Type == v1.ServiceTypeLoadBalancer {
		return true
	}

	return false
}

// IsClientFeatureExposed returns whether client service ports are exposed by the cluster.
func (cs *ClusterSpec) IsClientFeatureExposed() bool {
	for _, feature := range cs.Networking.ExposedFeatures {
		if feature == FeatureClient {
			return true
		}
	}

	return false
}

func (c *CouchbaseCluster) IsTLSEnabled() bool {
	return c.Spec.Networking.TLS != nil && (c.Spec.Networking.TLS.Static != nil || c.Spec.Networking.TLS.SecretSource != nil)
}

func (c *CouchbaseCluster) IsMutualTLSEnabled() bool {
	return c.IsTLSEnabled() && c.Spec.Networking.TLS.ClientCertificatePolicy != nil
}

func (c *CouchbaseCluster) IsMandatoryMutualTLSEnabled() bool {
	return c.IsMutualTLSEnabled() && *c.Spec.Networking.TLS.ClientCertificatePolicy == ClientCertificatePolicyMandatory
}

func (c *CouchbaseCluster) IsTLSShadowed() bool {
	return c.IsTLSEnabled() && c.Spec.Networking.TLS.SecretSource != nil
}

func (c *CouchbaseCluster) IsTLSScriptPassphraseEnabled() bool {
	return c.IsTLSEnabled() && c.Spec.Networking.TLS.PassphraseConfig.Script != nil
}

func (c *CouchbaseCluster) IsTLSRestPassphraseEnabled() bool {
	return c.IsTLSEnabled() && c.Spec.Networking.TLS.PassphraseConfig.Rest != nil
}

func (c *CouchbaseCluster) IsServerLoggingEnabled() bool {
	return c.Spec.Logging.Server != nil && c.Spec.Logging.Server.Enabled
}

func (c *CouchbaseCluster) IsAuditLoggingEnabled() bool {
	return c.Spec.Logging.Audit != nil && c.Spec.Logging.Audit.Enabled
}

func (c *CouchbaseCluster) IsAuditGarbageCollectionEnabled() bool {
	return c.IsAuditLoggingEnabled() && c.Spec.Logging.Audit.GarbageCollection != nil
}

func (c *CouchbaseCluster) IsAuditGarbageCollectionSidecarEnabled() bool {
	return c.IsAuditGarbageCollectionEnabled() && c.Spec.Logging.Audit.GarbageCollection.Sidecar != nil && c.Spec.Logging.Audit.GarbageCollection.Sidecar.Enabled
}

func (c *CouchbaseCluster) IsNativeAuditCleanupEnabled() bool {
	return c.IsAuditLoggingEnabled() && c.Spec.Logging.Audit.Rotation != nil &&
		c.Spec.Logging.Audit.Rotation.PruneAge != nil &&
		int(c.Spec.Logging.Audit.Rotation.PruneAge.Duration.Seconds()) > 0
}

// IsIndexerEnabled tells us whether any server class is running the index service.
// This is useful as the storage mode cannot be changed on the fly.
func (c *CouchbaseCluster) IsIndexerEnabled() bool {
	for _, class := range c.Spec.Servers {
		if ServiceList(class.Services).Contains(IndexService) {
			return true
		}
	}

	return false
}

// IsSupportable tells us whether we can realistically support this cluster.
// This means that all server classes use the required volume mounts to preserve
// both data and logs.
func (c *CouchbaseCluster) IsSupportable() bool {
	for _, class := range c.Spec.Servers {
		if !class.IsSupportable() {
			return false
		}
	}

	return true
}

// AnySupportable tells us whether any classes are supportable, and potentially whether
// we should enforce them all being so.
func (c *CouchbaseCluster) AnySupportable() bool {
	for _, class := range c.Spec.Servers {
		if class.IsSupportable() {
			return true
		}
	}

	return false
}

// IndexStorageMode returns the correct index storage setting for the cluster,
// taking into account precedence and deprecated fields.  Both of these fields
// have an API provided default.
func (c *CouchbaseCluster) IndexStorageMode() CouchbaseClusterIndexStorageSetting {
	if c.Spec.ClusterSettings.Indexer != nil {
		return c.Spec.ClusterSettings.Indexer.StorageMode
	}

	return c.Spec.ClusterSettings.IndexStorageSetting
}

// GetDefaultBucketStorageBackend gets the default storage backend if it is set,
// otherwise it returns the default default (I know), Couchstore (for now).
func (c *CouchbaseCluster) GetDefaultBucketStorageBackend() CouchbaseStorageBackend {
	if c.Spec.Buckets.DefaultStorageBackend != "" {
		return c.Spec.Buckets.DefaultStorageBackend
	}

	return CouchbaseStorageBackend(constants.DefaultBucketStorageBackend)
}

// IsSupportable tells us whether a specific server class is supportable, it must
// have volume mounts, the default claim, or the logs claim if all enabled services
// are stateless.
func (sc *ServerConfig) IsSupportable() bool {
	if !sc.VolumeMounts.HasVolumeMounts() {
		return false
	}

	if sc.VolumeMounts.HasDefaultMount() {
		return true
	}

	// Note that due to history, we consider full-text search stateless, it
	// in fact shares the indexer's storage for indexes.  So while we allow
	// it to use log volumes (and no one uses them anyway...) we recommend
	// that they use the index mount.
	statefulServices := []Service{
		DataService,
		IndexService,
		AnalyticsService,
	}

	if sc.VolumeMounts.LogsOnly() && !ServiceList(sc.Services).ContainsAny(statefulServices...) {
		return true
	}

	return false
}

// NodeMatchesConfig check if the given node can be classified in this ServerConfig.
func (sc *ServerConfig) NodeMatchesConfig(n couchbaseutil.NodeInfo) bool {
	// Check if the node has the correct services
	configServices, err := SpecServicesListToServerServiceList(sc.Services)
	if err != nil {
		log.Error(err, "failed to parse services from node")
		return false
	}

	servicesMatch := true

	for _, nodeService := range n.Services {
		if !util.Contains(configServices, couchbaseutil.ServiceName(nodeService)) {
			servicesMatch = false
			break
		}
	}

	return servicesMatch
}

// Set ready members from list.
func (ms *MembersStatus) SetReady(ready []string) {
	ms.Ready = nil

	sort.Strings(ready)

	for _, name := range ready {
		ms.Ready = append(ms.Ready, name)
	}
}

// Set Unready members from list.
func (ms *MembersStatus) SetUnready(unready []string) {
	ms.Unready = nil

	sort.Strings(unready)

	for _, name := range unready {
		ms.Unready = append(ms.Unready, name)
	}
}

func (cs *ClusterStatus) SetVersion(v string) {
	cs.CurrentVersion = v
}

func (cs *ClusterStatus) SetClusterID(uuid string) {
	cs.ClusterID = uuid
}

func (cs *ClusterStatus) PauseControl() {
	cs.ControlPaused = true
}

func (cs *ClusterStatus) Control() {
	cs.ControlPaused = false
}

type ScalingMessage struct {
	Server string
	From   int
	To     int
}

type ScalingMessageList []ScalingMessage

func (sml *ScalingMessageList) BuildMessage() string {
	var builtMessages []string

	for _, message := range *sml {
		builtMessage := fmt.Sprintf("Scaling Server Class %s from %d to %d", message.Server, message.From, message.To)
		builtMessages = append(builtMessages, builtMessage)
	}

	return strings.Join(builtMessages, ", ")
}

func (cs *ClusterStatus) SetScalingCondition() {
	c := newClusterCondition(ClusterConditionScaling, v1.ConditionTrue, "ClusterScaling", "The operator is attempting to scale the cluster")
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) SetScalingUpCondition(msg string) {
	cs.SetScalingCondition()

	c := newClusterCondition(ClusterConditionScalingUp, v1.ConditionTrue, "ScalingUp", msg)
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) SetScalingDownCondition(msg string) {
	cs.SetScalingCondition()

	c := newClusterCondition(ClusterConditionScalingDown, v1.ConditionTrue, "ScalingDown", msg)
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) SetRebalancingCondition() {
	c := newClusterCondition(ClusterConditionRebalancing, v1.ConditionTrue, "Rebalancing", "Rebalancing the cluster")
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) SetBalancedCondition() {
	c := newClusterCondition(ClusterConditionBalanced, v1.ConditionTrue, "Balanced",
		"Data is equally distributed across all nodes in the cluster")
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) SetUnbalancedCondition() {
	c := newClusterCondition(ClusterConditionBalanced, v1.ConditionFalse, "Unbalanced",
		"The operator is attempting to rebalance the data to correct this issue")
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) SetUnknownBalancedCondition() {
	c := newClusterCondition(ClusterConditionBalanced, v1.ConditionUnknown,
		"UnknownBalance", "Unable to determine if cluster is balanced")
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) SetCreatingCondition() {
	c := newClusterCondition(ClusterConditionAvailable, v1.ConditionFalse, "Creating", "The cluster is being created")
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) SetUnavailableCondition(down []string) {
	c := newClusterCondition(ClusterConditionAvailable, v1.ConditionFalse, "PartiallyAvailable",
		fmt.Sprintf("The following nodes are down and not serving requests: %s", strings.Join(down, ", ")))
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) SetReadyCondition() {
	c := newClusterCondition(ClusterConditionAvailable, v1.ConditionTrue, "Available", "")
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) SetConfigRejectedCondition(message string) {
	c := newClusterCondition(ClusterConditionManageConfig, v1.ConditionFalse, "ConfigRejected", message)
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) SetUpgradingCondition(status *UpgradeStatus) {
	c := newClusterCondition(ClusterConditionUpgrading, v1.ConditionTrue, "Upgrading", status.Format())
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) SetEnteringHibernatingCondition(message string) {
	c := newClusterCondition(ClusterConditionHibernating, v1.ConditionFalse, "Hibernating", message)
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) SetHibernatingCondition(message string) {
	c := newClusterCondition(ClusterConditionHibernating, v1.ConditionTrue, "Hibernating", message)
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) SetErrorCondition(message string) {
	c := newClusterCondition(ClusterConditionError, v1.ConditionTrue, "ErrorEncountered", message)
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) SetUnreconcilableCondition(message string) {
	c := newClusterCondition(ClusterUnreconcilable, v1.ConditionTrue, "Unreconcilable", message)
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) SetAutoscalerReadyCondition(message string) {
	c := newClusterCondition(ClusterConditionAutoscaleReady, v1.ConditionTrue, "AutoscaleReady", message)
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) SetAutoscalerUnreadyCondition(message string) {
	c := newClusterCondition(ClusterConditionAutoscaleReady, v1.ConditionFalse, "AutoscalePaused", message)
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) SetSynchronizedCondition() {
	c := newClusterCondition(ClusterConditionSynchronized, v1.ConditionTrue, "SynchronizationComplete", "Data topology synchronized and ready to be managed")
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) SetSynchronizationFailedCondition() {
	c := newClusterCondition(ClusterConditionSynchronized, v1.ConditionFalse, "SynchronizationFailed", "Data topology synchronization failed, enabling management unsafe and may delete data")
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) SetWaitingBetweenMigrations() {
	c := newClusterCondition(ClusterConditionWaitingBetweenMigrations, v1.ConditionTrue, "Waiting", "Waiting before starting next migration")
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) SetNotWaitingBetweenMigrations() {
	c := newClusterCondition(ClusterConditionWaitingBetweenMigrations, v1.ConditionFalse, "Waiting", "Waiting before starting next migration")
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) SetMigratingCondition() {
	c := newClusterCondition(ClusterConditionMigrating, v1.ConditionTrue, "Migrating", "Migrating unmanaged cluster")
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) SetExpandingVolumeCondition() {
	c := newClusterCondition(ClusterConditionExpandingVolume, v1.ConditionTrue, "ExpandingVolume", "Expanding volume")
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) SetBucketMigrationCondition() {
	c := newClusterCondition(ClusterConditionBucketMigration, v1.ConditionTrue, "BucketMigration", "Migrating buckets")
	cs.setClusterCondition(c)
}

func (cs *ClusterStatus) ClearCondition(t ClusterConditionType) {
	for index, condition := range cs.Conditions {
		if condition.Type == t {
			cs.Conditions = append(cs.Conditions[:index], cs.Conditions[index+1:]...)
			break
		}
	}
}

func (cs *ClusterStatus) GetCondition(t ClusterConditionType) *ClusterCondition {
	for index := range cs.Conditions {
		if cs.Conditions[index].Type == t {
			return &cs.Conditions[index]
		}
	}

	return nil
}

func (cs *ClusterStatus) setClusterCondition(c *ClusterCondition) {
	for index, condition := range cs.Conditions {
		if condition.Type == c.Type {
			// Only update the transition time on an status edge trigger.
			if condition.Status != c.Status {
				cs.Conditions[index].Status = c.Status
				cs.Conditions[index].LastTransitionTime = c.LastTransitionTime
				cs.Conditions[index].LastUpdateTime = c.LastUpdateTime
			}

			// If the message or reason have changed, then update the update time only
			if condition.Message != c.Message || condition.Reason != c.Reason {
				cs.Conditions[index].Message = c.Message
				cs.Conditions[index].Reason = c.Reason
				cs.Conditions[index].LastUpdateTime = c.LastUpdateTime
			}

			return
		}
	}

	cs.Conditions = append(cs.Conditions, *c)
}

func newClusterCondition(t ClusterConditionType, status v1.ConditionStatus, reason, message string) *ClusterCondition {
	now := time.Now().Format(time.RFC3339)

	return &ClusterCondition{
		Type:               t,
		Status:             status,
		LastUpdateTime:     now,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}
}

// Format creates an upgrade condition message.
func (status *UpgradeStatus) Format() string {
	return fmt.Sprintf(UpgradingMessageFormat, status.TargetCount, status.TotalCount)
}

// clusterRoles apply cluster wide and don't require a bucket.
var clusterRoles = []RoleName{
	RoleFullAdmin,
	RoleClusterAdmin,
	RoleSecurityAdmin,
	RoleReadOnlyAdmin,
	RoleXDCRAdmin,
	RoleQueryCurlAccess,
	RoleQuestySystemAccess,
	RoleAnalyticsReader,
	RoleSecurityAdminExternal,
	RoleSecurityAdminLocal,
	RoleBackupAdmin,
	RoleQueryManageGlobalFunctions,
	RoleQueryExecuteGlobalFunctions,
	RoleQueryManageGlobalExternalFunctions,
	RoleQueryExecuteGlobalExternalFunctions,
	RoleAnalyticsAdmin,
	RoleExternalStatsReader,
	RoleEventingAdmin,
	RoleEventingManageFunctions,
	RoleSyncDevOps,
}

// bucketRoles can be bucket scoped.
var bucketRoles = []RoleName{
	RoleBucketAdmin,
	RoleSearchAdmin,
	RoleApplicationAccess,
	RoleBackup,
	RoleXDCRInbound,
	RoleAnalyticsManager,
	RoleViewsAdmin,
	RoleViewsReader,
	RoleSyncGateway,
}

// scopeRoles can be bucket + scope scoped.
var scopeRoles = []RoleName{
	RoleScopeAdmin,
	RoleQueryManageFunctions,
	RoleQueryExecuteFunctions,
	RoleQueryManageExternalFunctions,
	RoleQueryExecuteExternalFunctions,
	RoleQueryUseSequences,
	RoleQueryManageSequences,
}

// collectionRoles can be bucket + scope + collection scoped.
var collectionRoles = []RoleName{
	RoleDataReader,
	RoleDataWriter,
	RoleDCPReader,
	RoleMonitor,
	RoleQuerySelect,
	RoleQueryUpdate,
	RoleQueryInsert,
	RoleQueryDelete,
	RoleQueryManageIndex,
	RoleSearchReader,
	RoleAnalyticsSelect,
	RoleSyncGatewayApplication,
	RoleSyncGatewayApplicationReadOnly,
	RoleSyncGatewayArchitect,
	RoleSyncReplicator,
	RoleQueryUseSequentialScans,
}

func ValidRolePattern() string {
	patterns := []string{}

	for _, role := range clusterRoles {
		patterns = append(patterns, fmt.Sprintf("^%v$", role))
	}

	for _, role := range bucketRoles {
		patterns = append(patterns, fmt.Sprintf("^%v$", role))
	}

	return strings.Join(patterns, "|")
}

func IsCollectionRole(role RoleName) bool {
	for _, r := range collectionRoles {
		if r == role {
			return true
		}
	}

	return false
}

func IsScopeRole(role RoleName) bool {
	for _, r := range scopeRoles {
		if r == role {
			return true
		}
	}

	return IsCollectionRole(role)
}

func IsBucketRole(role RoleName) bool {
	for _, r := range bucketRoles {
		if r == role {
			return true
		}
	}

	return IsScopeRole(role)
}

func IsClusterRole(role RoleName) bool {
	for _, r := range clusterRoles {
		if r == role {
			return true
		}
	}

	return false
}

// NamespacedName returns a canonical and unique cluster name for logging.
func (c *CouchbaseCluster) NamespacedName() string {
	return types.NamespacedName{Namespace: c.Namespace, Name: c.Name}.String()
}

// GetRecoveryPolicy returns the user provided recovery policy or a safe default if
// none is specified.
func (c *CouchbaseCluster) GetRecoveryPolicy() RecoveryPolicy {
	if c.Spec.RecoveryPolicy == nil {
		return PrioritizeDataIntegrity
	}

	return *c.Spec.RecoveryPolicy
}

// GetUpgradeProcess returns the user provided upgrade process or default value if
// none is specified.
func (c *CouchbaseCluster) GetUpgradeProcess() UpgradeProcess {
	var upgradeProcess UpgradeProcess

	if c.Spec.Upgrade != nil {
		upgradeProcess = c.Spec.Upgrade.UpgradeProcess
	} else if c.Spec.UpgradeProcess != nil {
		upgradeProcess = *c.Spec.UpgradeProcess
	}

	if upgradeProcess == "" {
		upgradeProcess = SwapRebalance
	}

	return upgradeProcess
}

// GetUpgradeStrategy returns the user provided upgrade strategy or a safe default if
// none is specified.
func (c *CouchbaseCluster) GetUpgradeStrategy() UpgradeStrategy {
	if c.Spec.UpgradeStrategy == nil {
		return RollingUpgrade
	}

	return *c.Spec.UpgradeStrategy
}

// GetHibernationStrategy return the user provided hibernation strategy or a safe
// default if none specified.
func (c *CouchbaseCluster) GetHibernationStrategy() HibernationStrategy {
	if c.Spec.HibernationStrategy == nil {
		return ImmediateHibernation
	}

	return *c.Spec.HibernationStrategy
}

// GetBucketLabelSelector returns a label selector to select buckets for
// inclusion in the cluster.
func (c *CouchbaseCluster) GetBucketLabelSelector() (labels.Selector, error) {
	if c.Spec.Buckets.Selector == nil {
		return labels.Everything(), nil
	}

	return metav1.LabelSelectorAsSelector(c.Spec.Buckets.Selector)
}

// AddressFamily returns the cluster address family, defaulting to IPv4 if
// none is specified.
func (c *CouchbaseCluster) AddressFamily() AddressFamily {
	af := AFInet

	if c.Spec.Networking.AddressFamily != nil {
		af = *c.Spec.Networking.AddressFamily
	}

	return af
}

// DualStack returns true if the address family is not explicitly stated.
func (c *CouchbaseCluster) DualStack() bool {
	return c.Spec.Networking.AddressFamily == nil
}

func (c *CouchbaseCluster) GetBackupStoreEndpoint() *ObjectEndpoint {
	return c.Spec.Backup.ObjectEndpoint
}

// Checks cluster version is above minimum version requirement.
func (c *CouchbaseCluster) IsAtLeastVersion(v string) (bool, error) {
	lowestImage, err := c.Spec.LowestInUseCouchbaseVersionImage()
	if err != nil {
		return false, err
	}

	tag, err := couchbaseutil.CouchbaseImageVersion(lowestImage)
	if err != nil {
		return false, err
	}

	available, err := couchbaseutil.VersionAfter(tag, v)
	if err != nil {
		return false, err
	}

	return available, nil
}

// Checks cluster version is above minimum version requirement.
func (c *CouchbaseCluster) HighestIsAtLeastVersion(v string) (bool, error) {
	image, err := c.Spec.HighestInUseCouchbaseVersionImage()
	if err != nil {
		return false, err
	}

	tag, err := couchbaseutil.CouchbaseImageVersion(image)
	if err != nil {
		return false, err
	}

	available, err := couchbaseutil.VersionAfter(tag, v)
	if err != nil {
		return false, err
	}

	return available, nil
}

// GetMinimumDurability returns a safe default for the bucket durability, because it's
// always set to something, it allows the feature to be disabled when posted to the API.
func (b *CouchbaseBucket) GetMinimumDurability() CouchbaseBucketMinimumDurability {
	if b.Spec.MinimumDurability != "" {
		return b.Spec.MinimumDurability
	}

	return CouchbaseBucketMinimumDurabilityNone
}

// GetMinimumDurability returns a safe default for the bucket durability, because it's
// always set to something, it allows the feature to be disabled when posted to the API.
func (b *CouchbaseEphemeralBucket) GetMinimumDurability() CouchbaseEphemeralBucketMinimumDurability {
	if b.Spec.MinimumDurability != "" {
		return b.Spec.MinimumDurability
	}

	return CouchbaseEphemeralBucketMinimumDurabilityNone
}

type BucketType string

const (
	BucketTypeCouchbase = "couchbase"
	BucketTypeEphemeral = "ephemeral"
	BucketTypeMemcached = "memcached"
)

// AbstractBucket give a bit of commonality to buckets!!
type AbstractBucket interface {
	// GetCouchbaseName returns the Couchbase bucket name, either from the metadata
	// or overridden by the spec name (which is more flexible and not tied
	// to DNS names).
	// ACHTUNG! You cannot add a GetName() receiver to a raw type as it will override
	// that provided by Kubernetes and break cache indexes and numerous other subtle things.
	GetCouchbaseName() string

	// GetLabels returns any metadata labels.
	GetLabels() map[string]string

	// SetLabels sets the metadata labels.
	SetLabels(map[string]string)

	// GetMemoryQuota simply returns the buckets resource allocation.
	GetMemoryQuota() *resource.Quantity

	// GetType returns the bucket type.
	GetType() BucketType

	// GetScopes gets the scopes and collections specification.
	GetScopes() *ScopeSelector

	// AddScopeResource appends a reference to a scope resource.
	AddScopeResource(ScopeLocalObjectReference)

	// IsSampleBucket() returns true if the bucket is a sample bucket.
	IsSampleBucket() bool

	// HasCrossClusterVersioningEnabled returns true if the bucket has cross cluster versioning enabled.
	HasCrossClusterVersioningEnabled() bool
}

func (b *CouchbaseBucket) GetCouchbaseName() string {
	name := b.Name

	if b.Spec.Name != "" {
		name = string(b.Spec.Name)
	}

	return name
}

func (b *CouchbaseBucket) GetLabels() map[string]string {
	return b.Labels
}

func (b *CouchbaseBucket) SetLabels(l map[string]string) {
	b.Labels = l
}

func (b *CouchbaseBucket) GetMemoryQuota() *resource.Quantity {
	return b.Spec.MemoryQuota
}

func (b *CouchbaseBucket) GetType() BucketType {
	return BucketTypeCouchbase
}

func (b *CouchbaseBucket) GetScopes() *ScopeSelector {
	return b.Spec.Scopes
}

func (b *CouchbaseBucket) AddScopeResource(resource ScopeLocalObjectReference) {
	b.Spec.Scopes.Resources = append(b.Spec.Scopes.Resources, resource)
}

func (b *CouchbaseBucket) GetStorageBackend(cluster *CouchbaseCluster) CouchbaseStorageBackend {
	// If there is no cluster then we can't check if cluster version supports the backend,
	// or get the cluster default so we trust the bucket and get the default default
	if cluster == nil {
		return b.getStorageBackend()
	}

	magmaSupported, err := cluster.IsAtLeastVersion("7.1.0")

	if err != nil {
		// we couldn't parse the cluster version so we'll trust the bucket/get default default
		log.Error(err, "failed to get cluster version, using default storage backend", "cluster", cluster.Name, "default-storage-backend", constants.DefaultBucketStorageBackend)
		return b.getStorageBackend()
	}

	if !magmaSupported || b.IsSampleBucket() {
		return CouchbaseStorageBackendCouchstore
	}

	if b.Spec.StorageBackend == "" {
		return cluster.GetDefaultBucketStorageBackend()
	}

	return b.Spec.StorageBackend
}

func (b *CouchbaseBucket) getStorageBackend() CouchbaseStorageBackend {
	if b.Spec.StorageBackend == "" {
		return CouchbaseStorageBackend(constants.DefaultBucketStorageBackend)
	}

	return b.Spec.StorageBackend
}

func (b *CouchbaseBucket) IsSampleBucket() bool {
	return b.Spec.SampleBucket
}

func (b *CouchbaseBucket) HasCrossClusterVersioningEnabled() bool {
	if err := annotations.Populate(&b.Spec, b.Annotations); err != nil {
		log.Error(err, "failed to populate annotations")
	}

	if b.Spec.EnableCrossClusterVersioning == nil {
		return false
	}

	return *b.Spec.EnableCrossClusterVersioning
}

func (b *CouchbaseEphemeralBucket) GetCouchbaseName() string {
	name := b.Name

	if b.Spec.Name != "" {
		name = string(b.Spec.Name)
	}

	return name
}

func (b *CouchbaseEphemeralBucket) GetLabels() map[string]string {
	return b.Labels
}

func (b *CouchbaseEphemeralBucket) SetLabels(l map[string]string) {
	b.Labels = l
}

func (b *CouchbaseEphemeralBucket) GetMemoryQuota() *resource.Quantity {
	return b.Spec.MemoryQuota
}

func (b *CouchbaseEphemeralBucket) GetType() BucketType {
	return BucketTypeEphemeral
}

func (b *CouchbaseEphemeralBucket) GetScopes() *ScopeSelector {
	return b.Spec.Scopes
}

func (b *CouchbaseEphemeralBucket) AddScopeResource(resource ScopeLocalObjectReference) {
	b.Spec.Scopes.Resources = append(b.Spec.Scopes.Resources, resource)
}

func (b *CouchbaseEphemeralBucket) IsSampleBucket() bool {
	return b.Spec.SampleBucket
}

func (b *CouchbaseEphemeralBucket) HasCrossClusterVersioningEnabled() bool {
	if err := annotations.Populate(&b.Spec, b.Annotations); err != nil {
		log.Error(err, "failed to populate annotations")
	}

	if b.Spec.EnableCrossClusterVersioning == nil {
		return false
	}

	return *b.Spec.EnableCrossClusterVersioning
}

func (b *CouchbaseMemcachedBucket) GetCouchbaseName() string {
	name := b.Name

	if b.Spec.Name != "" {
		name = string(b.Spec.Name)
	}

	return name
}

func (b *CouchbaseMemcachedBucket) GetLabels() map[string]string {
	return b.Labels
}

func (b *CouchbaseMemcachedBucket) SetLabels(l map[string]string) {
	b.Labels = l
}

func (b *CouchbaseMemcachedBucket) GetMemoryQuota() *resource.Quantity {
	return b.Spec.MemoryQuota
}

func (b *CouchbaseMemcachedBucket) GetType() BucketType {
	return BucketTypeMemcached
}

func (b *CouchbaseMemcachedBucket) GetScopes() *ScopeSelector {
	return nil
}

func (b *CouchbaseMemcachedBucket) AddScopeResource(_ ScopeLocalObjectReference) {
}

func (b *CouchbaseMemcachedBucket) IsSampleBucket() bool {
	return b.Spec.SampleBucket
}

func (b *CouchbaseMemcachedBucket) HasCrossClusterVersioningEnabled() bool {
	return false
}

// Abstractions for scopes and collections.
// ACHTUNG! You cannot add a GetName() receiver to a raw type as it will override
// that provided by Kubernetes and break cache indexes and numerous other subtle things.
func (c *CouchbaseCollection) CouchbaseName() string {
	if c.Spec.Name != "" {
		return string(c.Spec.Name)
	}

	return c.Name
}

func (s *CouchbaseScope) CouchbaseName() string {
	if s.Spec.DefaultScope {
		return DefaultScopeOrCollection
	}

	if s.Spec.Name != "" {
		return string(s.Spec.Name)
	}

	return s.Name
}

// CanBeImplied means the resource will be implictly filled in by the operator
// if not explcitly defined.  The operator will inject a default scope, without
// a set of collections, thus they are implicitly unmanaged, and a default
// collection will be preserved.
func (s *CouchbaseScope) CanBeImplied() bool {
	if !s.Spec.DefaultScope {
		return false
	}

	if s.Spec.Collections == nil {
		return true
	}

	if !s.Spec.Collections.Managed {
		return true
	}

	if s.Spec.Collections.Selector != nil || len(s.Spec.Collections.Resources) != 0 {
		return false
	}

	if !s.Spec.Collections.PreserveDefaultCollection {
		return false
	}

	return true
}

// BucketScopeOrCollectionNameWithDefaultsList provides some helpers to convert betwixt types.
type BucketScopeOrCollectionNameWithDefaultsList []BucketScopeOrCollectionNameWithDefaults

// StringSlice converts the typed names to strings.
func (l BucketScopeOrCollectionNameWithDefaultsList) StringSlice() []string {
	s := make([]string, len(l))

	for i, name := range l {
		s[i] = string(name)
	}

	return s
}

// Scope object name as string.
func (s ScopeLocalObjectReference) StrName() string {
	return string(s.Name)
}

// Collection object name as string.
func (c CollectionLocalObjectReference) StrName() string {
	return string(c.Name)
}

// StringSlice returns a list of names as a string slice.
func (l ScopeOrCollectionNameList) StringSlice() []string {
	var out []string

	for _, name := range l {
		out = append(out, string(name))
	}

	return out
}

// HasCloudStore returns if any remote cloud stores are set.
func (b CouchbaseBackup) HasCloudStore() bool {
	return b.Spec.S3Bucket != "" || (b.Spec.ObjectStore != nil && b.Spec.ObjectStore.URI != "")
}

func (r CouchbaseBackupRestore) HasCloudStore() bool {
	return r.Spec.S3Bucket != "" || (r.Spec.ObjectStore != nil && r.Spec.ObjectStore.URI != "")
}

func (c *CouchbaseCluster) IsMigrationCluster() bool {
	return c.Spec.Migration != nil && len(c.Spec.Migration.UnmanagedClusterHost) != 0
}

func (c *CouchbaseCluster) IsExternalMigrationCluster() bool {
	return c.IsMigrationCluster() && c.Spec.Networking.DNS != nil
}

func (s *ClusterAssimilationSpec) GetUnmanagedHostURL() string {
	return fmt.Sprintf("http://%s:8091", s.UnmanagedClusterHost)
}

// SpecServicesListToServerServiceList converts a list of services with CRD names (query, data) to a list of server service names (n1ql, kv ...).
func SpecServicesListToServerServiceList(services ServiceList) (couchbaseutil.ServiceList, error) {
	list := couchbaseutil.ServiceList{}

	for _, svc := range services {
		serviceName, err := couchbaseutil.MapServiceNameToServerServiceName(string(svc))
		if err != nil {
			return list, err
		}

		list = append(list, serviceName)
	}

	return list, nil
}

// ServerServiceListToSpecServiceList maps a server service names (n1ql, kv ...) to a CRD service names (query, data).
func MapServerServiceNameToServiceName(service string) (Service, error) {
	switch service {
	case "kv":
		return DataService, nil
	case "index":
		return IndexService, nil
	case "n1ql":
		return QueryService, nil
	case "fts":
		return SearchService, nil
	case "eventing":
		return EventingService, nil
	case "cbas":
		return AnalyticsService, nil
	default:
		return "", fmt.Errorf("%w: invalid service name: %s", errors.NewStackTracedError(couchbaseutil.ErrInvalidResourceName), service)
	}
}

// ServerServiceListToSpecServiceList converts a list of server service names (n1ql, kv ...) to a list of services with CRD names (query, data).
func ServerServiceListToSpecServiceList(services []string) (ServiceList, error) {
	list := ServiceList{}

	for _, svc := range services {
		serviceName, err := MapServerServiceNameToServiceName(svc)
		if err != nil {
			return list, err
		}

		list = append(list, serviceName)
	}

	return list, nil
}

func (c *CouchbaseCluster) IsReadyToAttemptMigration() bool {
	cond := c.Status.GetCondition(ClusterConditionWaitingBetweenMigrations)

	if cond == nil {
		return true
	}

	return cond.Status != v1.ConditionTrue
}

func (c *CouchbaseCluster) IsMigrating() bool {
	cond := c.Status.GetCondition(ClusterConditionMigrating)

	return (cond != nil && cond.Status == v1.ConditionTrue)
}

func (c *CouchbaseCluster) GetNumberOfDataServiceNodes() int {
	dataServiceNodes := 0

	for _, config := range c.Spec.Servers {
		if ServiceList(config.Services).Contains(DataService) {
			dataServiceNodes += config.Size
		}
	}

	return dataServiceNodes
}

func (l CloudNativeGatewayDataAPIProxyServiceList) StringSlice() []string {
	var out []string

	for _, name := range l {
		out = append(out, string(name))
	}

	return out
}

// canHibernate checks if the cluster be hibernated. If we cannot hibernate, this method should return false and a reason why.
func (c *CouchbaseCluster) CanHibernate() (bool, string) {
	// Check if cluster is migrating
	if c.IsMigrationCluster() {
		return false, "Cluster is a migration cluster"
	}

	if c.HasCondition(ClusterConditionUpgrading) {
		return false, "Cluster is upgrading"
	}

	if c.HasCondition(ClusterConditionBucketMigration) {
		return false, "Cluster is migrating buckets"
	}

	if c.HasCondition(ClusterConditionScaling) {
		return false, "Cluster is scaling"
	}

	if !c.HasCondition(ClusterConditionBalanced) {
		return false, "Cluster is unbalanced"
	}

	if !c.HasCondition(ClusterConditionAvailable) {
		return false, "Cluster is unavailable"
	}

	return true, ""
}

func (c *CouchbaseCluster) IsInIndexMismatchErrorState() bool {
	cond := c.Status.GetCondition(ClusterConditionError)

	return cond != nil && cond.Status == v1.ConditionTrue && cond.Message == errors.ErrIndexStorageModeMismatch.Error()
}

func (c *CouchbaseCluster) HasCondition(condition ClusterConditionType) bool {
	return c.Status.GetCondition(condition) != nil && c.Status.GetCondition(condition).Status == v1.ConditionTrue
}
