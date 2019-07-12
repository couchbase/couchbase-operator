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

// NewUser creates a new couchbase user
func NewUser(k8s *types.Cluster, namespace string, user *couchbasev2.CouchbaseUser) (*couchbasev2.CouchbaseUser, error) {
	return k8s.CRClient.CouchbaseV2().CouchbaseUsers(namespace).Create(user)
}

func MustNewUser(t *testing.T, k8s *types.Cluster, namespace string, user *couchbasev2.CouchbaseUser) *couchbasev2.CouchbaseUser {
	newUser, err := NewUser(k8s, namespace, user)
	if err != nil {
		Die(t, err)
	}
	return newUser
}

func DeleteUser(k8s *types.Cluster, namespace string, user *couchbasev2.CouchbaseUser) error {
	return k8s.CRClient.CouchbaseV2().CouchbaseUsers(namespace).Delete(user.Name, metav1.NewDeleteOptions(0))
}

func MustDeleteUser(t *testing.T, k8s *types.Cluster, namespace string, user *couchbasev2.CouchbaseUser) {
	if err := DeleteUser(k8s, namespace, user); err != nil {
		Die(t, err)
	}
}

// NewRole creates a new couchbase role
func NewRole(k8s *types.Cluster, namespace string, role *couchbasev2.CouchbaseRole) (*couchbasev2.CouchbaseRole, error) {
	return k8s.CRClient.CouchbaseV2().CouchbaseRoles(namespace).Create(role)
}

func MustNewRole(t *testing.T, k8s *types.Cluster, namespace string, role *couchbasev2.CouchbaseRole) *couchbasev2.CouchbaseRole {
	newRole, err := NewRole(k8s, namespace, role)
	if err != nil {
		Die(t, err)
	}
	return newRole
}

func DeleteRole(k8s *types.Cluster, namespace string, role *couchbasev2.CouchbaseRole) error {
	return k8s.CRClient.CouchbaseV2().CouchbaseRoles(namespace).Delete(role.Name, metav1.NewDeleteOptions(0))
}

func MustDeleteRole(t *testing.T, k8s *types.Cluster, namespace string, role *couchbasev2.CouchbaseRole) {
	if err := DeleteRole(k8s, namespace, role); err != nil {
		Die(t, err)
	}
}

// NewRoleBinding creates a new couchbase role binding
func NewRoleBinding(k8s *types.Cluster, namespace string, binding *couchbasev2.CouchbaseRoleBinding) (*couchbasev2.CouchbaseRoleBinding, error) {
	return k8s.CRClient.CouchbaseV2().CouchbaseRoleBindings(namespace).Create(binding)
}

func MustNewRoleBinding(t *testing.T, k8s *types.Cluster, namespace string, binding *couchbasev2.CouchbaseRoleBinding) *couchbasev2.CouchbaseRoleBinding {
	newBinding, err := NewRoleBinding(k8s, namespace, binding)
	if err != nil {
		Die(t, err)
	}
	return newBinding
}

func DeleteRoleBinding(k8s *types.Cluster, namespace string, binding *couchbasev2.CouchbaseRoleBinding) error {
	return k8s.CRClient.CouchbaseV2().CouchbaseRoleBindings(namespace).Delete(binding.Name, metav1.NewDeleteOptions(0))
}

func MustDeleteRoleBinding(t *testing.T, k8s *types.Cluster, namespace string, binding *couchbasev2.CouchbaseRoleBinding) {
	if err := DeleteRoleBinding(k8s, namespace, binding); err != nil {
		Die(t, err)
	}
}

// Patch CouchbaseRole
func PatchRole(k8s *types.Cluster, role *couchbasev2.CouchbaseRole, patches jsonpatch.PatchSet, timeout time.Duration) (*couchbasev2.CouchbaseRole, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return role, retryutil.Retry(ctx, 5*time.Second, func() (done bool, err error) {

		// get the role
		before, err := k8s.CRClient.CouchbaseV2().CouchbaseRoles(role.Namespace).Get(role.Name, metav1.GetOptions{})
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		// Apply the patch set to the role
		after := before.DeepCopy()
		if err := jsonpatch.Apply(after, patches.Patches()); err != nil {
			return false, retryutil.RetryOkError(err)
		}

		// If we are not modifiying e.g. just testing, then return ok
		if reflect.DeepEqual(before, after) {
			return true, nil
		}

		// Attempt to post the update, updating the role
		updated, err := k8s.CRClient.CouchbaseV2().CouchbaseRoles(role.Namespace).Update(after)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		role = updated

		// Everything successful
		return true, nil
	})
}

func MustPatchRole(t *testing.T, k8s *types.Cluster, role *couchbasev2.CouchbaseRole, patches jsonpatch.PatchSet, timeout time.Duration) *couchbasev2.CouchbaseRole {
	role, err := PatchRole(k8s, role, patches, timeout)
	if err != nil {
		Die(t, err)
	}
	return role
}

// Patch CouchbaseRoleBinding
func PatchRoleBinding(k8s *types.Cluster, binding *couchbasev2.CouchbaseRoleBinding, patches jsonpatch.PatchSet, timeout time.Duration) (*couchbasev2.CouchbaseRoleBinding, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return binding, retryutil.Retry(ctx, 5*time.Second, func() (done bool, err error) {

		// get the binding
		before, err := k8s.CRClient.CouchbaseV2().CouchbaseRoleBindings(binding.Namespace).Get(binding.Name, metav1.GetOptions{})
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		// Apply the patch set to the binding
		after := before.DeepCopy()
		if err := jsonpatch.Apply(after, patches.Patches()); err != nil {
			return false, retryutil.RetryOkError(err)
		}

		// If we are not modifiying e.g. just testing, then return ok
		if reflect.DeepEqual(before, after) {
			return true, nil
		}

		// Attempt to post the update, updating the binding
		updated, err := k8s.CRClient.CouchbaseV2().CouchbaseRoleBindings(binding.Namespace).Update(after)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		binding = updated

		// Everything successful
		return true, nil
	})
}

func MustPatchRoleBinding(t *testing.T, k8s *types.Cluster, binding *couchbasev2.CouchbaseRoleBinding, patches jsonpatch.PatchSet, timeout time.Duration) *couchbasev2.CouchbaseRoleBinding {
	binding, err := PatchRoleBinding(k8s, binding, patches, timeout)
	if err != nil {
		Die(t, err)
	}
	return binding
}
