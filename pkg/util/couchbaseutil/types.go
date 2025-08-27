package couchbaseutil

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/couchbase/couchbase-operator/pkg/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var (
	ErrInvalidResourceName = fmt.Errorf("unsupported resource name")
	ErrMissingResource     = fmt.Errorf("requested resource not found")
	ErrDuplicatedResource  = fmt.Errorf("requested resource duplicated")
)

type RebalanceStatus string

const (
	RebalanceStatusNotRunning RebalanceStatus = "notRunning"
	RebalanceStatusRunning    RebalanceStatus = "running"
	RebalanceStatusUnknown    RebalanceStatus = "unknown"
	RebalanceStatusNone       RebalanceStatus = "none"
)

type IndexStorageMode string

const (
	IndexStorageNone   IndexStorageMode = ""
	IndexStoragePlasma IndexStorageMode = "plasma"
	IndexStorageMOI    IndexStorageMode = "memory_optimized"
)

type AvailableStorageType string

const (
	StorageTypeHDD AvailableStorageType = "hdd"
	StorageTypeSSD AvailableStorageType = "ssd"
)

type IndexLogLevel string

const (
	IndexLogLevelDebug   IndexLogLevel = "debug"
	IndexLogLevelError   IndexLogLevel = "error"
	IndexLogLevelFatal   IndexLogLevel = "fatal"
	IndexLogLevelInfo    IndexLogLevel = "info"
	IndexLogLevelSilent  IndexLogLevel = "silent"
	IndexLogLevelTiming  IndexLogLevel = "timing"
	IndexLogLevelTrace   IndexLogLevel = "trace"
	IndexLogLevelVerbose IndexLogLevel = "verbose"
	IndexLogLevelWarn    IndexLogLevel = "warn"
)

type ServiceName string

const (
	DataService      ServiceName = "kv"
	IndexService     ServiceName = "index"
	QueryService     ServiceName = "n1ql"
	SearchService    ServiceName = "fts"
	EventingService  ServiceName = "eventing"
	AnalyticsService ServiceName = "cbas"
)

type ServiceList []ServiceName

func ServiceListFromStringArray(arr []string) (ServiceList, error) {
	list := []ServiceName{}

	for _, svc := range arr {
		if svc == "admin" {
			continue
		}

		serviceName, err := MapServiceNameToServerServiceName(svc)
		if err != nil {
			return list, err
		}

		list = append(list, serviceName)
	}

	return list, nil
}

// MapServiceNameToServerServiceName maps CRD and server service names to the server service name.
func MapServiceNameToServerServiceName(service string) (ServiceName, error) {
	switch service {
	case "kv", "data":
		return DataService, nil
	case "index":
		return IndexService, nil
	case "n1ql", "query":
		return QueryService, nil
	case "fts", "search":
		return SearchService, nil
	case "eventing":
		return EventingService, nil
	case "cbas", "analytics":
		return AnalyticsService, nil
	default:
		return "", fmt.Errorf("%w: invalid service name: %s", errors.NewStackTracedError(ErrInvalidResourceName), service)
	}
}

func (s ServiceList) String() string {
	strs := []string{}

	for _, svc := range s {
		strs = append(strs, (string)(svc))
	}

	return strings.Join(strs, ",")
}

type RecoveryType string

const (
	RecoveryTypeDelta RecoveryType = "delta"
	RecoveryTypeFull  RecoveryType = "full"
)

type ClusterInfo struct {
	SearchMemoryQuotaMB    int64                       `json:"ftsMemoryQuota"`
	IndexMemoryQuotaMB     int64                       `json:"indexMemoryQuota"`
	DataMemoryQuotaMB      int64                       `json:"memoryQuota"`
	EventingMemoryQuotaMB  int64                       `json:"eventingMemoryQuota"`
	AnalyticsMemoryQuotaMB int64                       `json:"cbasMemoryQuota"`
	Nodes                  []NodeInfo                  `json:"nodes"`
	RebalanceStatus        RebalanceStatus             `json:"rebalanceStatus"`
	ClusterName            string                      `json:"clusterName"`
	Balanced               bool                        `json:"balanced"`
	Counters               map[string]int              `json:"counters"`
	ServicesNeedRebalance  []ServicesNeedRebalanceCode `json:"servicesNeedRebalance,omitempty"`
	BucketsNeedRebalance   []BucketNeedsRebalanceCode  `json:"bucketsNeedRebalance,omitempty"`
}

type RebalanceReasonCode struct {
	Code        string `json:"code"`
	Description string `json:"description"`
}

type ServicesNeedRebalanceCode struct {
	RebalanceReasonCode
	Services []ServiceName `json:"services"`
}

type BucketNeedsRebalanceCode struct {
	RebalanceReasonCode
	Buckets []string `json:"buckets"`
}

// GetNode returns the named node.
func (c *ClusterInfo) GetNode(hostname HostName) (*NodeInfo, error) {
	for i := range c.Nodes {
		node := &c.Nodes[i]

		if node.HostName == hostname {
			return node, nil
		}
	}

	return nil, fmt.Errorf("%w: unable to lookup node %s", errors.NewStackTracedError(ErrMissingResource), hostname)
}

// PoolsDefaults returns a struct which could be used with the /pools/default API.
func (c *ClusterInfo) PoolsDefaults() *PoolsDefaults {
	return &PoolsDefaults{
		ClusterName:          c.ClusterName,
		DataMemoryQuota:      c.DataMemoryQuotaMB,
		IndexMemoryQuota:     c.IndexMemoryQuotaMB,
		SearchMemoryQuota:    c.SearchMemoryQuotaMB,
		EventingMemoryQuota:  c.EventingMemoryQuotaMB,
		AnalyticsMemoryQuota: c.AnalyticsMemoryQuotaMB,
	}
}

type IndexSettings struct {
	StorageMode           IndexStorageMode `url:"storageMode" json:"storageMode"`
	Threads               int              `url:"indexerThreads" json:"indexerThreads"`
	MemSnapInterval       int              `url:"memorySnapshotInterval" json:"memorySnapshotInterval"`
	StableSnapInterval    int              `url:"stableSnapshotInterval" json:"stableSnapshotInterval"`
	MaxRollbackPoints     int              `url:"maxRollbackPoints" json:"maxRollbackPoints"`
	LogLevel              IndexLogLevel    `url:"logLevel" json:"logLevel"`
	NumberOfReplica       int              `url:"numReplica" json:"numReplica"`
	RedistributeIndexes   bool             `url:"redistributeIndexes" json:"redistributeIndexes"`
	EnableShardAffinity   *bool            `url:"enableShardAffinity,omitempty" json:"enableShardAffinity"`
	EnablePageBloomFilter *bool            `url:"enablePageBloomFilter,omitempty" json:"enablePageBloomFilter"`
	DeferBuild            *bool            `url:"deferBuild,omitempty" json:"deferBuild"`
}

type FailoverOnDiskFailureSettings struct {
	Enabled    bool   `url:"failoverOnDataDiskIssues[enabled]" json:"enabled"`
	TimePeriod *int64 `url:"failoverOnDataDiskIssues[timePeriod],omitempty" json:"timePeriod"`
}

type AutoFailoverSettings struct {
	Enabled                          bool                          `url:"enabled" json:"enabled"`
	Timeout                          int64                         `url:"timeout" json:"timeout"`
	Count                            uint8                         `json:"count"`
	FailoverOnDataDiskIssues         FailoverOnDiskFailureSettings `url:"" json:"failoverOnDataDiskIssues"`
	FailoverServerGroup              *bool                         `url:"failoverServerGroup,omitempty" json:"failoverServerGroup"`
	MaxCount                         uint64                        `url:"maxCount" json:"maxCount"`
	AllowFailoverEphemeralNoReplicas *bool                         `url:"allowFailoverEphemeralNoReplicas,omitempty" json:"allowFailoverEphemeralNoReplicas,omitempty"`
}

type AlternateAddressesExternalPorts struct {
	// AdminPort is the admin service K8S node port (mapped to 8091)
	AdminServicePort int32 `url:"mgmt,omitempty" json:"mgmt"`
	// AdminPortSSL is the admin service K8S node port (mapped to 18091)
	AdminServicePortTLS int32 `url:"mgmtSSL,omitempty" json:"mgmtSSL"`
	// ViewAndXDCRServicePort is the view service K8S node port (mapped to 8092)
	ViewAndXDCRServicePort int32 `url:"capi,omitempty" json:"capi"`
	// ViewAndXDCRServicePortTLS is the view service K8S node port (mapped to 8092)
	ViewAndXDCRServicePortTLS int32 `url:"capiSSL,omitempty" json:"capiSSL"`
	// QueryServicePort is the query service K8S node port (mapped to 8093)
	QueryServicePort int32 `url:"n1ql,omitempty" json:"n1ql"`
	// QueryServicePortTLS is the query service K8S node port (mapped to 18093)
	QueryServicePortTLS int32 `url:"n1qlSSL,omitempty" json:"n1qlSSL"`
	// SearchServicePort is the full text search service K8S node port (mapped to 8094)
	SearchServicePort int32 `url:"fts,omitempty" json:"fts"`
	// SearchServicePortTLS is the full text search service K8S node port (mapped to 18094)
	SearchServicePortTLS int32 `url:"ftsSSL,omitempty" json:"ftsSSL"`
	// AnalyticsServicePort is the analytics service K8S node port (mapped to 8095)
	AnalyticsServicePort int32 `url:"cbas,omitempty" json:"cbas"`
	// AnalyticsServicePortTLS is the analytics service K8S node port (mapped to 18095)
	AnalyticsServicePortTLS int32 `url:"cbasSSL,omitempty" json:"cbasSSL"`
	// EventingServicePort is the eventing service K8S node port (mapped to 8096)
	EventingServicePort int32 `url:"eventingAdminPort,omitempty" json:"eventingAdminPort"`
	// EventingServicePortTLS is the eventing service K8S node port (mapped to 18096)
	EventingServicePortTLS int32 `url:"eventingSSL,omitempty" json:"eventingSSL"`
	// DataServicePort is the data service K8S node port (mapped to 11210)
	DataServicePort int32 `url:"kv,omitempty" json:"kv"`
	// DataServicePortTLS is the data service K8S node port (mapped to 11207)
	DataServicePortTLS int32 `url:"kvSSL,omitempty" json:"kvSSL"`
	// IndexServicePort is the index service K8S node port (mapped to 9102)
	IndexServicePort int32 `url:"indexHttp,omitempty" json:"indexHttp"`
	// IndexServicePortTLS is the index service K8S node port (mapped to 19102)
	IndexServicePortTLS int32 `url:"indexHttps,omitempty" json:"indexHttps"`
}

// AlternateAddresses defines a K8S node address and port mapping for
// use by clients outside of the pod network.  Hostname must be set,
// ports are ignored if zero.
type AlternateAddressesExternal struct {
	// Hostname is the host name to connect to (typically a L3 address)
	Hostname string `url:"hostname" json:"hostname"`
	// Ports is the map of service to external ports
	Ports *AlternateAddressesExternalPorts `url:"" json:"ports,omitempty"`
}

type AlternateAddresses struct {
	External *AlternateAddressesExternal `json:"external,omitempty"`
}

type NodeService struct {
	ThisNode           bool                `json:"thisNode"`
	AlternateAddresses *AlternateAddresses `json:"alternateAddresses,omitempty"`
}

// NodeServices is returned by the /pools/default/nodeServices API.
type NodeServices struct {
	NodesExt []NodeService `json:"nodesExt"`
}

// AddressFamilyOut in keeping Couchbase APIs returns "inet" and "inet6"
// but accepts "ipv4" and "ipv6".
type AddressFamilyOut string

const (
	AddressFamilyOutInet  AddressFamilyOut = "inet"
	AddressFamilyOutInet6 AddressFamilyOut = "inet6"
)

// ExternalListener is something that has nothing to do with network
// configuration, and changing it makes no difference to the cluster,
// but you need to set it in order to change the network configuration.
// I'd like to comment more, but there is literally no documentation
// about this secret hidden API.
type ExternalListener struct {
	AddressFamily  AddressFamilyOut `json:"afamily"`
	NodeEncryption bool             `json:"nodeEncryption"`
}

type NodeInfo struct {
	ThisNode           bool                 `json:"thisNode"`
	Uptime             string               `json:"uptime"`
	Membership         string               `json:"clusterMembership"`
	RecoveryType       RecoveryType         `json:"recoveryType"`
	Status             string               `json:"status"`
	OTPNode            OTPNode              `json:"otpNode"`
	HostName           HostName             `json:"hostname"`
	Services           []string             `json:"services"`
	AvailableStorage   AvailableStorageInfo `json:"storage"`
	AlternateAddresses *AlternateAddresses  `json:"alternateAddresses,omitempty"`
	Version            string               `json:"version"`

	// This property is only available if a bucket in the cluster has been changed from
	// couchstore to magma or vice versa.
	StorageBackend string `json:"storageBackend"`

	// AddressFamily is the actual network configuration of the node, and controls
	// the protocol Couchbase talks to itself over (A vs AAAA lookups most likely).
	// Must have a corresponding protocol listener defined.
	AddressFamily AddressFamilyOut `json:"addressFamily"`

	// AddressFamilyOnly only exposes the specified address family, if this
	// is not set, then it may be running dual stack and exposing on IPv4 and IPv6.
	AddressFamilyOnly bool `json:"addressFamilyOnly"`

	// NodeEncryption enables/disables node-to-node encryption.  Must have a corresponding
	// listener, for the address family, with encryption enabled.
	NodeEncryption bool `json:"nodeEncryption"`

	// ExternalListeners are... something, but you need to have a corresponding one
	// in order to set the address family, or node encryption.
	ExternalListeners []ExternalListener `json:"externalListeners"`

	// This property is only available if a bucket has eviction policy changed with
	// noRestart set to true.
	EvictionPolicy string `json:"evictionPolicy"`
}

type AvailableStorageInfo map[AvailableStorageType][]StorageInfo

type StorageInfo struct {
	Path      string `json:"path"`
	IndexPath string `json:"index_path"`
}

type PoolsInfo struct {
	// Enterprise determines whether this is an EE build.
	Enterprise bool `json:"isEnterprise"`

	// UUID, in true server style, is either a string, or initially
	// an array.
	UUID interface{} `json:"uuid"`

	// Version is a semver with a bunch of other cruft on the end.
	Version string `json:"implementationVersion"`
}

// GetUUID abstracts away the polymorphic nature of the UUID field,
// returning an empty string if something goes wrong.
func (p PoolsInfo) GetUUID() string {
	uuid, ok := p.UUID.(string)
	if !ok {
		return ""
	}

	return uuid
}

type TaskType string

const (
	TaskTypeRebalance TaskType = "rebalance"
	TaskTypeXDCR      TaskType = "xdcr"
)

// Task is a base object to describe the very unfriendly polymorphic
// task struct.
type Task struct {
	// Common attributes.
	Type   TaskType `json:"type"`
	Status string   `json:"status"`

	// Rebalance attributes.
	Progress  float64            `json:"progress"`
	Stale     bool               `json:"statusIsStale"`
	Timeout   bool               `json:"masterRequestTimedOut"`
	NodesInfo RebalanceNodesInfo `json:"nodesInfo"`

	// Replication attributes.
	Source           string          `json:"source"`
	Target           string          `json:"target"`
	ReplicationType  ReplicationType `url:"replicationType"`
	FilterExpression string          `url:"filterExpression"`
}

type RebalanceNodesInfo struct {
	ActiveNodes []string `json:"active_nodes"`
	KeepNodes   []string `json:"keep_nodes"`
	EjectNodes  []string `json:"eject_nodes"`
}

type TaskList []Task

// GetTask expects to find at most one task of the specified type.
// If more than one is found it's considered an error and you have
// probably introduced a bug.
func (t TaskList) GetTask(taskType TaskType) (*Task, error) {
	var task *Task

	for i := range t {
		temp := &t[i]

		if temp.Type == taskType {
			if task != nil {
				return nil, fmt.Errorf("%w: found more than one task of type %v", errors.NewStackTracedError(ErrDuplicatedResource), taskType)
			}

			task = temp
		}
	}

	if task == nil {
		return nil, fmt.Errorf("%w: no task of type %v found", errors.NewStackTracedError(ErrMissingResource), taskType)
	}

	return task, nil
}

// FilterType returns all tasks of the specified type.
func (t TaskList) FilterType(taskType TaskType) *TaskList {
	out := TaskList{}

	for _, task := range t {
		if task.Type == taskType {
			out = append(out, task)
		}
	}

	return &out
}

// PoolsDefaults is the data that may be posted via the /pools/default API.
type PoolsDefaults struct {
	ClusterName          string `url:"clusterName,omitempty"`
	DataMemoryQuota      int64  `url:"memoryQuota,omitempty"`
	IndexMemoryQuota     int64  `url:"indexMemoryQuota,omitempty"`
	SearchMemoryQuota    int64  `url:"ftsMemoryQuota,omitempty"`
	EventingMemoryQuota  int64  `url:"eventingMemoryQuota,omitempty"`
	AnalyticsMemoryQuota int64  `url:"cbasMemoryQuota,omitempty"`
}

type (
	IoPriorityType        string
	IoPriorityThreadCount int
)

const (
	IoPriorityTypeLow         IoPriorityType        = "low"
	IoPriorityTypeHigh        IoPriorityType        = "high"
	IoPriorityThreadCountLow  IoPriorityThreadCount = 3
	IoPriorityThreadCountHigh IoPriorityThreadCount = 8
)

type CouchbaseStorageBackend string

const (
	CouchbaseStorageBackendCouchstore CouchbaseStorageBackend = "couchstore"
	CouchbaseStorageBackendMagma      CouchbaseStorageBackend = "magma"
)

type DurabilityImpossibleFallback string

const (
	DurabilityImpossibleFallbackDisabled DurabilityImpossibleFallback = "disabled"
	DurabilityImpossibleFallbackActive   DurabilityImpossibleFallback = "fallbackToActiveAck"
)

type CompressionMode string

const (
	CompressionModeOff     CompressionMode = "off"
	CompressionModePassive CompressionMode = "passive"
	CompressionModeActive  CompressionMode = "active"
)

type Durability string

const (
	DurabilityNone                     Durability = "none"
	DurabilityMajority                 Durability = "majority"
	DurabilityMajorityAndPersistActive Durability = "majorityAndPersistActive"
	DurabilityPersistToMajority        Durability = "persistToMajority"
)

// Bucket is a canonical representation of the parameters used for creating/editing buckets with the REST API.
type Bucket struct {
	SampleBucket                      bool
	BucketName                        string                       `json:"name"`
	BucketType                        string                       `json:"type"`
	BucketStorageBackend              CouchbaseStorageBackend      `json:"storageBackend"`
	BucketMemoryQuota                 int64                        `json:"memoryQuota"`
	BucketReplicas                    int                          `json:"replicas"`
	IoPriority                        IoPriorityType               `json:"ioPriority"`
	EvictionPolicy                    string                       `json:"evictionPolicy"`
	ConflictResolution                string                       `json:"conflictResolution"`
	EnableFlush                       bool                         `json:"enableFlush"`
	EnableIndexReplica                bool                         `json:"enableIndexReplica"`
	BucketPassword                    string                       `json:"password"`
	CompressionMode                   CompressionMode              `json:"compressionMode"`
	DurabilityMinLevel                Durability                   `json:"durabilityMinLevel"`
	MaxTTL                            int                          `json:"maxTTL"`
	HistoryRetentionSeconds           uint64                       `json:"historyRetentionSeconds"`
	HistoryRetentionBytes             uint64                       `json:"historyRetentionBytes"`
	HistoryRetentionCollectionDefault *bool                        `json:"historyRetentionCollectionDefault"`
	MagmaSeqTreeDataBlockSize         *uint64                      `json:"magmaSeqTreeDataBlockSize"`
	MagmaKeyTreeDataBlockSize         *uint64                      `json:"magmaKeyTreeDataBlockSize"`
	Rank                              *int                         `json:"rank"`
	PurgeInterval                     *float64                     `json:"purgeInterval,omitempty"`
	AutoCompactionSettings            BucketAutoCompactionSettings `json:"autoCompactionSettings,omitempty"`
	EnableCrossClusterVersioning      *bool                        `json:"enableCrossClusterVersioning,omitempty"`
	VersionPruningWindowHrs           *uint64                      `json:"versionPruningWindowHrs,omitempty"`
	AccessScannerEnabled              *bool                        `json:"accessScannerEnabled,omitempty"`
	ExpiryPagerSleepTime              *uint64                      `json:"expiryPagerSleepTime,omitempty"`
	WarmupBehavior                    string                       `json:"warmupBehavior,omitempty"`
	MemoryLowWatermark                *int                         `json:"memoryLowWatermark,omitempty"`
	MemoryHighWatermark               *int                         `json:"memoryHighWatermark,omitempty"`
	DurabilityImpossibleFallback      DurabilityImpossibleFallback `json:"durabilityImpossibleFallback,omitempty"`
	NoRestart                         *bool                        `json:"noRestart,omitempty"`
}

type BucketList []Bucket
type BucketStatusList []BucketStatus

func (b BucketList) Get(name string) (*Bucket, error) {
	for i := range b {
		bucket := b[i]

		if bucket.BucketName == name {
			return &bucket, nil
		}
	}

	return nil, fmt.Errorf("%w: bucket %s not found", errors.NewStackTracedError(ErrMissingResource), name)
}

type BucketBasicStats struct {
	DataUsed         int     `json:"dataUsed"`
	DiskFetches      float64 `json:"diskFetches"`
	DiskUsed         int     `json:"diskUsed"`
	ItemCount        int     `json:"itemCount"`
	MemUsed          int     `json:"memUsed"`
	OpsPerSec        float64 `json:"opsPerSec"`
	QuotaPercentUsed float64 `json:"quotaPercentUsed"`
}

// SM: THIS IS SO M****RF***KING ANNOYING.  HAVE A SINGLE SOURCE OF TRUTH!!!!!
// THIS IS UTTERLY POINTLESS OVERENGINEERING.  (Fix me essentially).
type BucketStatus struct {
	Nodes                             []NodeInfo                   `json:"nodes"`
	BucketName                        string                       `json:"name"`
	BucketType                        string                       `json:"bucketType"`
	StorageBackend                    CouchbaseStorageBackend      `json:"storageBackend"`
	EvictionPolicy                    string                       `json:"evictionPolicy"`
	ConflictResolution                string                       `json:"conflictResolutionType"`
	EnableIndexReplica                bool                         `json:"replicaIndex"`
	ReplicaNumber                     int                          `json:"replicaNumber"`
	ThreadsNumber                     IoPriorityThreadCount        `json:"threadsNumber"`
	Controllers                       map[string]string            `json:"controllers"`
	Quota                             map[string]int64             `json:"quota"`
	Stats                             map[string]string            `json:"stats"`
	VBServerMap                       VBucketServerMap             `json:"vBucketServerMap"`
	CompressionMode                   CompressionMode              `json:"compressionMode"`
	DurabilityMinLevel                Durability                   `json:"durabilityMinLevel"`
	MaxTTL                            int                          `json:"maxTTL"`
	BasicStats                        BucketBasicStats             `json:"basicStats"`
	HistoryRetentionSeconds           uint64                       `json:"historyRetentionSeconds,omitempty"`
	HistoryRetentionBytes             uint64                       `json:"historyRetentionBytes,omitempty"`
	HistoryRetentionCollectionDefault *bool                        `json:"historyRetentionCollectionDefault,omitempty"`
	MagmaSeqTreeDataBlockSize         *uint64                      `json:"magmaSeqTreeDataBlockSize,omitempty"`
	MagmaKeyTreeDataBlockSize         *uint64                      `json:"magmaKeyTreeDataBlockSize,omitempty"`
	Rank                              *int                         `json:"rank"`
	PurgeInterval                     *float64                     `json:"purgeInterval,omitempty"`
	AutoCompactionSettings            BucketAutoCompactionSettings `json:"autoCompactionSettings,omitempty"`
	EnableCrossClusterVersioning      *bool                        `json:"enableCrossClusterVersioning,omitempty"`
	VersionPruningWindowHrs           *uint64                      `json:"versionPruningWindowHrs,omitempty"`
	AccessScannerEnabled              *bool                        `json:"accessScannerEnabled,omitempty"`
	ExpiryPagerSleepTime              *uint64                      `json:"expiryPagerSleepTime,omitempty"`
	WarmupBehavior                    string                       `json:"warmupBehavior,omitempty"`
	MemoryLowWatermark                *int                         `json:"memoryLowWatermark,omitempty"`
	MemoryHighWatermark               *int                         `json:"memoryHighWatermark,omitempty"`
	DurabilityImpossibleFallback      DurabilityImpossibleFallback `json:"durabilityImpossibleFallback,omitempty"`
}

type BucketAutoCompactionSettings struct {
	Enabled  bool
	Settings *AutoCompactionAutoCompactionSettings
}

func (b *BucketAutoCompactionSettings) UnmarshalJSON(data []byte) error {
	var boolVal bool
	if err := json.Unmarshal(data, &boolVal); err == nil {
		b.Enabled = false
		b.Settings = nil

		return nil
	}

	var settings AutoCompactionAutoCompactionSettings
	if err := json.Unmarshal(data, &settings); err == nil {
		b.Enabled = true
		b.Settings = &settings
	}

	return nil
}

type VBucketServerMap struct {
	ServerList []string   `json:"serverList"`
	VBMap      VBucketMap `json:"vBucketMap"`
}

type VBucketMap [][]int

type AuthDomain string

const (
	InternalAuthDomain AuthDomain = "local"
	LDAPAuthDomain     AuthDomain = "external"
)

type User struct {
	Name     string     `json:"name"`
	Password string     `json:"password"`
	Domain   AuthDomain `json:"domain"`
	ID       string     `json:"id"`
	Roles    []UserRole `json:"roles"`
	Groups   []string   `json:"groups"`
	Locked   *bool      `json:"locked,omitempty"`
}

type UserList []User

type Group struct {
	ID           string     `json:"id"`
	Roles        []UserRole `json:"roles"`
	Description  string     `json:"description"`
	LDAPGroupRef string     `json:"ldap_group_ref"`
}

func (g *Group) HasRole(role UserRole) bool {
	for _, gRole := range g.Roles {
		if RoleToStr(gRole) == RoleToStr(role) {
			return true
		}
	}

	return false
}

type GroupList []Group

type LDAPEncryption string

const (
	LDAPEncryptionNone     LDAPEncryption = "None"
	LDAPEncryptionStartTLS LDAPEncryption = "StartTLSExtension"
	LDAPEncryptionTLS      LDAPEncryption = "TLS"
)

type LDAPSettings struct {
	// Enables using LDAP to authenticate users.
	AuthenticationEnabled bool `json:"authenticationEnabled"`
	// Enables use of LDAP groups for authorization.
	AuthorizationEnabled bool `json:"authorizationEnabled"`
	// List of LDAP hosts.
	Hosts []string `json:"hosts"`
	// LDAP port
	Port int `json:"port"`
	// Encryption method to communicate with LDAP servers.
	// Can be StartTLSExtension, TLS, or false.
	Encryption LDAPEncryption `json:"encryption,omitempty"`
	// Whether server certificate validation be enabled
	EnableCertValidation bool `json:"serverCertValidation"`
	// Certificate in PEM format to be used in LDAP server certificate validation
	CACert string `json:"cacert"`
	// LDAP query, to get the users' groups by username in RFC4516 format.
	GroupsQuery string `json:"groupsQuery,omitempty"`
	// DN to use for searching users and groups synchronization.
	BindDN string `json:"bindDN,omitempty"`
	// Password for query_dn user.
	BindPass string `json:"bindPass,omitempty"`
	// User to distinguished name (DN) mapping. If none is specified,
	// the username is used as the user’s distinguished name.
	UserDNMapping LDAPUserDNMapping `json:"userDNMapping,omitempty"`
	// If enabled Couchbase server will try to recursively search for groups
	// for every discovered ldap group. groupsQuery will be user for the search.
	NestedGroupsEnabled bool `json:"nestedGroupsEnabled,omitempty"`
	// Maximum number of recursive groups requests the server is allowed to perform.
	// Requires NestedGroupsEnabled.  Values between 1 and 100: the default is 10.
	NestedGroupsMaxDepth uint64 `json:"nestedGroupsMaxDepth,omitempty"`
	// Lifetime of values in cache in milliseconds. Default 300000 ms.
	CacheValueLifetime uint64 `json:"cacheValueLifetime,omitempty"`
	// Enabled Moddlebox compatibility mode (only 7.6+)
	MiddleboxCompMode *bool `json:"middleboxCompMode,omitempty"`
}

type LDAPUserDNMapping struct {
	Template string `json:"template,omitempty"`
	Query    string `json:"query,omitempty"`
}

type LDAPStatusResult string

const (
	LDAPStatusResultSuccess LDAPStatusResult = "success"
	LDAPStatusResultError   LDAPStatusResult = "error"
)

type LDAPStatus struct {
	Result LDAPStatusResult `json:"result"`
	Reason string           `json:"reason"`
}

type UserRole struct {
	Role           string `json:"role"`
	BucketName     string `json:"bucket_name"`
	ScopeName      string `json:"scope_name"`
	CollectionName string `json:"collection_name"`
}

func (u *UserRole) IsBucketRole() bool {
	return u.BucketName != "" && (u.ScopeName == "*" || u.ScopeName == "")
}

func (u *UserRole) IsScopeRole() bool {
	return u.ScopeName != "*" && u.ScopeName != "" && (u.CollectionName == "*" || u.CollectionName == "")
}

func (u *UserRole) IsCollectionRole() bool {
	return u.CollectionName != "*" && u.CollectionName != ""
}

func NewRole(role, bucket, scope, collection string) *UserRole {
	return &UserRole{
		Role:           role,
		BucketName:     bucket,
		ScopeName:      scope,
		CollectionName: collection,
	}
}

// This is to remove "*" from collection_name and scope_name as we can't
// pass those back to the API, so we might as well dump it.
func (u *UserRole) UnmarshalJSON(data []byte) error {
	var dat map[string]interface{}
	if err := json.Unmarshal(data, &dat); err != nil {
		return errors.NewStackTracedError(err)
	}

	if val, ok := dat["bucket_name"]; ok {
		u.BucketName, _ = val.(string)
	}

	if val, ok := dat["role"]; ok {
		u.Role, _ = val.(string)
	}

	if val, ok := dat["scope_name"]; ok {
		u.ScopeName, _ = val.(string)
	}

	if val, ok := dat["collection_name"]; ok {
		u.CollectionName, _ = val.(string)
	}

	if u.CollectionName == "*" {
		u.CollectionName = ""
	}

	if u.ScopeName == "*" {
		u.ScopeName = ""
	}

	return nil
}

type LogMessage struct {
	Node       string `json:"node"`
	Type       string `json:"type"`
	Code       uint8  `json:"code"`
	Module     string `json:"module"`
	Tstamp     uint64 `json:"tstamp"`
	ShortText  string `json:"shortText"`
	Text       string `json:"text"`
	ServerTime string `json:"serverTime"`
}
type LogList []*LogMessage

func (li LogList) Len() int {
	return len(li)
}

func (li LogList) Less(i, j int) bool {
	return li[i].Tstamp < li[j].Tstamp
}

func (li LogList) Swap(i, j int) {
	li[i], li[j] = li[j], li[i]
}

func (s *BucketStatus) GetIoPriority() IoPriorityType {
	threadCount := s.ThreadsNumber

	if threadCount <= IoPriorityThreadCountLow {
		return IoPriorityTypeLow
	}

	return IoPriorityTypeHigh
}

// Unmarshall from json representation of type Bucket or BucketStatus.
func (b *Bucket) UnmarshalJSON(data []byte) error {
	// unmarshal as generic json
	var jsonData map[string]interface{}

	if err := json.Unmarshal(data, &jsonData); err != nil {
		return errors.NewStackTracedError(err)
	}

	// unmarshal as BucketStatus if nodes key exists
	if _, ok := jsonData["nodes"]; ok {
		return b.unmarshalFromStatus(data)
	}

	// unmarshal as standard bucket type
	type BucketAlias Bucket

	bucket := BucketAlias{}
	if err := json.Unmarshal(data, &bucket); err != nil {
		return errors.NewStackTracedError(err)
	}

	*b = Bucket(bucket)

	return nil
}

func (b *Bucket) unmarshalFromStatus(data []byte) error {
	// unmarshalling data as bucket status
	status := BucketStatus{}
	if err := json.Unmarshal(data, &status); err != nil {
		return errors.NewStackTracedError(err)
	}

	// Generic things across all bucket types
	b.BucketName = status.BucketName
	b.BucketType = status.BucketType

	if b.BucketType == "membase" {
		b.BucketType = "couchbase"
	}

	// For CB Server 7.0.0+
	// check "undefined"! looks crazy right? well, it's fixed now but may just leave it for now.
	if status.StorageBackend != "" && status.StorageBackend != "undefined" {
		b.BucketStorageBackend = status.StorageBackend
	}

	if b.BucketStorageBackend == CouchbaseStorageBackendMagma {
		b.HistoryRetentionBytes = status.HistoryRetentionBytes
		b.HistoryRetentionSeconds = status.HistoryRetentionSeconds
		b.HistoryRetentionCollectionDefault = status.HistoryRetentionCollectionDefault
		b.MagmaSeqTreeDataBlockSize = status.MagmaSeqTreeDataBlockSize
		b.MagmaKeyTreeDataBlockSize = status.MagmaKeyTreeDataBlockSize
	}

	if ramQuotaBytes, ok := status.Quota["rawRAM"]; ok {
		b.BucketMemoryQuota = ramQuotaBytes >> 20
	}

	b.EnableFlush = false

	if _, ok := status.Controllers["flush"]; ok {
		b.EnableFlush = ok
	}

	if b.BucketType == "memcached" {
		return nil
	}

	// Generic things across couchbase/ephemeral
	b.EvictionPolicy = status.EvictionPolicy

	// If the eviction policy for any node is different from the bucket, then we can
	// infer that the eviction policy was changed with noRestart set to true.
	for _, node := range status.Nodes {
		if node.EvictionPolicy != "" && node.EvictionPolicy != status.EvictionPolicy {
			noRestart := true
			b.NoRestart = &noRestart

			break
		}
	}

	b.ConflictResolution = status.ConflictResolution
	b.BucketReplicas = status.ReplicaNumber
	b.CompressionMode = status.CompressionMode
	b.IoPriority = status.GetIoPriority()
	b.DurabilityMinLevel = status.DurabilityMinLevel
	b.MaxTTL = status.MaxTTL
	b.Rank = status.Rank
	b.EnableCrossClusterVersioning = status.EnableCrossClusterVersioning
	b.VersionPruningWindowHrs = status.VersionPruningWindowHrs
	b.ExpiryPagerSleepTime = status.ExpiryPagerSleepTime
	b.WarmupBehavior = status.WarmupBehavior
	b.MemoryLowWatermark = status.MemoryLowWatermark
	b.MemoryHighWatermark = status.MemoryHighWatermark

	if b.BucketType == "ephemeral" {
		return nil
	}

	// Couchbase only things
	b.EnableIndexReplica = status.EnableIndexReplica

	b.MagmaSeqTreeDataBlockSize = status.MagmaSeqTreeDataBlockSize
	b.MagmaKeyTreeDataBlockSize = status.MagmaKeyTreeDataBlockSize

	b.PurgeInterval = status.PurgeInterval
	b.AutoCompactionSettings = status.AutoCompactionSettings

	b.AccessScannerEnabled = status.AccessScannerEnabled
	b.DurabilityImpossibleFallback = status.DurabilityImpossibleFallback

	return nil
}

//nolint:gocognit
func (b *Bucket) FormEncode(update bool) []byte {
	data := url.Values{}
	data.Set("name", b.BucketName)
	data.Set("bucketType", b.BucketType)
	data.Set("ramQuotaMB", strconv.Itoa(int(b.BucketMemoryQuota)))

	// Adds storageBackend for CB Server 7.0.0 onwards.
	if b.BucketStorageBackend != "" {
		data.Set("storageBackend", string(b.BucketStorageBackend))
	}

	if b.BucketType != "memcached" {
		data.Set("replicaNumber", strconv.Itoa(b.BucketReplicas))
		data.Set("maxTTL", strconv.Itoa(b.MaxTTL))
	}

	data.Set("authType", "sasl")
	data.Set("compressionMode", string(b.CompressionMode))
	data.Set("flushEnabled", BoolToBinaryStr(b.EnableFlush))

	if b.EvictionPolicy != "" {
		data.Set("evictionPolicy", b.EvictionPolicy)

		if update && b.NoRestart != nil {
			data.Set("noRestart", BoolAsStr(*b.NoRestart))
		}
	}

	if b.IoPriority == IoPriorityTypeLow {
		data.Set("threadsNumber", strconv.Itoa(int(IoPriorityThreadCountLow)))
	}

	if b.IoPriority == IoPriorityTypeHigh {
		data.Set("threadsNumber", strconv.Itoa(int(IoPriorityThreadCountHigh)))
	}

	if !update && b.ConflictResolution != "" {
		data.Set("conflictResolutionType", b.ConflictResolution)
	}

	if b.BucketType == "couchbase" {
		data.Set("replicaIndex", BoolToBinaryStr(b.EnableIndexReplica))

		data.Set("autoCompactionDefined", BoolAsStr(b.AutoCompactionSettings.Enabled))

		if b.AutoCompactionSettings.Enabled {
			if b.PurgeInterval != nil {
				data.Set("purgeInterval", strconv.FormatFloat(*b.PurgeInterval, 'f', -1, 64))
			}

			if b.AutoCompactionSettings.Settings != nil {
				for key, value := range encodeBucketAutoCompactionSettings(*b.AutoCompactionSettings.Settings) {
					data.Set(key, value)
				}
			}
		}

		if b.DurabilityImpossibleFallback != "" {
			data.Set("durabilityImpossibleFallback", string(b.DurabilityImpossibleFallback))
		}
	}

	if b.DurabilityMinLevel != "" {
		data.Set("durabilityMinLevel", string(b.DurabilityMinLevel))
	}

	if b.Rank != nil {
		data.Set("rank", strconv.Itoa(*b.Rank))
	}

	if b.BucketStorageBackend == CouchbaseStorageBackendMagma {
		if b.HistoryRetentionCollectionDefault != nil {
			data.Set("historyRetentionCollectionDefault", strconv.FormatBool(*b.HistoryRetentionCollectionDefault))
		}

		data.Set("historyRetentionBytes", strconv.FormatUint(b.HistoryRetentionBytes, 10))
		data.Set("historyRetentionSeconds", strconv.FormatUint(b.HistoryRetentionSeconds, 10))

		if b.MagmaSeqTreeDataBlockSize != nil {
			data.Set("magmaSeqTreeDataBlockSize", strconv.FormatUint(*b.MagmaSeqTreeDataBlockSize, 10))
		}

		if b.MagmaKeyTreeDataBlockSize != nil {
			data.Set("magmaKeyTreeDataBlockSize", strconv.FormatUint(*b.MagmaKeyTreeDataBlockSize, 10))
		}
	}

	if b.EnableCrossClusterVersioning != nil {
		// We can only set this to true if the bucket has already been created.
		if update {
			data.Set("enableCrossClusterVersioning", BoolAsStr(*b.EnableCrossClusterVersioning))
		} else {
			data.Set("enableCrossClusterVersioning", "false")
		}
	}

	if b.VersionPruningWindowHrs != nil {
		data.Set("versionPruningWindowHrs", strconv.FormatUint(*b.VersionPruningWindowHrs, 10))
	}

	if b.AccessScannerEnabled != nil {
		data.Set("accessScannerEnabled", BoolAsStr(*b.AccessScannerEnabled))
	}

	if b.ExpiryPagerSleepTime != nil {
		data.Set("expiryPagerSleepTime", strconv.FormatUint(*b.ExpiryPagerSleepTime, 10))
	}

	if b.WarmupBehavior != "" {
		data.Set("warmupBehavior", b.WarmupBehavior)
	}

	if b.MemoryLowWatermark != nil {
		data.Set("memoryLowWatermark", strconv.Itoa(*b.MemoryLowWatermark))
	}

	if b.MemoryHighWatermark != nil {
		data.Set("memoryHighWatermark", strconv.Itoa(*b.MemoryHighWatermark))
	}

	return []byte(data.Encode())
}

func encodeBucketAutoCompactionSettings(settings AutoCompactionAutoCompactionSettings) map[string]string {
	data := make(map[string]string)
	data["parallelDBAndViewCompaction"] = BoolAsStr(settings.ParallelDBAndViewCompaction)

	handleUndefinedInt64(&data, "databaseFragmentationThreshold[size]", settings.DatabaseFragmentationThreshold.Size)

	handleUndefinedInt(&data, "databaseFragmentationThreshold[percentage]", settings.DatabaseFragmentationThreshold.Percentage)

	handleUndefinedInt64(&data, "viewFragmentationThreshold[size]", settings.ViewFragmentationThreshold.Size)

	handleUndefinedInt(&data, "viewFragmentationThreshold[percentage]", settings.ViewFragmentationThreshold.Percentage)

	handleUndefinedInt(&data, "magmaFragmentationPercentage", settings.MagmaFragmentationThresholdPercentage)

	if settings.AllowedTimePeriod != nil {
		data["allowedTimePeriod[fromMinute]"] = strconv.Itoa(settings.AllowedTimePeriod.FromMinute)
		data["allowedTimePeriod[fromHour]"] = strconv.Itoa(settings.AllowedTimePeriod.FromHour)
		data["allowedTimePeriod[toMinute]"] = strconv.Itoa(settings.AllowedTimePeriod.ToMinute)
		data["allowedTimePeriod[toHour]"] = strconv.Itoa(settings.AllowedTimePeriod.ToHour)
		data["allowedTimePeriod[abortOutside]"] = BoolAsStr(settings.AllowedTimePeriod.AbortOutside)
	}

	return data
}

func handleUndefinedInt(data *map[string]string, key string, val int) {
	switch val {
	case -1:
		return
	case 0:
		(*data)[key] = "undefined"
	default:
		(*data)[key] = strconv.Itoa(val)
	}
}

func handleUndefinedInt64(data *map[string]string, key string, val int64) {
	switch val {
	case -1:
		return
	case 0:
		(*data)[key] = "undefined"
	default:
		(*data)[key] = strconv.FormatInt(val, 10)
	}
}

// SettingsStats is the data structure returned by /settings/stats.
type SettingsStats struct {
	// SendStats actually indicates whether to perform software update checks
	SendStats bool `json:"sendStats" url:"sendStats"`
}

// ServerGroup is a map from name to a list of nodes.
type ServerGroup struct {
	// Name is the human readable server group name
	Name string `json:"name"`
	// Nodes is a list of nodes who are members of the server group
	Nodes []NodeInfo `json:"nodes"`
	// URI is used to refer to a server group
	URI string `json:"uri"`
}

// ServerGroups is returned by /nodes/default/serverGroups.
type ServerGroups struct {
	// Groups is a list of ServerGroup objects
	Groups []ServerGroup `json:"groups"`
	// URI is the URI used to update server groups
	URI string `json:"uri"`
}

// GetRevision returns the server group revision ID (for CAS).
func (groups ServerGroups) GetRevision() string {
	// Expected to be /pools/default/serverGroups?rev=13585112
	return strings.Split(groups.URI, "=")[1]
}

// GetServerGroup looks up a server group by name.
func (groups ServerGroups) GetServerGroup(name string) *ServerGroup {
	for _, group := range groups.Groups {
		if group.Name == name {
			return &group
		}
	}

	return nil
}

// ServerGroupUpdateOTPNode defines a single node is OTP notation.
type ServerGroupUpdateOTPNode struct {
	OTPNode OTPNode `json:"otpNode"`
}

// ServerGroupUpdate defines a server group and its nodes.
type ServerGroupUpdate struct {
	// Name is the group name and must match the existing one
	Name string `json:"name,omitempty"`
	// URI is the same as returned in ServerGroup
	URI string `json:"uri"`
	// Nodes is a list of OTP nodes
	Nodes []ServerGroupUpdateOTPNode `json:"nodes"`
}

// ServerGroupsUpdate is used to move nodes between server groups.
type ServerGroupsUpdate struct {
	Groups []ServerGroupUpdate `json:"groups"`
}

// AutoCompactionDatabaseFragmentationThreshold indicates the percentage or size before a bucket
// compaction is triggered.
type AutoCompactionDatabaseFragmentationThreshold struct {
	Percentage int   `json:"percentage" url:"databaseFragmentationThreshold[percentage],handleundefined"`
	Size       int64 `json:"size" url:"databaseFragmentationThreshold[size],handleundefined"`
}

// UnmarshalJSON handles some *&$^ing moron's decision to have size as either an
// integer or "undefined".  Way to go!
func (r *AutoCompactionDatabaseFragmentationThreshold) UnmarshalJSON(b []byte) error {
	type t AutoCompactionDatabaseFragmentationThreshold

	var s struct {
		t
		Percentage interface{} `json:"percentage"`
		Size       interface{} `json:"size"`
	}

	if err := json.Unmarshal(b, &s); err != nil {
		return errors.NewStackTracedError(err)
	}

	*r = AutoCompactionDatabaseFragmentationThreshold(s.t)
	if i, ok := s.Size.(float64); ok {
		r.Size = int64(i)
	}

	if i, ok := s.Percentage.(float64); ok {
		r.Percentage = int(i)
	}

	return nil
}

// AutoCompactionViewFragmentationThreshold indicates the percentage or size before a view
// compaction is triggered.
type AutoCompactionViewFragmentationThreshold struct {
	Percentage int   `json:"percentage" url:"viewFragmentationThreshold[percentage],handleundefined"`
	Size       int64 `json:"size" url:"viewFragmentationThreshold[size],handleundefined"`
}

// UnmarshalJSON handles some *&$^ing moron's decision to have size as either an
// integer or "undefined".  Way to go!
func (r *AutoCompactionViewFragmentationThreshold) UnmarshalJSON(b []byte) error {
	type t AutoCompactionViewFragmentationThreshold

	var s struct {
		t
		Percentage interface{} `json:"percentage"`
		Size       interface{} `json:"size"`
	}

	if err := json.Unmarshal(b, &s); err != nil {
		return errors.NewStackTracedError(err)
	}

	*r = AutoCompactionViewFragmentationThreshold(s.t)

	if i, ok := s.Size.(float64); ok {
		r.Size = int64(i)
	}

	if i, ok := s.Percentage.(float64); ok {
		r.Percentage = int(i)
	}

	return nil
}

type AutoCompactionAllowedTimePeriod struct {
	FromHour     int  `json:"fromHour" url:"allowedTimePeriod[fromHour]"`
	FromMinute   int  `json:"fromMinute" url:"allowedTimePeriod[fromMinute]"`
	ToHour       int  `json:"toHour" url:"allowedTimePeriod[toHour]"`
	ToMinute     int  `json:"toMinute" url:"allowedTimePeriod[toMinute]"`
	AbortOutside bool `json:"abortOutside" url:"allowedTimePeriod[abortOutside]"`
}

type AutoCompactionAutoCompactionSettings struct {
	DatabaseFragmentationThreshold        AutoCompactionDatabaseFragmentationThreshold `json:"databaseFragmentationThreshold" url:""`
	ViewFragmentationThreshold            AutoCompactionViewFragmentationThreshold     `json:"viewFragmentationThreshold" url:""`
	MagmaFragmentationThresholdPercentage int                                          `json:"magmaFragmentationPercentage" url:"magmaFragmentationPercentage,handleundefined"`
	ParallelDBAndViewCompaction           bool                                         `json:"parallelDBAndViewCompaction" url:"parallelDBAndViewCompaction"`
	IndexCompactionMode                   string                                       `json:"indexCompactionMode" url:"indexCompactionMode"`
	AllowedTimePeriod                     *AutoCompactionAllowedTimePeriod             `json:"allowedTimePeriod,omitempty" url:""`
}

// handleAutoCompactionUndefinedFields sets the fragmentation threshold fields that are not set in the CRD to a value that the encoder can interpret in order to correctly clear the value from the settings regardless of server version.
// Omitting the value from the update auto-compaction settings request for server versions < 7.1 will clear the value. Later versions require "undefined" to be set as the field value in the request in order to clear the value.
// The "handleundefined" option in the urlencoding of the AutoCompactionAutoCompactionSettings struct manages this for us at a cluster level and the bucket encoding method handles this at a bucket level, however both require us to
// manually set the value to -1 for server versions < 7.1 before the request is encoded. This should be called before updating or creating a couchbasebucket when auto-compaction is enabled.
func (a *AutoCompactionAutoCompactionSettings) SetAutoCompactionUndefinedFieldsForEncoding(isOver71 bool) {
	if isOver71 {
		return
	}

	if a.DatabaseFragmentationThreshold.Percentage == 0 {
		a.DatabaseFragmentationThreshold.Percentage = -1
	}

	if a.DatabaseFragmentationThreshold.Size == 0 {
		a.DatabaseFragmentationThreshold.Size = -1
	}

	if a.ViewFragmentationThreshold.Percentage == 0 {
		a.ViewFragmentationThreshold.Percentage = -1
	}

	if a.ViewFragmentationThreshold.Size == 0 {
		a.ViewFragmentationThreshold.Size = -1
	}
}

// AutoCompactionSettings is the cluster wide auto-compaction settings for a
// Couchbase cluster.
type AutoCompactionSettings struct {
	AutoCompactionSettings AutoCompactionAutoCompactionSettings `json:"autoCompactionSettings" url:""`
	PurgeInterval          float64                              `json:"purgeInterval" url:"purgeInterval"`
}

// RemoteClusters is returned by /pools/default/remoteClusters.
type RemoteClusters []RemoteCluster

// RemoteClusterSecurity indicates the authentication method.
// There is a 'half', but we don't do half-arsed here.
type RemoteClusterSecurity string

const (
	// Username & password.
	RemoteClusterSecurityNone RemoteClusterSecurity = "none"

	// Full TLS/mTLS.
	RemoteClusterSecurityTLS RemoteClusterSecurity = "full"
)

// RemoteCluster describes an XDCR remote cluster.
type RemoteCluster struct {
	Name       string                `json:"name" url:"name"`
	Hostname   string                `json:"hostname"  url:"hostname"`
	Username   string                `json:"username"  url:"username"`
	Password   string                `json:"password"  url:"password"`
	UUID       string                `json:"uuid"  url:"uuid"`
	Deleted    bool                  `json:"deleted"`
	SecureType RemoteClusterSecurity `json:"secureType" url:"secureType,omitempty"`
	Network    string                `json:"network_type" url:"network_type,omitempty"`
	CA         string                `json:"certificate" url:"certificate,omitempty"`

	// These are here for convenience and should only be populated
	// after comparison as they are not supplied by the API.
	Certificate string `json:"-" url:"clientCertificate,omitempty"`
	Key         string `json:"-" url:"clientKey,omitempty"`
}

type ReplicationType string

const (
	ReplicationTypeXMEM ReplicationType = "xmem"
)

type ReplicationReplicationType string // _sigh_

const (
	ReplicationReplicationTypeContinuous ReplicationReplicationType = "continuous"
)

// Replication describes an XDCR replication as set with /controller/createReplication.
// Note we unmarshall this explicitly...
type Replication struct {
	FromBucket       string                     `url:"fromBucket"`
	ToCluster        string                     `url:"toCluster"`
	ToBucket         string                     `url:"toBucket"`
	Type             ReplicationType            `url:"type"`
	ReplicationType  ReplicationReplicationType `url:"replicationType"`
	CompressionType  string                     `url:"compressionType,omitempty"`
	FilterExpression string                     `url:"filterExpression,omitempty"`
	PauseRequested   bool                       `url:"pauseRequested"`
	// These are subject to scopes and collections support in CBS 7+
	ExplicitMapping  bool `url:"collectionsExplicitMapping,omitempty"`
	MigrationMapping bool `url:"collectionsMigrationMode,omitempty"`
	// This is really a map of strings to strings or pointers to strings but easier to handle here this way then deal with explicitly in code.
	MappingRules string `url:"colMappingRules,omitempty"`
	Mobile       string `url:"mobile,omitempty"`

	// ConflictLogging is the configuration for the conflict logging.
	// only available in Couchbase Server 8.0.0 and later.
	ConflictLogging *ConflictLoggingSettings `json:"conflictLogging,omitempty" url:"conflictLogging,empty={}"`
}

type ReplicationList []Replication

// ReplicationSettings describes an XDCR replication settings as returned by
// GET /settings/replications/<remote UUID>/<local bucket>/<remote bucket>.
type ReplicationSettings struct {
	CompressionType string `json:"compressionType" url:"compressionType,omitempty"`
	PauseRequested  bool   `json:"pauseRequested" url:"pauseRequested"`
	// These are subject to scopes and collections support in CBS 7+
	ExplicitMapping  bool `json:"collectionsExplicitMapping,omitempty" url:"collectionsExplicitMapping,omitempty"`
	MigrationMapping bool `json:"collectionsMigrationMode,omitempty" url:"collectionsMigrationMode,omitempty"`
	// One API uses a map, the other wants it as a JSON-ified string already.
	MappingRules map[string]*string `json:"colMappingRules,omitempty" url:"colMappingRules,omitempty"`
	Mobile       string             `json:"mobile,omitempty" url:"mobile,omitempty"`

	// ConflictLogging is the configuration for the conflict logging.
	// only available in Couchbase Server 8.0.0 and later.
	ConflictLogging *ConflictLoggingSettings `json:"conflictLogging,omitempty" url:"conflictLogging,omitempty"`
}

// ConflictLoggingSettings is the configuration for the conflict logging.
// This object doesn't have a url tag because it's encoded as JSON and the json string is then
// encoded as a url parameter.
type ConflictLoggingSettings struct {
	Disabled   *bool  `json:"disabled,omitempty"`
	Bucket     string `json:"bucket,omitempty"`
	Collection string `json:"collection,omitempty"`

	// LoggingRules is a map of scope and collection to the location to log the conflicts.
	// If the location is nil then the conflicts are not logged.
	// If the location is empty then the conflicts are logged to the default location for the replication.
	// If the location is not nil and not empty then the conflicts are logged to the specified location.
	LoggingRules map[string]*ConflictLoggingLocation `json:"loggingRules,omitempty"`
}

func (c *ConflictLoggingSettings) String() string {
	jsonS, err := json.Marshal(c)
	if err != nil {
		return ""
	}

	return string(jsonS)
}

type ConflictLoggingLocation struct {
	Bucket     string `json:"bucket,omitempty" url:"bucket,omitempty"`
	Collection string `json:"collection,omitempty" url:"collection,omitempty"`
}

// Convert from one API call to the other.
func MappingRulesToStr(rules map[string]*string) (string, error) {
	if len(rules) > 0 {
		bytes, err := json.Marshal(rules)
		if err != nil {
			return "", err
		}

		return string(bytes), nil
	}

	return "", nil
}

// FormEncode represents user type in api compatible form.
func (u *User) FormEncode() []byte {
	data := url.Values{}

	if u.Password != "" {
		data.Set("password", u.Password)
	}

	roles := RolesToStr(u.Roles)

	data.Set("name", u.Name)
	data.Set("roles", strings.Join(roles, ","))
	data.Set("groups", strings.Join(u.Groups, ","))

	if u.Locked != nil {
		data.Set("locked", BoolAsStr(*u.Locked))
	}

	return []byte(data.Encode())
}

// RolesToStr translates roles to string array and sorts them.
func RolesToStr(userRoles []UserRole) []string {
	roles := []string{}

	for _, role := range userRoles {
		roles = append(roles, RoleToStr(role))
	}

	sort.Strings(roles)

	return roles
}

// RoleToStr  translates a single role to a string representation.
func RoleToStr(userRole UserRole) string {
	roleStr := userRole.Role

	switch {
	case userRole.IsCollectionRole():
		roleStr += fmt.Sprintf("[%s:%s:%s]", userRole.BucketName, userRole.ScopeName, userRole.CollectionName)
	case userRole.IsScopeRole():
		roleStr += fmt.Sprintf("[%s:%s]", userRole.BucketName, userRole.ScopeName)
	case userRole.IsBucketRole():
		roleStr += fmt.Sprintf("[%s]", userRole.BucketName)
	}

	return roleStr
}

// Normal unmarshlling doesn't work because LDAP DN Mapping returns a string when unset.
func (s *LDAPSettings) UnmarshalJSON(data []byte) error {
	type ls LDAPSettings

	var t struct {
		ls
		UserDNMapping interface{} `json:"userDNMapping"`
	}

	if err := json.Unmarshal(data, &t); err != nil {
		return err
	}

	*s = LDAPSettings(t.ls)

	// This is either "None" or an object #fml.
	// When not a string, allow a full unmarshalling of the data.
	if _, ok := t.UserDNMapping.(string); !ok {
		if err := json.Unmarshal(data, &t.ls); err != nil {
			return err
		}

		*s = LDAPSettings(t.ls)
	}

	return nil
}

func (s *LDAPSettings) FormEncode() ([]byte, error) {
	data := url.Values{}
	data.Set("hosts", strings.Join(s.Hosts, ","))
	data.Set("port", strconv.Itoa(s.Port))
	data.Set("bindDN", s.BindDN)
	data.Set("bindPass", s.BindPass)
	data.Set("authenticationEnabled", strconv.FormatBool(s.AuthenticationEnabled))
	data.Set("authorizationEnabled", strconv.FormatBool(s.AuthorizationEnabled))
	data.Set("encryption", string(s.Encryption))
	data.Set("serverCertValidation", strconv.FormatBool(s.EnableCertValidation))

	if s.MiddleboxCompMode != nil {
		data.Set("middleboxCompMode", strconv.FormatBool(*s.MiddleboxCompMode))
	}

	if s.EnableCertValidation {
		data.Set("cacert", s.CACert)
	}

	if s.AuthenticationEnabled {
		dnData, err := json.Marshal(s.UserDNMapping)
		if err != nil {
			return []byte{}, errors.NewStackTracedError(err)
		}

		data.Set("userDNMapping", string(dnData))
	}

	if s.AuthorizationEnabled {
		data.Set("groupsQuery", s.GroupsQuery)
		data.Set("nestedGroupsEnabled", strconv.FormatBool(s.NestedGroupsEnabled))
		data.Set("nestedGroupsMaxDepth", strconv.FormatUint(s.NestedGroupsMaxDepth, 10))
	}

	if s.CacheValueLifetime > 0 {
		data.Set("cacheValueLifetime", strconv.FormatUint(s.CacheValueLifetime, 10))
	}

	return []byte(data.Encode()), nil
}

// TLSVersion is a TLS version, as understood by the security API.
type TLSVersion string

const (
	// Insecure, do not use.
	TLS10 TLSVersion = "tlsv1"

	// Insecure, do not use.
	TLS11 TLSVersion = "tlsv1.1"

	// Obsolete, do not use.
	TLS12 TLSVersion = "tlsv1.2"

	// Latest and greatest.
	TLS13 TLSVersion = "tlsv1.3"
)

// ClusterEncryptionLevel is used to fully encrypt everything.
type ClusterEncryptionLevel string

const (
	// Encrypt all traffic.
	ClusterEncryptionAll ClusterEncryptionLevel = "all"

	// Encrypt only the control plane, allowing all your data to be evesdropped.
	// The implication here is performance sucks, so use with caution!
	ClusterEncryptionControl ClusterEncryptionLevel = "control"

	// Same as above, just disables plaintext ports too.
	ClusterEncryptionStrict ClusterEncryptionLevel = "strict"
)

// Security settings for the cluster.
type SecuritySettings struct {
	// Disallow access to web APIs over 8091.
	DisableUIOverHTTP bool `json:"disableUIOverHttp" url:"disableUIOverHttp"`

	// Disallow access to web APIs over 18091.
	DisableUIOverHTTPS bool `json:"disableUIOverHttps" url:"disableUIOverHttps"`

	// Set the minimum TLS version, should always be 1.2.
	TLSMinVersion TLSVersion `json:"tlsMinVersion" url:"tlsMinVersion"`

	// Cipher suites is a list of OpenSSL suites (openssl ciphers -v)
	CipherSuites []string `json:"cipherSuites" url:"cipherSuites"`

	// Choose the first suite that the client accepts.  This goes against
	// standard security practice in that you should always use the most
	// secure.  When in the hands of users this isn't the case...
	HonorCipherOrder bool `json:"honorCipherOrder" url:"honorCipherOrder"`

	// Enable cluster level encryption.
	ClusterEncryptionLevel ClusterEncryptionLevel `json:"clusterEncryptionLevel" url:"clusterEncryptionLevel,omitempty"`

	// Configures how long (in seconds) before inactive user is signed out of Server's UI.
	// This is actually an int but the API doesnt accept 0.
	// Instead we have to send "" to unset the timeout.
	UISessionTimeoutSeconds UISessionTimeoutInt `json:"uiSessionTimeout" url:"uiSessionTimeout,emptyisnull"`
}

// This is basically an int except 0 is sent as "" because couchbase api.
type UISessionTimeoutInt uint

func (timeout *UISessionTimeoutInt) UnmarshalJSON(b []byte) error {
	if b[0] != '"' {
		return json.Unmarshal(b, (*uint)(timeout))
	}

	var s string

	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	if s == "" {
		*timeout = UISessionTimeoutInt(0)
		return nil
	}

	i, err := strconv.Atoi(s)
	if err != nil {
		return err
	}

	*timeout = UISessionTimeoutInt(i)

	return nil
}

func (timeout *UISessionTimeoutInt) MarshalJSON() ([]byte, error) {
	var res []byte

	var err error

	if *timeout == UISessionTimeoutInt(0) {
		res, err = json.Marshal("")
	} else {
		res, err = json.Marshal((*uint)(timeout))
	}

	if err != nil {
		return nil, err
	}

	return res, nil
}

// I'm not even going to comment on this...
type OnOrOff string

const (
	On OnOrOff = "on"

	Off OnOrOff = "off"
)

// AddressFamily The address family to apply the settings.
type AddressFamily string

const (
	AddressFamilyIPV4 AddressFamily = "ipv4"

	AddressFamilyIPV6 AddressFamily = "ipv6"
)

// ConvertAddressFamilyOutToAddressFamily addresses the fact Server doesn't have
// any consistency when reading and writing the same value.
func (f AddressFamilyOut) ConvertAddressFamilyOutToAddressFamily() AddressFamily {
	switch f {
	case AddressFamilyOutInet:
		return AddressFamilyIPV4
	case AddressFamilyOutInet6:
		return AddressFamilyIPV6
	}

	return ""
}

// NodeNetworkConfiguration allows configuration of node networking for a specific address family.
// I can only guess this default to IPv4, which is fine.
type NodeNetworkConfiguration struct {
	// AddressFamily is the family of addresses to affect (IPv4 or IPv6 presumably).
	AddressFamily AddressFamily `json:"afamily" url:"afamily,omitempty"`

	AddressFamilyOnly *bool `url:"afamilyOnly,omitempty"`

	// NodeEncryption is whether encyryption is enabled for a node.
	NodeEncryption OnOrOff `json:"nodeEncryption" url:"nodeEncryption,omitempty"`
}

type ListenerConfiguration struct {
	// AddressFamily is the family of addresses to affect (IPv4 or IPv6 presumably).
	AddressFamily AddressFamily `json:"afamily" url:"afamily,omitempty"`

	// NodeEncryption is whether encyryption is enabled for a node.
	NodeEncryption OnOrOff `json:"nodeEncryption" url:"nodeEncryption,omitempty"`
}

// Client certificate authentication prefixes, used to extract the user name
// All fields must be specified, so no "omitempty" tags.
type ClientCertAuthPrefix struct {
	Path      string `json:"path"`
	Prefix    string `json:"prefix"`
	Delimiter string `json:"delimiter"`
}

// Client certificate authentication settings
// All fields must be specified, so no "omitempty" tags.
type ClientCertAuth struct {
	// Must be 'disable', 'enable', 'mandatory'
	State string `json:"state"`
	// Maximum of 10
	Prefixes []ClientCertAuthPrefix `json:"prefixes"`
}

const (
	// See QuerySettings.TemporarySpaceSize.
	QueryTemporarySpaceSizBackfillDisabled = 0
	QueryTemporarySpaceSizeUnlimited       = -1
)

type QueryLogLevel string

const (
	QueryLogLevelDebug  QueryLogLevel = "debug"
	QueryLogLevelTrace  QueryLogLevel = "trace"
	QueryLogLevelInfo   QueryLogLevel = "info"
	QueryLogLevelWarn   QueryLogLevel = "warn"
	QueryLogLevelError  QueryLogLevel = "error"
	QueryLogLevelSevere QueryLogLevel = "severe"
	QueryLogLevelNone   QueryLogLevel = "none"
)

type QueryUseReplica string

const (
	QueryUseReplicaOn    QueryUseReplica = "on"
	QueryUseReplicaOff   QueryUseReplica = "off"
	QueryUseReplicaUnset QueryUseReplica = "unset"
)

// QuerySettings allow the query service to be tweaked.
type QuerySettings struct {
	// TemporarySpaceSize is another classic bit of API design, where 0 means turn backfill
	//  off -- literally nothing to do with the size of something. -1 means unlimted, and
	// anything else is a size in MiB.
	TemporarySpaceSize int64 `json:"queryTmpSpaceSize" url:"queryTmpSpaceSize"`

	// Number of items execution operators can batch for Fetch from the KV.
	PipelineBatch int32 `json:"queryPipelineBatch" url:"queryPipelineBatch"`

	// Maximum number of items each execution operator can buffer between various operators.
	PipelineCap int32 `json:"queryPipelineCap" url:"queryPipelineCap"`

	// Maximum buffered channel size between the indexer client and the query service for index scans.
	ScanCap int32 `json:"queryScanCap" url:"queryScanCap"`

	// Maximum  time to spend on the request before timing out (ns).
	Timeout int64 `json:"queryTimeout" url:"queryTimeout"`

	// Maximum number of prepared statements in the cache. When this cache reaches the limit.
	PreparedLimit int32 `json:"queryPreparedLimit" url:"queryPreparedLimit"`

	// Number of requests to be logged in the completed requests catalog.
	CompletedLimit int32 `json:"queryCompletedLimit" url:"queryCompletedLimit"`

	// Duration in milliseconds. All completed queries lasting longer than this threshold are logged in the completed requests catalog.
	// Specify 0 to track all requests, independent of duration. Specify any negative number to track none.
	CompletedThreshold int32 `json:"queryCompletedThreshold" url:"queryCompletedThreshold"`

	// Query service log level.
	LogLevel QueryLogLevel `json:"queryLogLevel" url:"queryLogLevel"`

	// Specifies the maximum parallelism for queries on all Query nodes in the cluster.
	MaxParallelism int32 `json:"queryMaxParallelism" url:"queryMaxParallelism"`

	// Specifies the timeout for transaction requests.
	TxTimeout string `json:"queryTxTimeout" url:"queryTxTimeout"`

	// Specifies the maximum amount of memory a request may use.
	MemoryQuota int32 `json:"queryMemoryQuota" url:"queryMemoryQuota"`

	// Specifies whether the cost-based optimizer is enabled.
	CBOEnabled bool `json:"queryUseCBO" url:"queryUseCBO"`

	// Specifies whether the service will try to clean up transactions it has created.
	CleanupClientAttemptsEnabled bool `json:"queryCleanupClientAttempts" url:"queryCleanupClientAttempts"`

	// Specifies whether the Query service takes part in the distributed cleanup process.
	CleanupLostAttemptsEnabled bool `json:"queryCleanupLostAttempts" url:"queryCleanupLostAttempts"`

	// Specifies how frequently the Query service checks its subset of active transaction records for cleanup.
	CleanupWindow string `json:"queryCleanupWindow,omitempty" url:"queryCleanupWindow,omitempty"`

	// Specifies the total number of active transaction records for all Query nodes in the cluster.
	NumActiveTransactionRecords int32 `json:"queryNumAtrs" url:"queryNumAtrs"`

	// Sets the soft memory limit for every Query node in the cluster, in MB.
	NodeQuota *int32 `json:"queryNodeQuota,omitempty" url:"queryNodeQuota,omitempty"`

	// Specifies whether a query can fetch data from a replica vBucket if active vBuckets are inaccessible. The possible values are:
	// off — read from replica is disabled for all queries and cannot be overridden at request level.
	// on — read from replica is enabled for all queries, but can be disabled at request level.
	// unset — read from replica is enabled or disabled at request level.
	UseReplica *QueryUseReplica `json:"queryUseReplica,omitempty" url:"queryUseReplica,omitempty"`

	// The percentage of the queryNodeQuota that is dedicated to tracked value content memory across all active requests for every Query node.
	NodeQuotaValPercent *int32 `json:"queryNodeQuotaValPercent,omitempty" url:"queryNodeQuotaValPercent,omitempty"`

	// The number of CPUs the Query service can use on any Query node in the cluster.
	NumCpus *int32 `json:"queryNumCpus,omitempty" url:"queryNumCpus,omitempty"`

	// A plan size in bytes. Limits the size of query execution plans that can be logged in the completed requests catalog.
	CompletedMaxPlanSize *int32 `json:"queryCompletedMaxPlanSize,omitempty" url:"queryCompletedMaxPlanSize,omitempty"`

	// Stream size in MiB. Controls how much data about completed N1QL queries is saved to disk for analysis.
	// When > 0, saves query info to GZIP-compressed files with prefix local_request_log. Supports CB 8.0.0+.
	CompletedStreamSize *int32 `json:"queryCompletedStreamSize,omitempty" url:"queryCompletedStreamSize,omitempty"`
}

// All these settings are passed through with minimal or no verification.
type AuditSettings struct {
	// Whether to enable or disable auditing
	Enabled bool `json:"auditdEnabled" url:"auditdEnabled"`
	// The list of events ignored for auditing, note this must be marshalled if it is empty as we may want to say no events are disabled.
	DisabledEvents []int `json:"disabled" url:"disabled"`
	// The list of users to ignore for auditing, note this must be marshalled if it is empty as we may want to say no users are disabled.
	DisabledUsers []AuditUser `json:"disabledUsers" url:"disabledUsers"`
	// Rotation interval is the time in minutes for rotation
	RotateInterval int `json:"rotateInterval" url:"rotateInterval"`
	// The size in bytes of the audit log before it is rotated
	RotateSize int `json:"rotateSize" url:"rotateSize"`
	// Not to be set ever but we do have to make sure we default it correctly in case someone else sets it
	LogPath string `json:"logPath" url:"logPath"`
	// The number of seconds Couchbase Server keeps rotated audit logs.
	PruneAge *uint32 `json:"pruneAge,omitempty" url:"pruneAge"`
}

type AuditUser struct {
	Name   string `json:"name"`
	Domain string `json:"domain"`
}

// The MemcachedGlobals thread settings return nothing when unset, but remain set once any has been initialized.
// They can also be either a string or integer, depending on couchbase version and user option.
type MemcachedGlobals struct {
	ReaderThreads        *DataThreadSetting `json:"num_reader_threads,omitempty" url:"num_reader_threads,omitempty"`
	WriterThreads        *DataThreadSetting `json:"num_writer_threads,omitempty" url:"num_writer_threads,omitempty"`
	NumNonIOThreads      *DataThreadSetting `json:"num_nonio_threads,omitempty" url:"num_nonio_threads,omitempty"`
	NumAuxIOThreads      *DataThreadSetting `json:"num_auxio_threads,omitempty" url:"num_auxio_threads,omitempty"`
	TCPKeepAliveIdle     *int               `json:"tcp_keepalive_idle,omitempty" url:"tcp_keepalive_idle,omitempty"`
	TCPKeepAliveInterval *int               `json:"tcp_keepalive_interval,omitempty" url:"tcp_keepalive_interval,omitempty"`
	TCPKeepAliveProbes   *int               `json:"tcp_keepalive_probes,omitempty" url:"tcp_keepalive_probes,omitempty"`
	TCPUserTimeout       *int               `json:"tcp_user_timeout,omitempty" url:"tcp_user_timeout,omitempty"`
}

type AnalyticsSettings struct {
	NumReplicas *int `json:"numReplicas,omitempty" url:"numReplicas,omitempty"`
}

type DataThreadSetting struct {
	FixedVal *int
	Setting  *string
}

// UnmarshalJSON is used for custom unmarshalling of responses from the settings/memcached/global endpoint.
// The REST API will either return a string, float or nothing. This allows us to handle those cases consistently.
func (t *DataThreadSetting) UnmarshalJSON(data []byte) error {
	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	switch v := raw.(type) {
	case float64:
		intVal := int(v)
		t.FixedVal = &intVal
		t.Setting = nil
	case string:
		t.Setting = &v
		t.FixedVal = nil
	}

	return nil
}

// String returns the string representation of the DataThreadSetting.
// This allows it to be used directly in URL encoding.
func (t *DataThreadSetting) String() string {
	if t.FixedVal != nil {
		return strconv.Itoa(*t.FixedVal)
	}

	if t.Setting != nil {
		return *t.Setting
	}

	return ""
}

func (t *DataThreadSetting) ToIntOrString() *intstr.IntOrString {
	if t.FixedVal != nil {
		val := intstr.FromInt(*t.FixedVal)
		return &val
	}

	if t.Setting != nil {
		val := intstr.FromString(*t.Setting)
		return &val
	}

	return nil
}

func ToDataThreadSetting(intOrStr *intstr.IntOrString) *DataThreadSetting {
	if intOrStr == nil {
		return nil
	}

	if intOrStr.Type == intstr.Int {
		val := intOrStr.IntValue()

		return &DataThreadSetting{
			FixedVal: &val,
		}
	}

	val := intOrStr.String()

	return &DataThreadSetting{
		Setting: &val,
	}
}

// ScopeList defines all scopes for a bucket, and all nested collections.
type ScopeList struct {
	Scopes []Scope `json:"scopes"`
}

type Scope struct {
	Name        string       `json:"name"`
	Collections []Collection `json:"collections"`
}

type Collection struct {
	Name    string `json:"name" url:"name"`
	MaxTTL  *int   `json:"maxTTL" url:"maxTTL,omitempty"`
	History *bool  `json:"history" url:"history,omitempty"`
}

// 09/05/2023: collections are now mutateable.
// Uses a separate struct because only one field is mutable.
// Creating a separate struct for now.
type CollectionPatchRequest struct {
	History *bool `json:"history" url:"history,omitempty"`
	MaxTTL  *int  `json:"maxTTL" url:"maxTTL,omitempty"`
}

// HasScope determines whether the named scope exists in the list.
func (l ScopeList) HasScope(name string) bool {
	for _, scope := range l.Scopes {
		if scope.Name == name {
			return true
		}
	}

	return false
}

// GetScope returns the named configuration in lieu of an actual
// API not existing.
func (l ScopeList) GetScope(name string) Scope {
	for _, scope := range l.Scopes {
		if scope.Name == name {
			return scope
		}
	}

	return Scope{}
}

// HasCollection determines whether the named collection exists in the scope.
func (s Scope) HasCollection(name string) bool {
	for _, collection := range s.Collections {
		if collection.Name == name {
			return true
		}
	}

	return false
}

// GetCollection returns the collection if found.
func (s Scope) GetCollection(name string) Collection {
	for _, collection := range s.Collections {
		if collection.Name == name {
			return collection
		}
	}

	return Collection{}
}

// MetricsResponse (and it's derivatives) are the format of the JSON returned from the prometheus endpoints provided by CB Server 7.0.
// See: https://docs.couchbase.com/server/current/rest-api/rest-statistics-single.html#responses
type MetricsResponse struct {
	Data      []MetricsData  `json:"data"`
	Err       []MetricsError `json:"errors"`
	StartTime int            `json:"startTimestamp"`
	EndTime   int            `json:"endTimestamp"`
}

// MetricsError is an error returned from a metrics endpoint, containing the errored node, and an error message.
type MetricsError struct {
	Node string `json:"node"`
	Err  string `json:"error"`
}

// MetricsData is the meat of a metrics query, containing some metadata about the query, and the values for the metric.
type MetricsData struct {
	Metric MetricsInfo `json:"metric"`
	// Values is a 2D array containing timestamps and the metric value (in that order). This seems to return up to 7 values,
	// each is spaced 10 seconds apart, and are chronologically ordered.
	Values [][]interface{} `json:"values"`
}

// MetricsInfo contains metadata about the query.
type MetricsInfo struct {
	Nodes      []string `json:"nodes"`
	Bucket     string   `json:"bucket"`
	Collection string   `json:"collection"`
	Scope      string   `json:"scope"`
	MetricName string   `json:"name"`
}

// TrustedCAType represents the origin of a CA certificate.
type TrustedCAType string

const (
	// TrustedCAUploaded is a user provided CA.
	TrustedCAUploaded TrustedCAType = "uploaded"

	// TrustedCAGenerated is a self signed CA generated by CBS.
	TrustedCAGenerated TrustedCAType = "generated"
)

// TrustedCA is a CA certificate in CBS.
type TrustedCA struct {
	// ID a unique certificate ID used by the CBS API to manage it
	// e.g. delete it.
	ID int `json:"id"`

	// Type represents the origin of the CA.
	Type TrustedCAType `json:"type"`

	// PEM is the raw PEM encoded CA certificate.
	PEM string `json:"pem"`

	// Subject is the certiifcate subject.
	Subject string `json:"subject"`
}

// TrustedCAList is a list of CAs that CBS knows about.
type TrustedCAList []TrustedCA

// PrivateKeyPassphraseSettings used to register a passphrase via script or rest.
type PrivateKeyPassphraseSettings struct {
	PrivateKeyPassphrase PrivateKeyPassphrase `url:"privateKeyPassphrase" json:"privateKeyPassphrase"`
}

type PrivateKeyPassphrase struct {

	// Type of passphrase registration to use.
	Type string `url:"type" json:"type"`

	// Path to the script on the current node. must be in couchbase/scripts dir.
	Path string `json:"path,omitempty"`

	// Args is the list of arguments passed to the script.
	Args []string `json:"args,omitempty"`

	// Trim determines whether redundant characters are to be removed from the script.
	Trim bool `json:"trim,omitempty"`

	// URL is the endpoint to be called to retrieve the passphrase.
	// URL will be called using the GET method and may use http/https protocol.
	URL string `json:"url,omitempty"`

	// HttpOpts is a map of optional arguments to provide alongside the URL request.
	// When specified the key must be 'verifyPeer' as a boolean.
	HTTPOpts map[string]bool `json:"httpOpts,omitempty"`

	// Headers is a map of one or more key-value pairs to pass alongside the Get request.
	Headers map[string]string `json:"headers,omitempty"`

	// AddressFamily is the address family to use. By default inet (meaning IPV4) is used.
	AddressFamily string `json:"addressFamily,omitempty"`

	// Timeout is  the number of milliseconds that must elapse before the call is timed out.
	Timeout uint64 `json:"timeout,omitempty"`
}

type PodCreationResult struct {
	Err    error
	Member Member
}

type Metric struct {
	Nodes []string `json:"nodes"`
}

type StatsRangeMetricsData struct {
	Metric Metric          `json:"metric"`
	Values [][]interface{} `json:"values"`
}

type StatsRangeMetrics struct {
	Data           []StatsRangeMetricsData `json:"data"`
	Errors         []interface{}           `json:"errors"`
	StartTimestamp int                     `json:"startTimestamp"`
	EndTimestamp   int                     `json:"endTimestamp"`
}

type TerseClusterInfo struct {
	ClusterUUID          string `json:"clusterUUID"`
	Orchestrator         string `json:"orchestrator"`
	IsBalanced           bool   `json:"isBalanced"`
	ClusterCompatVersion string `json:"clusterCompatVersion"`
}

type RunningTasks []RunningTask

type RunningTask struct {
	StatusID              string `json:"statusId"`
	TaskType              string `json:"type"`
	SubType               string `json:"subtype"`
	Status                string `json:"status"`
	StatusIsStale         bool   `json:"statusIsStale"`
	MasterRequestTimedOut bool   `json:"masterRequestTimedOut"`
	LastReportURI         string `json:"lastReportURI"`
}

type XDCRConnectionPreCheckResponse struct {
	HostName string `json:"hostname"`
	TaskID   string `json:"taskId"`
	Username string `json:"username"`
}

// XdcrConnectionCheckResponse I hate this but that's what the API gives us :(.
type XdcrConnectionCheckResponse struct {
	Done   bool                           `json:"done"`
	Result map[string]map[string][]string `json:"result"`
	TaskID string                         `json:"taskId"`
}

// End of the formatting I hate

type DataServiceSettings struct {
	MinReplicasCount int `url:"minReplicasCount" json:"minReplicasCount"`
}

type RebalanceProgress struct {
	Status RebalanceStatus `json:"status"`
}

type ResourceManagementSettings struct {
	DiskUsage *DiskUsage `json:"diskUsage,omitempty"`
}

type DiskUsage struct {
	Enabled bool `json:"enabled"`
	Maximum *int `json:"maximum,omitempty"`
}

type AppTelemetrySettings struct {
	Enabled                 bool `json:"enabled"`
	MaxScrapeClientsPerNode int  `json:"maxScrapeClientsPerNode"`
	ScrapeIntervalSeconds   int  `json:"scrapeIntervalSeconds"`
}
