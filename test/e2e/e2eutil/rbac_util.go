package e2eutil

import (
	"context"
	"reflect"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
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
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return group, retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		// get the group
		before, err := k8s.CRClient.CouchbaseV2().CouchbaseGroups(group.Namespace).Get(context.Background(), group.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		// Apply the patch set to the group
		after := before.DeepCopy()
		if err := jsonpatch.Apply(after, patches.Patches()); err != nil {
			return err
		}

		// If we are not modifiying e.g. just testing, then return ok
		if reflect.DeepEqual(before, after) {
			return nil
		}

		// Attempt to post the update, updating the group
		updated, err := k8s.CRClient.CouchbaseV2().CouchbaseGroups(group.Namespace).Update(context.Background(), after, metav1.UpdateOptions{})
		if err != nil {
			return err
		}

		group = updated

		// Everything successful
		return nil
	})
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
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return binding, retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		// get the binding
		before, err := k8s.CRClient.CouchbaseV2().CouchbaseRoleBindings(binding.Namespace).Get(context.Background(), binding.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		// Apply the patch set to the binding
		after := before.DeepCopy()
		if err := jsonpatch.Apply(after, patches.Patches()); err != nil {
			return err
		}

		// If we are not modifiying e.g. just testing, then return ok
		if reflect.DeepEqual(before, after) {
			return nil
		}

		// Attempt to post the update, updating the binding
		updated, err := k8s.CRClient.CouchbaseV2().CouchbaseRoleBindings(binding.Namespace).Update(context.Background(), after, metav1.UpdateOptions{})
		if err != nil {
			return err
		}

		binding = updated

		// Everything successful
		return nil
	})
}

func MustPatchRoleBinding(t *testing.T, k8s *types.Cluster, binding *couchbasev2.CouchbaseRoleBinding, patches jsonpatch.PatchSet, timeout time.Duration) *couchbasev2.CouchbaseRoleBinding {
	binding, err := PatchRoleBinding(k8s, binding, patches, timeout)
	if err != nil {
		Die(t, err)
	}

	return binding
}
