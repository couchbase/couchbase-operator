package couchbaseutil

import (
	"fmt"
	"sort"
	"strings"
)

const (
	adminPort    = 8091
	adminPortTLS = 18091
)

type Member struct {
	Name         string
	Namespace    string
	ServerConfig string
	SecureClient bool
	Version      string
}

type MemberSet map[string]*Member

func NewMemberSet(ms ...*Member) MemberSet {
	res := MemberSet{}
	for _, m := range ms {
		res[m.Name] = m
	}
	return res
}

func (m *Member) Addr() string {
	return fmt.Sprintf("%s.%s.%s.svc", m.Name, clusterNameFromMemberName(m.Name), m.Namespace)
}

// ClientURL is the client URL for this member
func (m *Member) ClientURL() string {
	return fmt.Sprintf("%s://%s:%d", m.clientScheme(), m.Addr(), m.clientPort())
}

func (m *Member) HostURL() string {
	return fmt.Sprintf("%s:%d", m.Addr(), m.clientPort())
}

// Be **very** careful using these functions.
// NS server isn't able to handle /controller/addNode with a HTTPS/TLS node. As
// an unfortunate result when adding or removing nodes from the cluster we must
// force the use of HTTP.
// Regular client operations should use the functions above which dynamically
// adjust depending on the current node state.
func (m *Member) ClientURLPlaintext() string {
	return fmt.Sprintf("http://%s:%d", m.Addr(), adminPort)
}

func (m *Member) HostURLPlaintext() string {
	return fmt.Sprintf("%s:%d", m.Addr(), adminPort)
}

func (m *Member) HostURLTLS() string {
	return fmt.Sprintf("%s:%d", m.Addr(), adminPortTLS)
}

func (m *Member) clientScheme() string {
	if m.SecureClient {
		return "https"
	}
	return "http"
}

func (m *Member) clientPort() int {
	if m.SecureClient {
		return adminPortTLS
	}
	return adminPort
}

func (ms MemberSet) Contains(name string) bool {
	_, ok := ms[name]
	return ok
}

func (ms MemberSet) Copy() MemberSet {
	clone := MemberSet{}
	for k, v := range ms {
		clone[k] = v
	}
	return clone
}

// the set of all members of s1 that are not members of s2
func (ms MemberSet) Diff(other MemberSet) MemberSet {
	diff := MemberSet{}
	for n, m := range ms {
		if _, ok := other[n]; !ok {
			diff[n] = m
		}
	}
	return diff
}

func (ms MemberSet) Equal(other MemberSet) bool {
	for n := range ms {
		if _, ok := other[n]; !ok {
			return false
		}
	}
	return true
}

func (ms MemberSet) IsEqual(other MemberSet) bool {
	if ms.Size() != other.Size() {
		return false
	}
	for n := range ms {
		if _, ok := other[n]; !ok {
			return false
		}
	}
	return true
}

func (ms MemberSet) Size() int {
	return len(ms)
}

func (ms MemberSet) Empty() bool {
	return len(ms) == 0
}

func (ms MemberSet) Add(m *Member) {
	ms[m.Name] = m
}

func (ms MemberSet) Remove(name string) {
	delete(ms, name)
}

func (ms MemberSet) Append(other MemberSet) {
	for _, m := range other {
		ms.Add(m)
	}
}

// Names returns a sorted list of member names
func (ms MemberSet) Names() []string {
	names := []string{}
	for _, m := range ms {
		names = append(names, m.Name)
	}
	sort.Strings(names)
	return names
}

func (ms MemberSet) ClientURLs() []string {
	endpoints := make([]string, 0, len(ms))
	for _, m := range ms {
		endpoints = append(endpoints, m.ClientURL())
	}
	return endpoints
}

func (ms MemberSet) ClientURLsPlaintext() []string {
	endpoints := make([]string, 0, len(ms))
	for _, m := range ms {
		endpoints = append(endpoints, m.ClientURLPlaintext())
	}
	return endpoints
}

func (ms MemberSet) HostURLs() []string {
	endpoints := make([]string, 0, len(ms))
	for _, m := range ms {
		endpoints = append(endpoints, m.HostURL())
	}
	return endpoints
}

// See the documentation for HostURLPlaintext as to the intended use of
// this method.
func (ms MemberSet) HostURLsPlaintext() []string {
	endpoints := make([]string, 0, len(ms))
	for _, m := range ms {
		endpoints = append(endpoints, m.HostURLPlaintext())
	}
	return endpoints
}

func (ms MemberSet) GroupByServerConfig(config string) MemberSet {
	rv := NewMemberSet()
	for _, m := range ms {
		if m.ServerConfig == config {
			rv.Add(m)
		}
	}

	return rv
}

func (ms MemberSet) PickOne() *Member {
	for _, m := range ms {
		return m
	}
	panic("empty")
}

// retrieve the member with lowest index
func (ms MemberSet) First(clusterName string, max int) *Member {
	for i := 0; i < max; i++ {
		name := CreateMemberName(clusterName, i)
		m := ms[name]
		if m != nil {
			return m
		}
	}
	panic("empty")
}

func (ms MemberSet) Highest() *Member {
	if ms.Empty() {
		return nil
	}

	rv := ms.PickOne()
	for _, m := range ms {
		if strings.Compare(m.Name, rv.Name) > 0 {
			rv = m
		}
	}

	return rv
}

func CreateMemberName(clusterName string, member int) string {
	return fmt.Sprintf("%s-%04d", clusterName, member)
}

func clusterNameFromMemberName(mn string) string {
	i := strings.LastIndex(mn, "-")
	if i == -1 {
		panic(fmt.Sprintf("unexpected member name: %s", mn))
	}
	return mn[:i]
}

func (ms MemberSet) String() string {
	var mstring []string

	for m := range ms {
		mstring = append(mstring, m)
	}
	return strings.Join(mstring, ",")
}
