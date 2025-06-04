package couchbaseutil

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const (
	adminPort    = 8091
	adminPortTLS = 18091
)

// A host name is Couchbase's idea of a host name, this is the DNS name and a port.
type HostName string

// GetMemberName extracts the Operator member/pod name from the Couchbase host name.
func (hostName HostName) GetMemberName() string {
	return strings.Split(string(hostName), ".")[0]
}

// GetOTPNode extracts the Couchbase OTP node names from the Couchbase host name.
func (hostName HostName) GetOTPNode() OTPNode {
	dnsName := strings.Split(string(hostName), ":")[0]
	return OTPNode(fmt.Sprintf("ns_1@%s", dnsName))
}

// HostNameList is a list of Couchbase host names.
type HostNameList []HostName

// OTPNodes translates between a Couchbase host name and an OTP node name.
// Used primarily for the shit show that is rebalance.
func (hostNames HostNameList) OTPNodes() OTPNodeList {
	otpNodes := make(OTPNodeList, len(hostNames))

	for i, hostName := range hostNames {
		otpNodes[i] = hostName.GetOTPNode()
	}

	return otpNodes
}

// An OTP node is a relic of exposing erlang via public APIs.
type OTPNode string

// OTPNodeList is a list of OTP nodes.
type OTPNodeList []OTPNode

// StringSlice converts from a list of OTP nodes to a slice of strings.  This should
// only be used the the API.
func (otpNodes OTPNodeList) StringSlice() []string {
	out := make([]string, len(otpNodes))

	for i, otpNode := range otpNodes {
		out[i] = string(otpNode)
	}

	return out
}

type Member interface { //nolint: interfacebloat
	Name() string
	Config() string
	UseTLS() bool
	SetVersion(string)
	Version() string
	SetDNSName(string)
	GetDNSName() string
	GetHostPort() string
	GetHostPortTLS() string
	GetHostURL() string
	GetHostURLPlaintext() string
	GetHostName() HostName
	GetOTPNode() OTPNode
}

// memberImpl is the core internal representation of a Couchbase server node.
type memberImpl struct {
	// name is the pod name of a member.
	name string

	// cluster is the Couchbase cluster a member belongs to.
	cluster string

	// namespace is the namespace the Couchbase cluster resides in.
	namespace string

	// config is the server class (spec.servers.name) the member belongs to.
	config string

	// useTLS defines whether this member will be communicated with over
	// plain text or TLS.
	useTLS bool

	// version is the Couchbase server version this member is using.
	version string

	// dnsHostName is the DNS name of the member if the default isn't used.
	dnsHostName string
}

// NewMember returns a new member that is created or managed by the operator
// as we are in possession of all the metadata that entails.
func NewMember(namespace, cluster, name, version, config string, useTLS bool) Member {
	return &memberImpl{
		namespace: namespace,
		cluster:   cluster,
		name:      name,
		version:   version,
		config:    config,
		useTLS:    useTLS,
	}
}

// NewPartialMember returns a new member that is only partially known about
// in that we don't know its configuration or how to communicate with it.
// What we do know is enough to address it and refer to it in Couchbase server.
func NewPartialMember(namespace, cluster, name string) Member {
	return &memberImpl{
		namespace: namespace,
		cluster:   cluster,
		name:      name,
	}
}

// NewMember returns a new member that is created or managed by the operator
// as we are in possession of all the metadata that entails.
func NewExtConnectedMember(namespace, cluster, name, version, config string, useTLS bool, hostname string) Member {
	return &memberImpl{
		namespace:   namespace,
		cluster:     cluster,
		name:        name,
		version:     version,
		config:      config,
		useTLS:      useTLS,
		dnsHostName: hostname,
	}
}

// SetVersion is used when a member has been created,
// but it's version has not yet been realised.
// i.e unknown SHA256 but we've queried the pod
// after launch to work out a version.
func (m *memberImpl) SetVersion(version string) {
	m.version = version
}

// SetDNSName is used to set the member's DNS name.
func (m *memberImpl) SetDNSName(hostname string) {
	m.dnsHostName = hostname
}

// GetDNSName returns the member's DNS name.  The host name is generated for an endpoint
// associated with a cluster-wide headless service.
func (m *memberImpl) GetDNSName() string {
	if m.dnsHostName != "" {
		return m.dnsHostName
	}

	return m.GetLocalDNSName()
}

func (m *memberImpl) GetLocalDNSName() string {
	return fmt.Sprintf("%s.%s.%s.svc", m.name, m.cluster, m.namespace)
}

// GetHostPort returns the member's host and port.  The port is dynamic based on the TLS
// configuration, if TLS is enabled, we will use the TLS admin port.
func (m *memberImpl) GetHostPort() string {
	return fmt.Sprintf("%s:%d", m.GetLocalDNSName(), m.clientPort())
}

// GetHostPortTLS is used to force the use of TLS, in particular for probing the TLS
// state before upgrading client connections.
func (m *memberImpl) GetHostPortTLS() string {
	return fmt.Sprintf("%s:18091", m.GetLocalDNSName())
}

// GetHostURL return the member's host URL (without a path).  The scheme and port are
// based on the TLS configuration, if TLS is enabled, we will use HTTPS and the TLS admin
// port.  This is how the Operator will communicate with Couchbase server.
func (m *memberImpl) GetHostURL() string {
	return fmt.Sprintf("%s://%s", m.clientScheme(), m.GetHostPort())
}

// GetHostURLPlaintext is for use when we need to force the use of HTTP, typically
// during TLS reconciliation where the TLS state would usually prohibit this.
func (m *memberImpl) GetHostURLPlaintext() string {
	return fmt.Sprintf("http://%s:8091", m.GetLocalDNSName())
}

// GetHostName returns what Couchbase calls a host name; a combination of DNS and port.
func (m *memberImpl) GetHostName() HostName {
	return HostName(fmt.Sprintf("%s:8091", m.GetDNSName()))
}

// GetOTPNode is an anachronism and is used to operator the Couchbase cluster even though
// nothing on the client ever refers to nodes this way!  While this can be mapped from a
// /pools/default, it's quicker and less error prone to just to procedurally generate it.
func (m *memberImpl) GetOTPNode() OTPNode {
	return OTPNode(fmt.Sprintf("ns_1@%s", m.GetDNSName()))
}

// clientScheme returns the URL scheme for a member dependant upon the TLS mode.
func (m *memberImpl) clientScheme() string {
	if m.useTLS {
		return "https"
	}

	return "http"
}

// clientPort returns the admin port dependant on the member TLS mode.
func (m *memberImpl) clientPort() int {
	if m.useTLS {
		return adminPortTLS
	}

	return adminPort
}

func (m *memberImpl) Name() string {
	return m.name
}

func (m *memberImpl) Config() string {
	if m.config == "" {
		return "unknown"
	}

	return m.config
}

func (m *memberImpl) Version() string {
	if m.version == "" {
		return "unknown"
	}

	return m.version
}

func (m *memberImpl) UseTLS() bool {
	return m.useTLS
}

// MemberSet is a mapping from member/pod name to the member.
type MemberSet map[string]Member

// NewMemberSet creates a new member set from the list of members.
func NewMemberSet(ms ...Member) MemberSet {
	res := MemberSet{}

	for _, m := range ms {
		res[m.Name()] = m
	}

	return res
}

// Contains returns whether a named member is part of the set.
func (ms MemberSet) Contains(name string) bool {
	_, ok := ms[name]
	return ok
}

func (ms MemberSet) ContainsOTP(node OTPNode) bool {
	for _, m := range ms {
		if m.GetOTPNode() == node {
			return true
		}
	}

	return false
}

// Copy clones a member set into a new map.
func (ms MemberSet) Copy() MemberSet {
	clone := MemberSet{}

	for k, v := range ms {
		clone[k] = v
	}

	return clone
}

// Diff returns the set of all members of the receiver that are not members of the
// specified member set.
func (ms MemberSet) Diff(other MemberSet) MemberSet {
	diff := MemberSet{}

	for n, m := range ms {
		if _, ok := other[n]; !ok {
			diff[n] = m
		}
	}

	return diff
}

func (ms MemberSet) Intersect(other MemberSet) MemberSet {
	intersection := MemberSet{}

	for n, m := range ms {
		if _, ok := other[n]; ok {
			intersection[n] = m
		}
	}

	return intersection
}

// Equal returns whether each member set contains the same members.  It is only a
// shallow compare, so member fields are not compared.
func (ms MemberSet) Equal(other MemberSet) bool {
	for n := range ms {
		if _, ok := other[n]; !ok {
			return false
		}
	}

	return true
}

// Size returns the member set size.
func (ms MemberSet) Size() int {
	return len(ms)
}

// Emptry returns whether the member set is empty.
func (ms MemberSet) Empty() bool {
	return len(ms) == 0
}

// Add adds a new member to the member set.
func (ms MemberSet) Add(m Member) {
	ms[m.Name()] = m
}

// Remove removes the named member from the member set.
func (ms MemberSet) Remove(name string) {
	delete(ms, name)
}

// MergeWithOverwrite adds the specified member set to the receiver.
func (ms MemberSet) MergeWithOverwrite(other MemberSet) {
	for _, m := range other {
		ms.Add(m)
	}
}

// Merges one set into another, retaining the original
// member if set.
func (ms MemberSet) Merge(other MemberSet) {
	for name, member := range other {
		if _, ok := ms[name]; !ok {
			ms.Add(member)
		}
	}
}

// Names returns a sorted list of member names.
func (ms MemberSet) Names() []string {
	names := []string{}

	for _, m := range ms {
		names = append(names, m.Name())
	}

	sort.Strings(names)

	return names
}

// OTPNodes returns the list of OTP nodes for a member set.
func (ms MemberSet) OTPNodes() OTPNodeList {
	otpNodes := make(OTPNodeList, 0, len(ms))

	for _, member := range ms {
		otpNodes = append(otpNodes, member.GetOTPNode())
	}

	return otpNodes
}

type FilterFunc func(Member) bool

func (ms MemberSet) GroupBy(f FilterFunc) MemberSet {
	rv := NewMemberSet()

	for _, m := range ms {
		if f(m) {
			rv.Add(m)
		}
	}

	return rv
}

// GroupByServerConfig filters members based on their server class.
func (ms MemberSet) GroupByServerConfig(config string) MemberSet {
	rv := NewMemberSet()

	for _, m := range ms {
		if m.Config() == config {
			rv.Add(m)
		}
	}

	return rv
}

func (ms MemberSet) GroupByServerConfigs() map[string]MemberSet {
	groupedMembers := map[string]MemberSet{}

	for _, m := range ms {
		if set, ok := groupedMembers[m.Config()]; ok {
			set.Add(m)
		} else {
			groupedMembers[m.Config()] = NewMemberSet(m)
		}
	}

	return groupedMembers
}

// CreateMemberName is a helper function that defines a member name based on
// cluster and numerical index.
func CreateMemberName(cluster string, member int) string {
	return fmt.Sprintf("%s-%04d", cluster, member)
}

func GetIndexFromMemberName(name string) (int, error) {
	// its just the last 4 digits...
	return strconv.Atoi(name[len(name)-4:])
}

func DoesMemberIndexExistInIndexes(indexes string, memberName string) bool {
	index, err := GetIndexFromMemberName(memberName)

	if err != nil {
		return false
	}

	return slices.Contains(strings.Split(indexes, ","), strconv.Itoa(index))
}

func AddMemberIndexToIndexList(indexes string, memberName string) (string, error) {
	index, err := GetIndexFromMemberName(memberName)

	if err != nil {
		return indexes, err
	}

	if index == 0 {
		indexes += strconv.Itoa(index)
	} else {
		indexes += "," + strconv.Itoa(index)
	}

	return indexes, nil
}

// this exists to allow testing.
func GetAvailableIndexes(names []string, num int) ([]int, error) {
	sort.Slice(names, func(i, j int) bool {
		return names[i] < names[j]
	})

	// since pods are in order, we basically expect the first one to be 0.
	// so count up until we hit the limit
	index := 0
	availableIndex := 0
	availableIndexes := []int{}

	for len(availableIndexes) < num && index < len(names) {
		usedIndex, err := GetIndexFromMemberName(names[index])
		if err != nil {
			return nil, err
		}

		if availableIndex < usedIndex {
			availableIndexes = append(availableIndexes, availableIndex)
			availableIndex++
		} else {
			availableIndex = usedIndex + 1
			index++
		}
	}

	// every number from here on is available
	for len(availableIndexes) < num {
		availableIndexes = append(availableIndexes, availableIndex)
		availableIndex++
	}

	return availableIndexes, nil
}

// NewExternalMember returns a new member that is external to the operator's control.
func NewExternalMember(host string, config string, useTLS bool) Member {
	return &externamMemberImpl{
		useTLS: useTLS,
		host:   host,
		config: config,
	}
}

type externamMemberImpl struct {
	useTLS bool
	host   string
	config string
}

func (m *externamMemberImpl) Name() string {
	return strings.Split(m.host, ".")[0]
}

func (m *externamMemberImpl) Config() string {
	if m.config == "" {
		return "unknown"
	}

	return m.config
}

func (m *externamMemberImpl) UseTLS() bool {
	return m.useTLS
}

func (m *externamMemberImpl) SetVersion(string) {
	return
}

func (m *externamMemberImpl) SetDNSName(string) {
	return
}

func (m *externamMemberImpl) Version() string {
	return "unknown"
}

func (m *externamMemberImpl) GetDNSName() string {
	return m.host
}

func (m *externamMemberImpl) GetHostPort() string {
	return fmt.Sprintf("%s:%d", m.GetDNSName(), m.clientPort())
}

func (m *externamMemberImpl) GetHostPortTLS() string {
	return fmt.Sprintf("%s:18091", m.GetDNSName())
}

func (m *externamMemberImpl) GetHostURL() string {
	return fmt.Sprintf("%s://%s", m.clientScheme(), m.GetHostPort())
}

// GetHostURLPlaintext is for use when we need to force the use of HTTP, typically
// during TLS reconciliation where the TLS state would usually prohibit this.
func (m *externamMemberImpl) GetHostURLPlaintext() string {
	return fmt.Sprintf("http://%s:8091", m.GetDNSName())
}

// GetHostName returns what Couchbase calls a host name; a combination of DNS and port.
func (m *externamMemberImpl) GetHostName() HostName {
	return HostName(fmt.Sprintf("%s:8091", m.GetDNSName()))
}

func (m *externamMemberImpl) GetOTPNode() OTPNode {
	return OTPNode(fmt.Sprintf("ns_1@%s", m.GetDNSName()))
}

func (m *externamMemberImpl) clientPort() int {
	if m.useTLS {
		return adminPortTLS
	}

	return adminPort
}

func (m *externamMemberImpl) clientScheme() string {
	if m.useTLS {
		return "https"
	}

	return "http"
}
