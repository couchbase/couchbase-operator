package v2

import (
	"github.com/couchbase/gocbmgr"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseBucket struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CouchbaseBucketSpec `json:"spec"`
}

type CouchbaseBucketSpec struct {
	MemoryQuota        int                   `json:"memoryQuota,omitempty"`
	Replicas           int                   `json:"replicas,omitempty"`
	IoPriority         string                `json:"ioPriority,omitempty"`
	EvictionPolicy     string                `json:"evictionPolicy,omitempty"`
	ConflictResolution string                `json:"conflictResolution,omitempty"`
	EnableFlush        bool                  `json:"enableFlush,omitempty"`
	EnableIndexReplica bool                  `json:"enableIndexReplica,omitempty"`
	CompressionMode    cbmgr.CompressionMode `json:"compressionMode,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseBucketList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseBucket `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseEphemeralBucket struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CouchbaseEphemeralBucketSpec `json:"spec"`
}

type CouchbaseEphemeralBucketSpec struct {
	MemoryQuota        int                   `json:"memoryQuota,omitempty"`
	Replicas           int                   `json:"replicas,omitempty"`
	IoPriority         string                `json:"ioPriority,omitempty"`
	EvictionPolicy     string                `json:"evictionPolicy,omitempty"`
	ConflictResolution string                `json:"conflictResolution,omitempty"`
	EnableFlush        bool                  `json:"enableFlush,omitempty"`
	CompressionMode    cbmgr.CompressionMode `json:"compressionMode,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseEphemeralBucketList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseEphemeralBucket `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseMemcachedBucket struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CouchbaseMemcachedBucketSpec `json:"spec"`
}

type CouchbaseMemcachedBucketSpec struct {
	MemoryQuota int  `json:"memoryQuota,omitempty"`
	EnableFlush bool `json:"enableFlush,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseMemcachedBucketList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseMemcachedBucket `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseReplication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CouchbaseReplicationSpec `json:"spec"`
}

// CompressionType represents all allowable XDCR compression modes.
type CompressionType string

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseUser struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CouchbaseUserSpec `json:"spec"`
}

type CouchbaseUserSpec struct {
	// (Optional) Full Name of Couchbase user
	FullName string `json:"fullName"`
	// The domain which provides user auth
	AuthDomain string `json:"authDomain"`
	// Name of kubernetes secret with password for couchbase domain
	AuthSecret string `json:"authSecret"`
}

// CouchbaseUserList is a list of Couchbase users.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseUserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseUser `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseRole struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CouchbaseRoleSpec `json:"spec"`
}

type CouchbaseRoleSpec struct {
	// role identifier
	Roles []Role `json:"roles"`
}

type Role struct {
	// name of role
	Name string `json:"name"`
	// optional bucket name for bucket admin roles
	Bucket string `json:"bucket"`
}

// CouchbaseRoleList is a list of Couchbase users.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseRoleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseRole `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseRoleBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CouchbaseRoleBindingSpec `json:"spec"`
}

type CouchbaseRoleBindingSpec struct {
	//  List of users to bind a role to
	Subjects []CouchbaseRoleBindingSubject `json:"subjects"`
	// CouchbaseRole being bound to subjects
	RoleRef CouchbaseRoleBindingRef `json:"roleRef"`
}

type CouchbaseRoleBindingSubject struct {
	// Couchbase user kind
	Kind string `json:"kind"`
	// Name of Couchbase user resource
	Name string `json:"name"`
}

type CouchbaseRoleBindingRef struct {
	// Kind of role to use for binding
	Kind string `json:"kind"`
	// Name of role resource to use for binding
	Name string `json:"name"`
}

// CouchbaseRoleBindingList is a list of Couchbase users.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseRoleBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseRoleBinding `json:"items"`
}

const (
	// CompressionTypeNone applies no compression
	CompressionTypeNone CompressionType = "none"

	// CompressionTypeAuto automatically applies compression based on internal heuristics.
	CompressionTypeAuto CompressionType = "auto"

	// CompressionTypeSnappy applies snappy compression to all documents.
	CompressionTypeSnappy CompressionType = "snappy"
)

type CouchbaseReplicationSpec struct {
	// Bucket is the source bucket to replicate from.  This must be defined and
	// selected by the cluster.
	Bucket string `json:"bucket,omitempty"`

	// RemoteBucket is the remote bucket name to synchronize to.
	RemoteBucket string `json:"remoteBucket,omitempty"`

	// CompressionType is the type of compression to apply to the replication.
	CompressionType CompressionType `json:"compressionType,omitempty"`

	// FilterExpression allows certain documents to be filtered out of the replication.
	FilterExpression string `json:"filterExpression,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CouchbaseReplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CouchbaseReplication `json:"items"`
}

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

type ServiceList []Service

// Supported features
const (
	// Exposes the admin port/UI
	FeatureAdmin = "admin"
	// Exposes ports necessary for XDCR
	FeatureXDCR = "xdcr"
	// Exposes all client ports for services
	FeatureClient = "client"
)

// A list of exposed features e.g. admin,xdcr
type ExposedFeatureList []string

// PlatformType defines the platform you are running on which provides explicit
// control over how resources are configured.
type PlatformType string

const (
	PlatformTypeAWS   PlatformType = "aws"
	PlatformTypeGCE   PlatformType = "gce"
	PlatformTypeAzure PlatformType = "azure"
)

type ClusterSpec struct {
	// BaseImage is the base couchbase image name that will be used to launch
	// couchbase clusters. This is useful for private registries, etc.
	Image string `json:"image"`

	// Paused is to pause the control of the operator for the couchbase cluster.
	Paused bool `json:"paused,omitempty"`

	// AntiAffinity determines if the couchbase-operator tries to avoid putting
	// the couchbase members in the same cluster onto the same node.
	AntiAffinity bool `json:"antiAffinity,omitempty"`

	// Cluster specific settings
	ClusterSettings ClusterConfig `json:"cluster"`

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

	// Platform gives a hint as to what platform we are running on and how
	// to configure services etc.
	Platform PlatformType `json:"platform,omitempty"`

	// Security groups together related security options.
	Security CouchbaseClusterSecuritySpec `json:"security"`

	// Networking groups together related networking options.
	Networking CouchbaseClusterNetworkingSpec `json:"networking"`

	// Logging groups together logging related options.
	Logging CouchbaseClusterLoggingSpec `json:"logging"`

	// Servers specifies
	Servers []ServerConfig `json:"servers,omitempty"`

	// Bucket specific settings
	Buckets Buckets `json:"buckets,omitempty"`

	// XDCR specific settings.
	XDCR XDCR `json:"xdcr"`
}

type CouchbaseClusterSecuritySpec struct {
	// AdminSecret is the name of a kubernetes secret to use for administrator authentication.
	AdminSecret string `json:"adminSecret"`
	// Couchbase RBAC Users
	RBAC RBAC `json:"rbac,omitempty"`
}

type CouchbaseClusterNetworkingSpec struct {
	// ExposeAdminConsole creates a service referencing the admin console.
	ExposeAdminConsole bool `json:"exposeAdminConsole,omitempty"`

	// AdminConsoleServices is a selector to choose specific services to expose via the admin console.
	AdminConsoleServices ServiceList `json:"adminConsoleServices,omitempty"`

	// ExposedFeatures is a list of features to expose on the K8S node
	// network.  They represent a subset of ports e.g. admin=8091,
	// xdcr=8091,8092,11210, and thus may overlap.
	ExposedFeatures ExposedFeatureList `json:"exposedFeatures,omitempty"`

	// TLS contains the TLS configuration for the cluster.
	TLS *TLSPolicy `json:"tls,omitempty"`

	// ExposedFeatureServiceType defines whether to create a NodePort or LoadBalancer service.
	ExposedFeatureServiceType v1.ServiceType `json:"exposedFeatureServiceType,omitempty"`

	// AdminConsoleServiceType defines whether to create a NodePort or LoadBalancer service.
	AdminConsoleServiceType v1.ServiceType `json:"adminConsoleServiceType,omitempty"`

	// DNS points to information for Dynamic DNS support.
	DNS *DNS `json:"dns,omitempty"`
}

type CouchbaseClusterLoggingSpec struct {
	// LogRetentionTime gives the time to keep persistent log PVCs alive for.
	LogRetentionTime string `json:"logRetentionTime,omitempty"`

	// LogRetentionCount gives the number of persistent log PVCs to keep.
	LogRetentionCount int `json:"logRetentionCount,omitempty"`
}

// DNS contains information for Dynamic DNS support.
type DNS struct {
	// Domain is the domain to create pods in.
	Domain string `json:"domain,omitempty"`
}

type ClusterConfig struct {
	// The name of the cluster
	ClusterName string `json:"clusterName,omitempty"`

	// The amount of memory that should be allocated to the data service
	DataServiceMemQuota uint64 `json:"dataServiceMemoryQuota,omitempty"`

	// The amount of memory that should be allocated to the index service
	IndexServiceMemQuota uint64 `json:"indexServiceMemoryQuota,omitempty"`

	// The amount of memory that should be allocated to the search service
	SearchServiceMemQuota uint64 `json:"searchServiceMemoryQuota,omitempty"`

	// The amount of memory that should be allocated to the eventing service
	EventingServiceMemQuota uint64 `json:"eventingServiceMemoryQuota,omitempty"`

	// The amount of memory that should be allocated to the analytics service
	AnalyticsServiceMemQuota uint64 `json:"analyticsServiceMemoryQuota,omitempty"`

	// The index storage mode to use for secondary indexing
	IndexStorageSetting string `json:"indexStorageSetting,omitempty"`

	// Timeout that expires to trigger the auto failover.
	AutoFailoverTimeout uint64 `json:"autoFailoverTimeout,omitempty"`

	// The number of failover events we can tolerate
	AutoFailoverMaxCount uint64 `json:"autoFailoverMaxCount,omitempty"`

	// Whether to auto failover if disk issues are detected
	AutoFailoverOnDataDiskIssues bool `json:"autoFailoverOnDataDiskIssues,omitempty"`

	// How long to wait for transient errors before failing over a faulty disk
	AutoFailoverOnDataDiskIssuesTimePeriod uint64 `json:"autoFailoverOnDataDiskIssuesTimePeriod,omitempty"`

	// Whether to enable failing over a server group
	AutoFailoverServerGroup bool `json:"autoFailoverServerGroup,omitempty"`

	// Auto-compaction settings
	AutoCompaction *AutoCompaction `json:"autoCompaction,omitempty,omitempty"`
}

// DatabaseFragmentationThreshold lists triggers for when database compaction should start.
type DatabaseFragmentationThreshold struct {
	// Percent is the percentage of disk fragmentation (2-100).
	Percent *int `json:"percent,omitempty"`

	// Size is the size of disk framentation.
	Size resource.Quantity `json:"size,omitempty"`
}

// ViewFragmentationThreshold lists triggers for when view compaction should start.
type ViewFragmentationThreshold struct {
	// Percent is the percentage of disk fragmentation (2-100).
	Percent *int `json:"percent,omitempty"`

	// Size is the size of disk framentation.
	Size resource.Quantity `json:"size,omitempty"`
}

// TimeWindow allows the user to restrict when compaction can occur.
type TimeWindow struct {
	// Start is a string in the form HH:MM.
	Start string `json:"start,omitempty"`

	// End is a string in the form HH:MM.
	End string `json:"end,omitempty"`

	// AbortCompactionOutsideWindow stops compaction processes when the
	// process moves outside the window.
	AbortCompactionOutsideWindow bool `json:"abortCompactionOutsideWindow,omitempty"`
}

// AutoCompaction define auto compaction settings.
type AutoCompaction struct {
	// DatabaseFragmentationThreshold lists triggers for when database compaction should start.
	DatabaseFragmentationThreshold DatabaseFragmentationThreshold `json:"databaseFragmentationThreshold,omitempty"`

	// ViewFragmentationThreshold lists triggers for when view compaction should start.
	ViewFragmentationThreshold ViewFragmentationThreshold `json:"viewFragmentationThreshold,omitempty"`

	// ParallelCompaction controls whether database and view compactions can happen
	// in parallel.
	ParallelCompaction bool `json:"parallelCompaction,omitempty"`

	// TimeWindow allows the user to restrict when compaction can occur.
	TimeWindow TimeWindow `json:"timeWindow,omitempty"`

	// TombstonePurgeInterval controls how long to wait before purging tombstones.
	TombstonePurgeInterval metav1.Duration `json:"tombstonePurgeInterval,omitempty"`
}

// Replications defines XDCR replications.
type Replications struct {
	// Selector allows CouchbaseReplication resources to be filtered
	// based on labels.
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

const (
	// RemoteClusterTLSCA is the key in the secret mapping to the remote cluster's
	// expected trust anchor.
	RemoteClusterTLSCA = "ca"

	// RemoteClusterTLSCertificate is the key in the secret mapping to a client
	// certificate signed by the remote cluster.
	RemoteClusterTLSCertificate = "certificate"

	// RemoteClusterTLSKey is the key in the secret mapping to a client key to
	// digitally sign data for validation by remote cluster.
	RemoteClusterTLSKey = "key"
)

// RemoteClusterTLS is a structure that can source TLS certificates from
// different sources.
type RemoteClusterTLS struct {
	// Secret references a secret containing the CA certificate, and optionally a
	// client certificate and key.
	Secret *string `json:"secret"`
}

// RemoteCluster is a reference to a remote cluster for XDCR.
type RemoteCluster struct {
	// Name of the remote cluster.  Referenced by Replications.
	Name string `json:"name,onitempty"`

	// UUID of the remote cluster.
	UUID string `json:"uuid,omitempty"`

	// Hostname is the connection string to use to connect the remote cluster.
	Hostname string `json:"hostname,omitempty"`

	// AuthenticationSecret is a secret used to authenticate when establishing a
	// remote connection.  It is only required when not using mTLS
	AuthenticationSecret *string `json:"authenticationSecret,omitempty"`

	// Replications are replication streams from this cluster to the remote one.
	Replications Replications `json:"replications"`

	// TLS if specified references a resource containing the necessary certificate
	// data.
	TLS *RemoteClusterTLS `json:"tls,omitempty"`
}

// XDCR allows management of XDCR settings.
type XDCR struct {
	// Managed defines whether XDCR is managed by the operator or not.
	Managed bool `json:"managed,omitempty"`

	// RemoteClusters is a set of named remote clusters to establish replications to.
	RemoteClusters []RemoteCluster `json:"remoteClusters,omitempty"`
}

type Buckets struct {
	// Managed defines whether buckets are managed by us or the clients.
	Managed bool `json:"managed,omitempty"`
	// Selector is a label selector used to list buckets in the namespace
	// that are managed by the Operator.
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

type RBAC struct {
	// Managed defines whether RBAC is managed by us or the clients.
	Managed bool `json:"managed,omitempty"`
	// Selector is a label selector used to list RBAC users in the namespace
	// that are managed by the Operator.
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
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

// TLSPolicy defines the TLS policy of an couchbase cluster
type TLSPolicy struct {
	// StaticTLS enables user to generate static x509 certificates and keys,
	// put them into Kubernetes secrets, and specify them into here.
	Static *StaticTLS `json:"static,omitempty"`

	// ClientCertificatePolicy optionally defines the policy to use.
	// If set then the OperatorSecret must contain a valid
	// certificate/key pair for the Administrator account.
	ClientCertificatePolicy *ClientCertificatePolicy `json:"clientCertificatePolicy,omitempty"`

	// ClientCertificatePaths optionally defines where to look in client
	// certificates to extract the user name.
	ClientCertificatePaths []ClientCertificatePath `json:"clientCertificatePaths,omitempty"`
}

type StaticTLS struct {
	// Member contains secrets containing TLS certs used by each couchbase member pod.
	Member *MemberSecret `json:"member,omitempty"`

	// OperatorSecret is the secret containing TLS certs used by operator to
	// talk securely to this cluster.
	OperatorSecret string `json:"operatorSecret,omitempty"`
}

// ClientCertificatePolicy defines the type of TLS policy to apply.  The default
// "disable" is implicit when the policy is not set.
type ClientCertificatePolicy string

const (
	// ClientCertificatePolicyEnable enables optional TLS client ceritifcates
	// falling back to password based authentication on failure.  This mode
	// is required when using secure XDCR... aledgedly, even though that can
	// have a client cert specified.
	ClientCertificatePolicyEnable ClientCertificatePolicy = "enable"

	// ClientCertificatePolicyMandatory enables mandatory TLS client certification.
	ClientCertificatePolicyMandatory ClientCertificatePolicy = "mandatory"
)

// ClientCertificatePath defines how to extract a username from a client ceritficate.
type ClientCertificatePath struct {
	// Path defines where in the X.509 specification to extract the username from.
	// Valid values are subject.cn, san.uri, san.dnsname, san.email.
	Path string `json:"path,omitempty"`

	// Prefix if specified allows a prefix to be stripped from the username.
	Prefix string `json:"prefix,omitempty"`

	// Delimiter if specified allows a suffix to be stripped from the username.
	Delimiter string `json:"delimiter,omitempty"`
}

type MemberSecret struct {
	// ServerSecret is the secret containing TLS certs used by each couchbase member pod
	// for the communication between couchbase server and its clients.
	ServerSecret string `json:"serverSecret,omitempty"`
}

type ClusterPhase string

const (
	ClusterPhaseNone     ClusterPhase = ""
	ClusterPhaseCreating ClusterPhase = "Creating"
	ClusterPhaseRunning  ClusterPhase = "Running"
	ClusterPhaseFailed   ClusterPhase = "Failed"
)

type ClusterCondition struct {
	// Status of the condition, one of True, False, Unknown.
	Status v1.ConditionStatus `json:"status"`
	// The last time this condition was updated.
	LastUpdateTime string `json:"lastUpdateTime,omitempty"`
	// Last time the condition transitioned from one status to another.
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`
	// The reason for the condition's last transition.
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition.
	Message string `json:"message,omitempty"`
}

type ClusterConditionType string

const (
	ClusterConditionAvailable    ClusterConditionType = "Available"
	ClusterConditionBalanced     ClusterConditionType = "Balanced"
	ClusterConditionManageConfig ClusterConditionType = "ManageConfig"
	ClusterConditionScaling      ClusterConditionType = "Scaling"
	ClusterConditionUpgrading    ClusterConditionType = "Upgrading"
)

type ClusterStatusMap map[ClusterConditionType]*ClusterCondition

// PortStatus contains the K8S port mappings for various services
type PortStatus struct {
	AdminServicePort        int32 `json:"adminServicePort,omitempty"`
	AdminServicePortTLS     int32 `json:"adminServicePortTLS,omitempty"`
	IndexServicePort        int32 `json:"indexServicePort,omitempty"`
	IndexServicePortTLS     int32 `json:"indexServicePortTLS,omitempty"`
	QueryServicePort        int32 `json:"queryServicePort,omitempty"`
	QueryServicePortTLS     int32 `json:"queryServicePortTLS,omitempty"`
	SearchServicePort       int32 `json:"searchServicePort,omitempty"`
	SearchServicePortTLS    int32 `json:"searchServicePortTLS,omitempty"`
	AnalyticsServicePort    int32 `json:"analyticsServicePort,omitempty"`
	AnalyticsServicePortTLS int32 `json:"analyticsServicePortTLS,omitempty"`
	EventingServicePort     int32 `json:"eventingServicePort,omitempty"`
	EventingServicePortTLS  int32 `json:"eventingServicePortTLS,omitempty"`
	DataServicePort         int32 `json:"dataServicePort,omitempty"`
	DataServicePortTLS      int32 `json:"dataServicePortTLS,omitempty"`
}

// PortStatusMap maps a node name to port status information
type PortStatusMap map[string]*PortStatus

type ClusterStatus struct {
	// Phase is the cluster running phase
	Phase  ClusterPhase `json:"phase"`
	Reason string       `json:"reason"`

	// ControlPuased indicates the operator pauses the control of the cluster.
	ControlPaused bool `json:"controlPaused"`

	// Condition keeps ten most recent cluster conditions
	Conditions ClusterStatusMap `json:"conditions,omitempty"`

	// A unique cluster identifier
	ClusterID string `json:"clusterId"`
	// Size is the current size of the cluster
	Size int `json:"size"`
	// Members are the couchbase members in the cluster
	Members MembersStatus `json:"members"`
	// CurrentVersion is the current cluster version
	CurrentVersion string `json:"currentVersion"`

	// Name of buckets active within cluster
	Buckets []cbmgr.Bucket `json:"buckets,omitempty"`

	// Name of users active within cluster
	Users []string `json:"users,omitempty"`

	// port exposing couchbase cluster
	AdminConsolePort    string `json:"adminConsolePort,omitempty"`
	AdminConsolePortSSL string `json:"adminConsolePortSSL,omitempty"`

	// ExposedFeatures keeps tabs on what features are currently
	// exposed as node ports
	ExposedFeatures ExposedFeatureList `json:"exposedFeatures,omitempty"`

	// ports exposing couchbase cluster on the K8S node network
	ExposedPorts PortStatusMap `json:"nodePorts,omitempty"`
}

type MemberStatusEntry struct {
	Name string
}

type MemberStatusList []MemberStatusEntry

type MembersStatus struct {
	// Ready are the couchbase members that are ready to serve requests
	// The member names are the same as the couchbase pod names
	Ready MemberStatusList `json:"ready,omitempty"`
	// Unready are the couchbase members not ready to serve requests
	Unready MemberStatusList `json:"unready,omitempty"`
}

// Used to marshal and unmarshal information from the Upgrading condition.
type UpgradeStatus struct {
	State       string
	Source      string
	Target      string
	TargetCount int
	TotalCount  int
}

const (
	// UpgradingMessageFormat is the message format used when the cluster is upgrading.
	// The first field is the state of the operation, the second and third fields are
	// the source and target versions respectively, the forth and fifth fields are the
	// counts of members at the target version and total members respectively.
	UpgradingMessageFormat = "Cluster %s from %s to %s (progress %d/%d)"
	// UpgradingMessageStateUpgrading is used in the UpgradingMessageFormat to indicate
	// an upgrade in progress.
	UpgradingMessageStateUpgrading = "upgrading"
	// UpgradingMessageStateRollback is used in the UpgradingMessageFormat to indicate
	// a rollback in process.
	UpgradingMessageStateRollback = "rolling-back"
)
