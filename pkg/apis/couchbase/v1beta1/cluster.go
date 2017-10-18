package v1beta1

import (
	"fmt"
	"reflect"
	"strings"
	"time"

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

type PVSource struct {
	// VolumeSizeInMB specifies the required volume size.
	VolumeSizeInMB int `json:"volumeSizeInMB"`

	// StorageClass indicates what Kubernetes storage class will be used.
	// This enables the user to have fine-grained control over how persistent
	// volumes are created since it uses the existing StorageClass mechanism in
	// Kubernetes.
	StorageClass string `json:"storageClass"`
}

type ClusterSpec struct {
	// Size is the expected size of the couchbase cluster. The
	// couchbase-operator will eventually make the size of the running
	// cluster equal to the expected size. The vaild range of the size is
	// from 1 to 50.
	Size int `json:"size"`

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
	// Pod defines the policy to create pod for the etcd pod.
	//
	// Updating Pod does not take effect on any existing couchbase pods.
	Pod *PodPolicy `json:"pod,omitempty"`

	// couchbase cluster TLS configuration
	TLS *TLSPolicy `json:"TLS,omitempty"`

	// Cluster specific settings
	ClusterSettings *ClusterConfig `json:"cluster"`

	// Bucket specific settings
	BucketSettings []BucketConfig `json:"buckets"`
}

type ClusterConfig struct {
	ClusterAuth

	// The services to run on each node in the cluster
	Services string `json:"services"`

	// The amount of memory that should be allocated to the data service
	DataServiceMemQuota int `json:"dataServiceMemoryQuota"`

	// The amount of memory that should be allocated to the index service
	IndexServiceMemQuota int `json:"indexServiceMemoryQuota"`

	// The amount of memory that should be allocated to the search service
	SearchServiceMemQuota int `json:"searchServiceMemoryQuota"`

	// The index storage mode to use for secondary indexing
	IndexStorageSetting string `json:"indexStorageSetting"`

	// The path on each node to store key-value data
	DataPath string `json:"dataPath"`

	// The path on each node to store index data
	IndexPath string `json:"indexPath"`

	// Timeout that expires to trigger the auto failover.
	AutoFailoverTimeout uint64 `json:"autoFailoverTimeout"`
}

type ClusterAuth struct {
	// The username for the Administrator user
	AdminUsername string `json:"username"`

	// The password for the Administrator user
	AdminPassword string `json:"password"`
}

type BucketConfig struct {
	// The bucket name
	BucketName string `json:"name"`

	// The type of bucket to use
	BucketType string `json:"type"`

	// The amount of memory that should be allocated to the bucket
	BucketMemoryQuota int `json:"memoryQuota"`

	// The number of bucket replicates
	BucketReplicas int `json:"replicas"`

	// The priority when compared to other buckets
	IoPriority string `json:"ioPriority"`

	// The bucket eviction policy which determines behavior during expire and high mem usage
	EvictionPolicy string `json:"evictionPolicy"`

	// The bucket's conflict resolution mechanism; which is to be used if a conflict occurs during Cross Data-Center Replication (XDCR). Sequence-based and timestamp-based mechanisms are supported.
	ConflictResolution string `json:"conflictResolution"`

	// The enable flush option denotes wether the data in the bucket can be flushed
	EnableFlush bool `json:"enableFlush"`

	// Enable Index replica specifies whether or not to enable view index replicas for this bucket. This parameter defaults to false if it is not specified. This parameter only affects Couchbase buckets.
	EnableIndexReplica bool `json:"enableIndexReplica"`
}

// PodPolicy defines the policy to create pod for the couchbase container.
type PodPolicy struct {
	// Labels specifies the labels to attach to pods the operator creates for the
	// couchbase cluster.
	// "app" and "couchbase_*" labels are reserved for the internal use of the couchbase operator.
	// Do not overwrite them.
	Labels map[string]string `json:"labels,omitempty"`

	// NodeSelector specifies a map of key-value pairs. For the pod to be eligible
	// to run on a node, the node must have each of the indicated key-value pairs as
	// labels.
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// AntiAffinity determines if the couchbase-operator tries to avoid putting
	// the couchbase members in the same cluster onto the same node.
	AntiAffinity bool `json:"antiAffinity,omitempty"`

	// Resources is the resource requirements for the couchbase container.
	// This field cannot be updated once the cluster is created.
	Resources v1.ResourceRequirements `json:"resources,omitempty"`

	// Tolerations specifies the pod's tolerations.
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`

	// List of environment variables to set in the etcd container.
	// This is used to configure etcd process. couchbase cluster cannot be created, when
	// bad environement variables are provided. Do not overwrite any flags used to
	// bootstrap the cluster (for example `--initial-cluster` flag).
	// This field cannot be updated.
	CouchbaseEnv []v1.EnvVar `json:"couchbaseEnv,omitempty"`

	// PV represents a Persistent Volume resource.
	// If defined new pods will use a persistent volume to store etcd data.
	// TODO(sgotti) unimplemented
	PV *PVSource `json:"pv,omitempty"`

	// By default, kubernetes will mount a service account token into the couchbase pods.
	// AutomountServiceAccountToken indicates whether pods running with the service account should have an API token automatically mounted.
	AutomountServiceAccountToken *bool `json:"automountServiceAccountToken,omitempty"`
}

func (c *ClusterSpec) Cleanup() {

}

type ClusterPhase string

const (
	ClusterPhaseNone     ClusterPhase = ""
	ClusterPhaseCreating              = "Creating"
	ClusterPhaseRunning               = "Running"
	ClusterPhaseFailed                = "Failed"
)

type ClusterCondition struct {
	Type ClusterConditionType `json:"type"`

	Reason string `json:"reason"`

	TransitionTime string `json:"transitionTime"`
}

type ClusterConditionType string

const (
	ClusterConditionReady = "Ready"

	ClusterConditionRemovingDeadMember = "RemovingDeadMember"

	ClusterConditionRecovering = "Recovering"

	ClusterConditionScalingUp   = "ScalingUp"
	ClusterConditionScalingDown = "ScalingDown"

	ClusterConditionUpgrading = "Upgrading"
)

type ClusterStatus struct {
	// Phase is the cluster running phase
	Phase  ClusterPhase `json:"phase"`
	Reason string       `json:"reason"`

	// ControlPuased indicates the operator pauses the control of the cluster.
	ControlPaused bool `json:"controlPaused"`

	// Condition keeps ten most recent cluster conditions
	Conditions []ClusterCondition `json:"conditions"`

	// A unique cluster identifier
	ClusterID string `json:"clusterId"`
	// Size is the current size of the cluster
	Size int `json:"size"`
	// Members are the couchbase members in the cluster
	Members MembersStatus `json:"members"`
	// CurrentVersion is the current cluster version
	CurrentVersion string `json:"currentVersion"`
	// TargetVersion is the version the cluster upgrading to.
	// If the cluster is not upgrading, TargetVersion is empty.
	TargetVersion string `json:"targetVersion"`

	// Name of buckets active within cluster
	Buckets map[string]*BucketConfig `json:"buckets"`
}

type MembersStatus struct {
	// Ready are the couchbase members that are ready to serve requests
	// The member names are the same as the couchbase pod names
	Ready []string `json:"ready,omitempty"`
	// Unready are the couchbase members not ready to serve requests
	Unready []string `json:"unready,omitempty"`
}

func (cl *CouchbaseCluster) Auth() (string, string) {
	auth := cl.Spec.ClusterSettings.ClusterAuth
	return auth.AdminUsername, auth.AdminPassword
}

func (cs *ClusterStatus) SetVersion(v string) {
	cs.TargetVersion = ""
	cs.CurrentVersion = v
}

func (cs *ClusterStatus) UpdateBuckets(name string, config *BucketConfig) {
	if cs.Buckets == nil {
		cs.Buckets = make(map[string]*BucketConfig)
	}

	cs.Buckets[name] = config
}

// get index of bucket name within status and remove
func (cs *ClusterStatus) RemoveBucket(b string) {
	if cs.Buckets == nil {
		cs.Buckets = make(map[string]*BucketConfig)
	}

	delete(cs.Buckets, b)
}

func (c *ClusterStatus) IsFailed() bool {
	return false
}

func (cs *ClusterStatus) SetPhase(p ClusterPhase) {
	cs.Phase = p
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

func (cs *ClusterStatus) SetReason(r string) {
	cs.Reason = r
}

func (cs *ClusterStatus) AppendScalingUpCondition(from, to int) {
	c := ClusterCondition{
		Type:           ClusterConditionScalingUp,
		Reason:         scalingReason(from, to),
		TransitionTime: time.Now().Format(time.RFC3339),
	}
	cs.appendCondition(c)
}

func (cs *ClusterStatus) AppendScalingDownCondition(from, to int) {
	c := ClusterCondition{
		Type:           ClusterConditionScalingDown,
		Reason:         scalingReason(from, to),
		TransitionTime: time.Now().Format(time.RFC3339),
	}
	cs.appendCondition(c)
}

func (cs *ClusterStatus) AppendRemovingDeadMember(name string) {
	reason := fmt.Sprintf("removing dead member %s", name)

	c := ClusterCondition{
		Type:           ClusterConditionRemovingDeadMember,
		Reason:         reason,
		TransitionTime: time.Now().Format(time.RFC3339),
	}
	cs.appendCondition(c)
}

func (cs *ClusterStatus) SetReadyCondition() {
	c := ClusterCondition{
		Type:           ClusterConditionReady,
		TransitionTime: time.Now().Format(time.RFC3339),
	}

	if len(cs.Conditions) == 0 {
		cs.appendCondition(c)
		return
	}

	lastc := cs.Conditions[len(cs.Conditions)-1]
	if lastc.Type == ClusterConditionReady {
		return
	}
	cs.appendCondition(c)
}

func (cs *ClusterStatus) appendCondition(c ClusterCondition) {
	cs.Conditions = append(cs.Conditions, c)
	if len(cs.Conditions) > 10 {
		cs.Conditions = cs.Conditions[1:]
	}
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

// diff spec and existing buckets to determine
// which should be added and which removed
func (cs *ClusterSpec) BucketDiff(existingBuckets []string) ([]string, []string) {
	specBuckets := cs.BucketNames()
	bucketsToAdd := MissingItems(specBuckets, existingBuckets)
	bucketsToRemove := MissingItems(existingBuckets, specBuckets)

	return bucketsToAdd, bucketsToRemove
}

// compare bucket status with new spec
// and return list of buckets that have changed
func (c *CouchbaseCluster) CompareBucketSpecs() []string {
	if c.Status.Buckets == nil {
		c.Status.Buckets = make(map[string]*BucketConfig)
	}

	bucketsChanged := []string{}
	specBuckets := c.Spec.BucketSettings
	statusBuckets := c.Status.Buckets

	for _, b := range specBuckets {
		if statusBucket, ok := statusBuckets[b.BucketName]; ok {
			if reflect.DeepEqual(*statusBucket, b) == false {
				bucketsChanged = append(bucketsChanged, b.BucketName)
			}
		}
	}
	return bucketsChanged
}

func (cc *ClusterConfig) ServicesArr() []string {
	return strings.Split(cc.Services, ",")
}
func scalingReason(from, to int) string {
	return fmt.Sprintf("Current cluster size: %d, desired cluster size: %d", from, to)
}

// check wether item exists within array
func HasItem(itm string, arr []string) (int, bool) {
	for i, a := range arr {
		if a == itm {
			return i, true
		}
	}
	return -1, false
}

// get list of items which are in first array but not in second
func MissingItems(a1, a2 []string) []string {
	missingItems := []string{}
	for _, a := range a1 {
		// checking if item from a1 is missing from a2
		if _, ok := HasItem(a, a2); ok == false {
			// add to missing
			missingItems = append(missingItems, a)
		}
	}
	return missingItems
}
