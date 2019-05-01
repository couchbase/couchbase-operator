package v2

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *CouchbaseCluster) AsOwner() metav1.OwnerReference {
	trueVar := true
	return metav1.OwnerReference{
		APIVersion: SchemeGroupVersion.String(),
		Kind:       ClusterCRDResourceKind,
		Name:       c.Name,
		UID:        c.UID,
		Controller: &trueVar,
	}
}

// Convert from typed to string
func (s Service) String() string {
	return string(s)
}

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

var SupportedFeatures = []string{
	FeatureAdmin,
	FeatureXDCR,
	FeatureClient,
}

// Contains returns true if a requested feature is enabled
func (efl ExposedFeatureList) Contains(feature string) bool {
	for _, f := range efl {
		if f == feature {
			return true
		}
	}
	return false
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

func (c *ClusterSpec) Cleanup() {

}

func (c *ClusterSpec) TotalSize() int {
	size := 0
	for _, server := range c.ServerSettings {
		size += server.Size
	}
	return size
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

func (tp *TLSPolicy) Validate() error {
	if tp.Static == nil {
		return nil
	}
	st := tp.Static

	if len(st.OperatorSecret) != 0 {
		if len(st.Member.ServerSecret) == 0 {
			return fmt.Errorf("operator secret set but member serverSecret not set")
		}
	} else if st.Member != nil && len(st.Member.ServerSecret) != 0 {
		return fmt.Errorf("member serverSecret set but operator secret not set")
	}
	return nil
}

func (tp *TLSPolicy) IsSecureClient() bool {
	if tp == nil || tp.Static == nil {
		return false
	}
	return len(tp.Static.OperatorSecret) != 0
}

func (m MemberTimestamp) Ts() time.Time {
	return time.Unix(m.timestamp, 0)
}

func NewMemberTimestamp(name string) MemberTimestamp {
	return MemberTimestamp{name, time.Now().Unix()}
}

func (l MemberStatusList) Contains(name string) bool {
	return l.GetMember(name) != nil
}

func (l MemberStatusList) GetMember(name string) *MemberTimestamp {
	for _, m := range l {
		if m.Name == name {
			return &m
		}
	}
	return nil
}

func (l MemberStatusList) Names() []string {
	names := []string{}
	for _, m := range l {
		names = append(names, m.Name)
	}
	return names
}

func (l *MemberStatusList) Add(name string) {
	if !l.Contains(name) {
		*l = append(*l, NewMemberTimestamp(name))
	}
}

// Set ready members from list.
func (ms *MembersStatus) SetReady(ready []string) {
	sort.Strings(ready)
	ms.Ready = MemberStatusList{}
	for _, m := range ready {
		ms.Ready.Add(m)
	}
}

// Set Unready members from list.
// If the member is already in the list
// then it's old timestamp is retained
func (ms *MembersStatus) SetUnready(unready []string) {
	sort.Strings(unready)
	unreadyList := MemberStatusList{}
	for _, m := range unready {
		if oldMember := ms.Unready.GetMember(m); oldMember != nil {
			unreadyList = append(unreadyList, *oldMember)
		} else {
			unreadyList.Add(m)
		}
	}
	ms.Unready = unreadyList
}

func (cs *ClusterStatus) SetVersion(v string) {
	cs.CurrentVersion = v
}

func (cs *ClusterStatus) IsFailed() bool {
	if cs == nil {
		return false
	}
	return cs.Phase == ClusterPhaseFailed
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

func (cs *ClusterStatus) SetBucketManagementFailedCondition(reason, message string) {
	c := newClusterCondition(v1.ConditionFalse, reason, message)
	cs.setClusterCondition(ClusterConditionManageBuckets, c)
}

func (cs *ClusterStatus) SetScalingUpCondition(from, to int) {
	c := newClusterCondition(v1.ConditionTrue, "Scaling up", scalingMsg(from, to))
	cs.setClusterCondition(ClusterConditionScaling, c)
}

func (cs *ClusterStatus) SetScalingDownCondition(from, to int) {
	c := newClusterCondition(v1.ConditionTrue, "Scaling down", scalingMsg(from, to))
	cs.setClusterCondition(ClusterConditionScaling, c)
}

func (cs *ClusterStatus) SetBalancedCondition() {
	c := newClusterCondition(v1.ConditionTrue, "Cluster is balanced",
		"Data is equally distributed across all nodes in the cluster")
	cs.setClusterCondition(ClusterConditionBalanced, c)
}

func (cs *ClusterStatus) SetUnbalancedCondition() {
	c := newClusterCondition(v1.ConditionFalse, "Cluster is unbalanced",
		"The operator is attempting to rebalance the data to correct this issue")
	cs.setClusterCondition(ClusterConditionBalanced, c)
}

func (cs *ClusterStatus) SetUnknownBalancedCondition() {
	c := newClusterCondition(v1.ConditionUnknown,
		"Unable to check balanced state", "Unable to determine if cluster is balanced")
	cs.setClusterCondition(ClusterConditionBalanced, c)
}

func (cs *ClusterStatus) SetUnavailableCondition(down []string) {
	c := newClusterCondition(v1.ConditionFalse, "Cluster partially available",
		fmt.Sprintf("The following nodes are down and not serving requests: %s", strings.Join(down, ", ")))
	cs.setClusterCondition(ClusterConditionAvailable, c)
}

func (cs *ClusterStatus) SetReadyCondition() {
	c := newClusterCondition(v1.ConditionTrue, "Cluster available", "")
	cs.setClusterCondition(ClusterConditionAvailable, c)
}

func (cs *ClusterStatus) SetConfigRejectedCondition(message string) {
	c := newClusterCondition(v1.ConditionFalse, "Cluster config is rejected", message)
	cs.setClusterCondition(ClusterConditionManageConfig, c)
}

func (cs *ClusterStatus) SetUpgradingCondition(status *UpgradeStatus) {
	c := newClusterCondition(v1.ConditionTrue, "Cluster upgrading", status.Format())
	cs.setClusterCondition(ClusterConditionUpgrading, c)
}

func (cs *ClusterStatus) ClearCondition(t ClusterConditionType) {
	if cs.Conditions == nil {
		cs.Conditions = ClusterStatusMap{}
	}
	delete(cs.Conditions, t)
}

func (cs *ClusterStatus) GetCondition(t ClusterConditionType) *ClusterCondition {
	if cs.Conditions == nil {
		return nil
	}
	return cs.Conditions[t]
}

func (cs *ClusterStatus) setClusterCondition(t ClusterConditionType, c *ClusterCondition) {
	if cs.Conditions == nil {
		cs.Conditions = ClusterStatusMap{}
	}
	if cp, ok := cs.Conditions[t]; ok {
		if cp.Status == c.Status && cp.Reason == c.Reason && cp.Message == c.Message {
			return
		}
	}
	cs.Conditions[t] = c
}

func newClusterCondition(status v1.ConditionStatus, reason, message string) *ClusterCondition {
	now := time.Now().Format(time.RFC3339)
	return &ClusterCondition{
		Status:             status,
		LastUpdateTime:     now,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}
}

func scalingMsg(from, to int) string {
	return fmt.Sprintf("Current cluster size: %d, desired cluster size: %d", from, to)
}

// Format creates an upgrade condition message.
func (status *UpgradeStatus) Format() string {
	return fmt.Sprintf(UpgradingMessageFormat, status.State, status.Source, status.Target, status.TargetCount, status.TotalCount)
}

// NewUpgradeStatus creates an UpgradeStatus from an upgrade condition message.
func NewUpgradeStatus(message string) *UpgradeStatus {
	status := &UpgradeStatus{}
	fmt.Sscanf(message, UpgradingMessageFormat, &status.State, &status.Source, &status.Target, &status.TargetCount, &status.TotalCount)
	return status
}
