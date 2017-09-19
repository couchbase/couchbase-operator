package couchbaseutil

import (
	"fmt"
)

type Member struct {
	Name string
	// Kubernetes namespace this member runs in.
	Namespace string
}

type MemberSet map[string]*Member

func NewMemberSet(ms ...*Member) MemberSet {
	res := MemberSet{}
	for _, m := range ms {
		res[m.Name] = m
	}
	return res
}

func CreateMemberName(clusterName string, member int) string {
	return fmt.Sprintf("%s-%04d", clusterName, member)
}
