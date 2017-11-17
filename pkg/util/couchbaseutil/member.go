package couchbaseutil

import (
	"fmt"
	"strings"
)

type Member struct {
	Name         string
	Namespace    string
	SecureClient bool
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
	return fmt.Sprintf("%s://%s:8091", m.clientScheme(), m.Addr())
}

func (m *Member) clientScheme() string {
	if m.SecureClient {
		return "https"
	}
	return "http"
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
	for n, _ := range ms {
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

func (ms MemberSet) ClientURLs() []string {
	endpoints := make([]string, 0, len(ms))
	for _, m := range ms {
		endpoints = append(endpoints, m.ClientURL())
	}
	return endpoints
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
