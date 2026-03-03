/*
Copyright 2019-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2eutil

import (
	"context"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NewUser creates a new couchbase user.
func NewUser(k8s *types.Cluster, user *couchbasev2.CouchbaseUser) (*couchbasev2.CouchbaseUser, error) {
	return k8s.CRClient.CouchbaseV2().CouchbaseUsers(k8s.Namespace).Create(context.Background(), user, metav1.CreateOptions{})
}

func MustNewUser(t *testing.T, k8s *types.Cluster, user *couchbasev2.CouchbaseUser) *couchbasev2.CouchbaseUser {
	newUser, err := NewUser(k8s, user)
	if err != nil {
		Die(t, err)
	}

	return newUser
}

func DeleteUser(k8s *types.Cluster, user *couchbasev2.CouchbaseUser) error {
	return k8s.CRClient.CouchbaseV2().CouchbaseUsers(k8s.Namespace).Delete(context.Background(), user.Name, *metav1.NewDeleteOptions(0))
}

func MustDeleteUser(t *testing.T, k8s *types.Cluster, user *couchbasev2.CouchbaseUser) {
	if err := DeleteUser(k8s, user); err != nil {
		Die(t, err)
	}
}

// NewRole creates a new couchbase group.
func NewGroup(k8s *types.Cluster, group *couchbasev2.CouchbaseGroup) (*couchbasev2.CouchbaseGroup, error) {
	return k8s.CRClient.CouchbaseV2().CouchbaseGroups(k8s.Namespace).Create(context.Background(), group, metav1.CreateOptions{})
}

func MustNewGroup(t *testing.T, k8s *types.Cluster, group *couchbasev2.CouchbaseGroup) *couchbasev2.CouchbaseGroup {
	newGroup, err := NewGroup(k8s, group)
	if err != nil {
		Die(t, err)
	}

	return newGroup
}

func DeleteGroup(k8s *types.Cluster, group *couchbasev2.CouchbaseGroup) error {
	return k8s.CRClient.CouchbaseV2().CouchbaseGroups(k8s.Namespace).Delete(context.Background(), group.Name, *metav1.NewDeleteOptions(0))
}

func MustDeleteGroup(t *testing.T, k8s *types.Cluster, group *couchbasev2.CouchbaseGroup) {
	if err := DeleteGroup(k8s, group); err != nil {
		Die(t, err)
	}
}

// NewRoleBinding creates a new couchbase role binding.
func NewRoleBinding(k8s *types.Cluster, binding *couchbasev2.CouchbaseRoleBinding) (*couchbasev2.CouchbaseRoleBinding, error) {
	return k8s.CRClient.CouchbaseV2().CouchbaseRoleBindings(k8s.Namespace).Create(context.Background(), binding, metav1.CreateOptions{})
}

func MustNewRoleBinding(t *testing.T, k8s *types.Cluster, binding *couchbasev2.CouchbaseRoleBinding) *couchbasev2.CouchbaseRoleBinding {
	newBinding, err := NewRoleBinding(k8s, binding)
	if err != nil {
		Die(t, err)
	}

	return newBinding
}

func DeleteRoleBinding(k8s *types.Cluster, binding *couchbasev2.CouchbaseRoleBinding) error {
	return k8s.CRClient.CouchbaseV2().CouchbaseRoleBindings(k8s.Namespace).Delete(context.Background(), binding.Name, *metav1.NewDeleteOptions(0))
}

func MustDeleteRoleBinding(t *testing.T, k8s *types.Cluster, binding *couchbasev2.CouchbaseRoleBinding) {
	if err := DeleteRoleBinding(k8s, binding); err != nil {
		Die(t, err)
	}
}

// Patch CouchbaseGroup.
func PatchGroup(k8s *types.Cluster, group *couchbasev2.CouchbaseGroup, patches jsonpatch.PatchSet, timeout time.Duration) (*couchbasev2.CouchbaseGroup, error) {
	resource, err := patchResource(k8s, group, patches, timeout)
	if err != nil {
		return nil, err
	}

	return resource.(*couchbasev2.CouchbaseGroup), nil
}

func MustPatchGroup(t *testing.T, k8s *types.Cluster, group *couchbasev2.CouchbaseGroup, patches jsonpatch.PatchSet, timeout time.Duration) *couchbasev2.CouchbaseGroup {
	group, err := PatchGroup(k8s, group, patches, timeout)
	if err != nil {
		Die(t, err)
	}

	return group
}

// Patch CouchbaseRoleBinding.
func PatchRoleBinding(k8s *types.Cluster, binding *couchbasev2.CouchbaseRoleBinding, patches jsonpatch.PatchSet, timeout time.Duration) (*couchbasev2.CouchbaseRoleBinding, error) {
	resource, err := patchResource(k8s, binding, patches, timeout)
	if err != nil {
		return nil, err
	}

	return resource.(*couchbasev2.CouchbaseRoleBinding), nil
}

func MustPatchRoleBinding(t *testing.T, k8s *types.Cluster, binding *couchbasev2.CouchbaseRoleBinding, patches jsonpatch.PatchSet, timeout time.Duration) *couchbasev2.CouchbaseRoleBinding {
	binding, err := PatchRoleBinding(k8s, binding, patches, timeout)
	if err != nil {
		Die(t, err)
	}

	return binding
}
