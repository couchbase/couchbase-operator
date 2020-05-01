package v1

import (
	"fmt"
	"reflect"

	"github.com/couchbase/gocbmgr"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// CouchbaseClusterList is a list of Couchbase clusters.
type CouchbaseClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseCluster `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type CouchbaseCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ClusterSpec   `json:"spec"`
	Status            ClusterStatus `json:"status"`
}

func (c *CouchbaseCluster) AsOwner() metav1.OwnerReference {
	trueVar := true
	return metav1.OwnerReference{
		APIVersion: SchemeGroupVersion.String(),
		Kind:       CRDResourceKind,
		Name:       c.Name,
		UID:        c.UID,
		Controller: &trueVar,
	}
}

// Supported services
type Service string

const (
	AdminService     Service = "admin"
	DataService      Service = "data"
	IndexService     Service = "index"
	QueryService     Service = "query"
	SearchService    Service = "search"
	EventingService  Service = "eventing"
	AnalyticsService Service = "analytics"
)

// Convert from typed to string
func (s Service) String() string {
	return string(s)
}

type ServiceList []Service

// Len returns the ServiceList length
func (l ServiceList) Len() int {
	return len(l)
}

// Less compares two ServiceList items and returns true if a is less than b
func (l ServiceList) Less(a, b int) bool {
	return l[a].String() < l[b].String()
}

// Swap swaps the position of two ServiceList elements
func (l ServiceList) Swap(a, b int) {
	l[a], l[b] = l[b], l[a]
}

// Contains returns true if a service is part of a service list
func (l ServiceList) Contains(service Service) bool {
	for _, s := range l {
		if s == service {
			return true
		}
	}
	return false
}

// ContainsAny returns true if any service is part of a service list
func (l ServiceList) ContainsAny(services ...Service) bool {
	for _, service := range services {
		if l.Contains(service) {
			return true
		}
	}
	return false
}

// Sub removes members from 'other' from a ServiceList
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

// Convert from a typed array to plain string array
func (l ServiceList) StringSlice() []string {
	slice := make([]string, len(l))
	for i, s := range l {
		slice[i] = s.String()
	}
	return slice
}

// Supported features
const (
	// Exposes the admin port/UI
	FeatureAdmin = "admin"
	// Exposes ports necessary for XDCR
	FeatureXDCR = "xdcr"
	// Exposes all client ports for services
	FeatureClient = "client"
)

var SupportedFeatures = []string{
	FeatureAdmin,
	FeatureXDCR,
	FeatureClient,
}

// A list of exposed features e.g. admin,xdcr
type ExposedFeatureList []string

// Contains returns true if a requested feature is enabled
func (efl ExposedFeatureList) Contains(feature string) bool {
	for _, f := range efl {
		if f == feature {
			return true
		}
	}
	return false
}

// PlatformType defines the platform you are running on which provides explicit
// control over how resources are configured.
type PlatformType string

const (
	PlatformTypeAWS   = "aws"
	PlatformTypeGCE   = "gce"
	PlatformTypeAzure = "azure"
)

type ClusterSpec struct {
	// BaseImage is the base couchbase image name that will be used to launch
	// couchbase clusters. This is useful for private registries, etc.
	BaseImage string `json:"baseImage"`

	// Version is the expected version of the couchbase cluster.
	// The couchbase-operator will eventually make the couchbase cluster version
	// equal to the expected version.
	//
	// The version must follow the [semver]( http://semver.org) format, for
	// example "3.1.8".
	Version string `json:"version,omitempty"`

	// Paused is to pause the control of the operator for the couchbase cluster.
	Paused bool `json:"paused,omitempty"`

	// AntiAffinity determines if the couchbase-operator tries to avoid putting
	// the couchbase members in the same cluster onto the same node.
	AntiAffinity bool `json:"antiAffinity,omitempty"`

	// couchbase cluster TLS configuration
	TLS *TLSPolicy `json:"tls,omitempty"`

	// Cluster specific settings
	ClusterSettings ClusterConfig `json:"cluster"`

	// Bucket specific settings
	BucketSettings []BucketConfig `json:"buckets,omitempty"`

	// A specificaion for the way nodes should be configured in the cluster
	ServerSettings []ServerConfig `json:"servers,omitempty"`

	// AuthSecret is the name of a kube secret to use for authentication
	AuthSecret string `json:"authSecret"`

	// Option to expose admin console
	ExposeAdminConsole bool `json:"exposeAdminConsole"`

	// Specific services to use when exposing ui
	AdminConsoleServices ServiceList `json:"adminConsoleServices,omitempty"`

	// ExposedFeatures is a list of features to expose on the K8S node
	// network.  They represent a subset of ports e.g. admin=8091,
	// xdcr=8091,8092,11210, and thus may overlap.
	ExposedFeatures ExposedFeatureList `json:"exposedFeatures,omitempty"`

	// Enables software update notifications in the UI
	SoftwareUpdateNotifications bool `json:"softwareUpdateNotifications"`

	// VolumeClaimTemplates define the desired characteristics of a volume
	// that can be requested/claimed by a pod.
	// When specified, each claim should map to the name of a volumeMount
	// defined in a PodPolicy
	VolumeClaimTemplates []v1.PersistentVolumeClaim `json:"volumeClaimTemplates,omitempty"`

	// ServerGroups define the set of availability zones we want to distribute
	// pods over.  This allows the Kubernetes cluster administrator to label all
	// nodes, but use a specific subset for a particular Couchbase cluster.
	ServerGroups []string `json:"serverGroups,omitempty"`

	// Security Context for all pods
	SecurityContext *v1.PodSecurityContext `json:"securityContext,omitempty"`

	// DisableBucketManagement tells the reconcile loop to ignore all
	// buckets in the system and leave that entirely to the customer
	DisableBucketManagement bool `json:"disableBucketManagement,omitempty"`

	// LogRetentionTime gives the time to keep persistent log PVCs alive for.
	LogRetentionTime string `json:"logRetentionTime,omitempty"`

	// LogRetentionCount gives the number of persistent log PVCs to keep.
	LogRetentionCount int `json:"logRetentionCount,omitempty"`

	// ExposedFeatureServiceType defines whether to create a NodePort or LoadBalancer service.
	ExposedFeatureServiceType v1.ServiceType `json:"exposedFeatureServiceType,omitempty"`

	// AdminConsoleServiceType defines whether to create a NodePort or LoadBalancer service.
	AdminConsoleServiceType v1.ServiceType `json:"adminConsoleServiceType,omitempty"`

	// ExposedFeatureTrafficPolicy defines how packets should be routed.
	ExposedFeatureTrafficPolicy *v1.ServiceExternalTrafficPolicyType `json:"exposedFeatureTrafficPolicy,omitempty"`

	// DNS points to information for Dynamic DNS support.
	DNS *DNS `json:"dns,omitempty"`

	// Platform gives a hint as to what platform we are running on and how
	// to configure services etc.
	Platform PlatformType `json:"platform,omitempty"`

	// ServiceAnnotations allows services to be annotated with custom labels.
	// Operator annotations are merged on top of these so have precedence as
	// they are required for correct operation.
	ServiceAnnotations map[string]string `json:"serviceAnnotations,omitempty"`
}

// DNS contains information for Dynamic DNS support.
type DNS struct {
	// Domain is the domain to create pods in.
	Domain string `json:"domain,omitempty"`
}

type ClusterConfig struct {
	// The name of the cluster
	ClusterName string `json:"clusterName"`

	// The amount of memory that should be allocated to the data service
	DataServiceMemQuota uint64 `json:"dataServiceMemoryQuota"`

	// The amount of memory that should be allocated to the index service
	IndexServiceMemQuota uint64 `json:"indexServiceMemoryQuota"`

	// The amount of memory that should be allocated to the search service
	SearchServiceMemQuota uint64 `json:"searchServiceMemoryQuota"`

	// The amount of memory that should be allocated to the eventing service
	EventingServiceMemQuota uint64 `json:"eventingServiceMemoryQuota"`

	// The amount of memory that should be allocated to the analytics service
	AnalyticsServiceMemQuota uint64 `json:"analyticsServiceMemoryQuota"`

	// The index storage mode to use for secondary indexing
	IndexStorageSetting string `json:"indexStorageSetting"`

	// Timeout that expires to trigger the auto failover.
	AutoFailoverTimeout uint64 `json:"autoFailoverTimeout"`

	// The number of failover events we can tolerate
	AutoFailoverMaxCount uint64 `json:"autoFailoverMaxCount"`

	// Whether to auto failover if disk issues are detected
	AutoFailoverOnDataDiskIssues bool `json:"autoFailoverOnDataDiskIssues"`

	// How long to wait for transient errors before failing over a faulty disk
	AutoFailoverOnDataDiskIssuesTimePeriod uint64 `json:"autoFailoverOnDataDiskIssuesTimePeriod"`

	// Whether to enable failing over a server group
	AutoFailoverServerGroup bool `json:"autoFailoverServerGroup"`
}

type BucketConfig struct {
	// The bucket name
	BucketName string `json:"name"`

	// The type of bucket to use
	BucketType string `json:"type"`

	// The amount of memory that should be allocated to the bucket
	BucketMemoryQuota int `json:"memoryQuota"`

	// The number of bucket replicates
	BucketReplicas int `json:"replicas,omitempty"`

	// The priority when compared to other buckets
	IoPriority string `json:"ioPriority,omitempty"`

	// The bucket eviction policy which determines behavior during expire and high mem usage
	EvictionPolicy string `json:"evictionPolicy,omitempty"`

	// The bucket's conflict resolution mechanism; which is to be used if a conflict occurs during Cross Data-Center Replication (XDCR). Sequence-based and timestamp-based mechanisms are supported.
	ConflictResolution string `json:"conflictResolution,omitempty"`

	// The enable flush option denotes wether the data in the bucket can be flushed
	EnableFlush bool `json:"enableFlush,omitempty"`

	// Enable Index replica specifies whether or not to enable view index replicas for this bucket. This parameter defaults to false if it is not specified. This parameter only affects Couchbase buckets.
	EnableIndexReplica bool `json:"enableIndexReplica,omitempty"`

	// CompressionMode is the compression mode for the bucket to run in.
	CompressionMode cbmgr.CompressionMode `json:"compressionMode,omitempty"`
}

type ServerConfig struct {
	// Size is the expected size of the couchbase cluster. The
	// couchbase-operator will eventually make the size of the running
	// cluster equal to the expected size. The vaild range of the size is
	// from 1 to 50.
	Size int `json:"size"`

	// A name for the server configuration. It must be unique.
	Name string `json:"name"`

	// The services to run on nodes created with this spec
	Services ServiceList `json:"services"`

	// ServerGroups define the set of availability zones we want to distribute
	// pods over.  This allows the Kubernetes cluster administrator to label all
	// nodes, but use a specific subset for a particular Couchbase cluster.
	ServerGroups []string `json:"serverGroups,omitempty"`

	// Pod defines the policy to create pod for the couchbase pod.
	//
	// Updating Pod does not take effect on any existing couchbase pods.
	Pod *PodPolicy `json:"pod,omitempty"`
}

// PodPolicy defines the policy to create pod for the couchbase container.
type PodPolicy struct {
	// Labels specifies the labels to attach to pods the operator creates for the
	// couchbase cluster.
	// "app" and "couchbase_*" labels are reserved for the internal use of the couchbase operator.
	// Do not overwrite them.
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations allow custom annotations of Couchbase Server pods.  We reserve the right
	// to overwrite anything in the couchbase.com namespace.
	Annotations map[string]string `json:"annotations,omitempty"`

	// NodeSelector specifies a map of key-value pairs. For the pod to be eligible
	// to run on a node, the node must have each of the indicated key-value pairs as
	// labels.
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Resources is the resource requirements for the couchbase container.
	// This field cannot be updated once the cluster is created.
	Resources v1.ResourceRequirements `json:"resources,omitempty"`

	// Tolerations specifies the pod's tolerations.
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`

	// List of environment variables to set in the couchbase container.
	// This is used to configure couchbase process. couchbase cluster cannot be
	// created, when bad environement variables are provided. Do not overwrite
	// any flags used to bootstrap the cluster (for example `--initial-cluster`
	// flag). This field cannot be updated.
	CouchbaseEnv []v1.EnvVar `json:"couchbaseEnv,omitempty"`

	// EnvFrom allows the setting of environment variables from things like
	// Secrets and ConfigMaps.
	EnvFrom []v1.EnvFromSource `json:"envFrom,omitempty"`

	// Volume mounts represent persistent volume claims to attach to pod.
	// If defined new pods will use persistent volumes.
	VolumeMounts *VolumeMounts `json:"volumeMounts,omitempty"`

	// By default, kubernetes will mount a service account token into the couchbase pods.
	// AutomountServiceAccountToken indicates whether pods running with the service account should have an API token automatically mounted.
	AutomountServiceAccountToken *bool `json:"automountServiceAccountToken,omitempty"`

	// ImagePullSecrets allows users to pull Couchbase Server images from private repos.
	ImagePullSecrets []v1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
}

type VolumeMountName string

const (
	DefaultVolumeMount   VolumeMountName = "default"
	DataVolumeMount      VolumeMountName = "data"
	IndexVolumeMount     VolumeMountName = "index"
	AnalyticsVolumeMount VolumeMountName = "analytics"
	LogsVolumeMount      VolumeMountName = "logs"
)

type VolumeMounts struct {
	// Name of claim to use for couchbases default install path
	DefaultClaim string `json:"default,omitempty"`
	// Name of claim to use for index path
	IndexClaim string `json:"index,omitempty"`
	// Name of claim to use for data path
	DataClaim string `json:"data,omitempty"`
	// Name of claims to use for analytics paths
	AnalyticsClaims []string `json:"analytics,omitempty"`
	// Name of claim to use for logs path
	LogsClaim string `json:"logs,omitempty"`
}

// Get all of the volume mounts to be used for analytics service
// as an indexed list mapped to their claims
func (v *VolumeMounts) GetAnalyticsMountClaims() map[string]string {
	mountClaims := make(map[string]string)
	if v.AnalyticsClaims != nil {
		for i, claim := range v.AnalyticsClaims {
			mount := fmt.Sprintf("%s-%02d", AnalyticsVolumeMount, i)
			mountClaims[mount] = claim
		}
	}
	return mountClaims
}

// Get all of the paths which correspond to the mounts to be used
// for analytics service
func (v *VolumeMounts) GetAnalyticsVolumePaths() []string {
	paths := []string{}
	for mount := range v.GetAnalyticsMountClaims() {
		paths = append(paths, fmt.Sprintf("/mnt/%s", mount))
	}
	return paths
}

// LogsOnly returns true if logs will be the only mounts applied to cluster
func (v *VolumeMounts) LogsOnly() bool {
	return v.LogsClaim != ""
}

func (sc *ServerConfig) GetVolumeMounts() *VolumeMounts {
	if sc != nil && sc.Pod != nil {
		return sc.Pod.VolumeMounts
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

func (cs *ClusterSpec) Cleanup() {

}

func (cs *ClusterSpec) TotalSize() int {
	size := 0
	for _, server := range cs.ServerSettings {
		size += server.Size
	}
	return size
}

// list of bucket names from config
func (cs *ClusterSpec) BucketNames() []string {
	buckets := []string{}
	if cs.BucketSettings != nil {
		for _, b := range cs.BucketSettings {
			buckets = append(buckets, b.BucketName)
		}
	}
	return buckets
}

// Get bucket config by name of bucket
func (cs *ClusterSpec) GetBucketByName(name string) *BucketConfig {
	if cs.BucketSettings != nil {
		for _, b := range cs.BucketSettings {
			if b.BucketName == name {
				return &b
			}
		}
	}
	return nil
}

// Get the volumeClaimTemplate with specified name
func (cs *ClusterSpec) GetVolumeClaimTemplate(name string) *v1.PersistentVolumeClaim {
	for _, claim := range cs.VolumeClaimTemplates {
		if name == claim.Name {
			return &claim
		}
	}
	return nil
}

// Get GetVolumeClaimTemplateNames returns all template names defined.
func (cs *ClusterSpec) GetVolumeClaimTemplateNames() []string {
	names := []string{}
	for _, template := range cs.VolumeClaimTemplates {
		names = append(names, template.Name)
	}
	return names
}

// diff spec and existing buckets to determine
// which should be added and which removed
func (cs *ClusterSpec) BucketDiff(existingBuckets []string) ([]string, []string) {
	specBuckets := cs.BucketNames()
	bucketsToAdd := MissingItems(specBuckets, existingBuckets)
	bucketsToRemove := MissingItems(existingBuckets, specBuckets)

	return bucketsToAdd, bucketsToRemove
}

// ServerGroupsEnabled returns true if any server config contains server group
// settings or it is defined globally
func (cs *ClusterSpec) ServerGroupsEnabled() bool {
	for _, setting := range cs.ServerSettings {
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

func (c *BucketConfig) Equals(other *BucketConfig) bool {
	return reflect.DeepEqual(c, other)
}

// check whether item exists within array
func HasItem(itm string, arr []string) (int, bool) {
	for i, a := range arr {
		if a == itm {
			return i, true
		}
	}
	return -1, false
}

// Get the server specification or nil if it doesn't exist
func (cs *ClusterSpec) GetServerConfigByName(name string) *ServerConfig {
	for _, spec := range cs.ServerSettings {
		if spec.Name == name {
			return &spec
		}
	}
	return nil
}

// get list of items which are in first array but not in second
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

// HasExposedFeatures returns whether we need to expose ports and update the
// alternate addresses in server.
func (cs *ClusterSpec) HasExposedFeatures() bool {
	return len(cs.ExposedFeatures) != 0
}

// IsExposedFeatureServiceTypePublic returns whether exposed ports will be public and
// therefore need to be TLS protected and may have DDNS entries created.
func (cs *ClusterSpec) IsExposedFeatureServiceTypePublic() bool {
	return cs.ExposedFeatureServiceType == v1.ServiceTypeLoadBalancer
}

// IsAdminConsoleServiceTypePublic returns whether exposed ports will be public and
// therefore need to be TLS protected and may have DDNS entries created.
func (cs *ClusterSpec) IsAdminConsoleServiceTypePublic() bool {
	return cs.AdminConsoleServiceType == v1.ServiceTypeLoadBalancer
}
