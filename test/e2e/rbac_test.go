package e2e

import (
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	e2e_constants "github.com/couchbase/couchbase-operator/test/e2e/constants"

	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
)

// mustCreateBoundUser creates user bound to cluster and bucket admin roles
func mustCreateBoundUser(t *testing.T, k8s *types.Cluster, namespace string) (*couchbasev2.CouchbaseUser, *couchbasev2.CouchbaseRole, *couchbasev2.CouchbaseRoleBinding) {
	user := e2eutil.MustNewUser(t, k8s, namespace, e2espec.NewDefaultUser())
	role := e2eutil.MustNewRole(t, k8s, namespace, e2espec.NewClusterAdminRole())
	binding := e2eutil.MustNewRoleBinding(t, k8s, namespace, e2espec.NewClusterRoleBinding())
	return user, role, binding
}

// Create cluster with user and cluster admin binding
func TestRBACCreateAdminUser(t *testing.T) {
	// Plaform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 1

	// Create the cluster.
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, clusterSize)

	// Create user
	user, _, _ := mustCreateBoundUser(t, targetKube, f.Namespace)
	e2eutil.MustWaitUntilUserExists(t, targetKube, testCouchbase, user, 4*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * UserCreated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUserCreated},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestRBACDeleteUser verifies basic user deletion
func TestRBACDeleteUser(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)
	timeout := 4 * time.Minute

	// Create Cluster
	clusterSize := 1
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, clusterSize)

	// Create User
	user, _, _ := mustCreateBoundUser(t, targetKube, f.Namespace)
	e2eutil.MustWaitUntilUserExists(t, targetKube, testCouchbase, user, timeout)

	// Delete user deletion
	e2eutil.MustDeleteUser(t, targetKube, f.Namespace, user)
	e2eutil.MustWaitForUserDeletion(t, targetKube, f.Namespace, user.Name, timeout)

	event := k8sutil.UserDeleteEvent(user.Name, testCouchbase)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, event, timeout)

	// Check the events match what we expect:
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUserCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserDeleted},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestRBACDeleteRole verifies that deleting a role results in deleting User
func TestRBACDeleteRole(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	timeout := 4 * time.Minute

	// Create Cluster
	clusterSize := 1
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, clusterSize)

	// Create User
	user, role, _ := mustCreateBoundUser(t, targetKube, f.Namespace)
	e2eutil.MustWaitUntilUserExists(t, targetKube, testCouchbase, user, timeout)

	// Delete role and wait for user deletion from cluster
	e2eutil.MustDeleteRole(t, targetKube, f.Namespace, role)
	_ = e2eutil.MustWaitForClusterUserDeletion(t, targetKube, testCouchbase, user.Name, timeout)

	// Waiting for deletion event
	event := k8sutil.UserDeleteEvent(user.Name, testCouchbase)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, event, timeout)

	// Validation
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUserCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserDeleted},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestRBACUpdateRole changes cluster role to a bucket role and verifies
// reconciliation with couchbase
func TestRBACUpdateRole(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)
	timeout := 4 * time.Minute

	// Cluster
	clusterSize := 1
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, clusterSize)

	// User
	user, role, _ := mustCreateBoundUser(t, targetKube, f.Namespace)
	e2eutil.MustWaitUntilUserExists(t, targetKube, testCouchbase, user, timeout)

	// Change to bucket role user
	e2eutil.MustPatchRole(t, targetKube, role, jsonpatch.NewPatchSet().Replace("/Spec/Roles/0/Name", "bucket_admin"), time.Minute)
	e2eutil.MustPatchUserInfo(t, targetKube, testCouchbase, user.Name, user.Spec.AuthDomain, jsonpatch.NewPatchSet().Replace("/Roles/0/Role", "bucket_admin"), time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * UserCreated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUserCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserEdited},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestRBACRemoveUserFromBinding tests that a user is deleted
// when it is no longer referenced as a subject to any roles
//
// Test binds 2 users to the same role.  One of the user is
// removed from the binding and since it doesn't have a role
// in any other binding the user is also deleted
func TestRBACRemoveUserFromBinding(t *testing.T) {

	f := framework.Global
	targetKube := f.GetCluster(0)
	timeout := 4 * time.Minute

	// Cluster
	clusterSize := 1
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, clusterSize)

	// User
	user, _, binding := mustCreateBoundUser(t, targetKube, f.Namespace)
	e2eutil.MustWaitUntilUserExists(t, targetKube, testCouchbase, user, timeout)

	// Create another user
	customUser := e2espec.NewDefaultUser()
	customUser.Name = "alt-user"
	customUser = e2eutil.MustNewUser(t, targetKube, f.Namespace, customUser)

	// Add new user to role binding
	subject := couchbasev2.CouchbaseRoleBindingSubject{
		Kind: e2e_constants.CouchbaseSubjectUserKind,
		Name: customUser.Name,
	}
	e2eutil.MustPatchRoleBinding(t, targetKube, binding, jsonpatch.NewPatchSet().Add("/Spec/Subjects/1", subject), time.Minute)

	// New user is created
	e2eutil.MustWaitUntilUserExists(t, targetKube, testCouchbase, customUser, timeout)

	// Remove original user from binding
	e2eutil.MustPatchRoleBinding(t, targetKube, binding, jsonpatch.NewPatchSet().Remove("/Spec/Subjects/0"), time.Minute)
	_ = e2eutil.MustWaitForClusterUserDeletion(t, targetKube, testCouchbase, user.Name, timeout)

	// Waiting for deletion event
	event := k8sutil.UserDeleteEvent(user.Name, testCouchbase)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, event, timeout)

	// Check the events match what we expect:
	// * Cluster created
	// * UserCreated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUserCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserDeleted},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)

}

// TestRBACDeleteBinding tests that user is deleted when entire
// rolebinding is deleted
func TestRBACDeleteBinding(t *testing.T) {

	f := framework.Global
	targetKube := f.GetCluster(0)

	timeout := 4 * time.Minute

	// Create Cluster
	clusterSize := 1
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, clusterSize)

	// Create User
	user, _, binding := mustCreateBoundUser(t, targetKube, f.Namespace)
	e2eutil.MustWaitUntilUserExists(t, targetKube, testCouchbase, user, timeout)

	// Delete binding and wait for user deletion from cluster
	e2eutil.MustDeleteRoleBinding(t, targetKube, f.Namespace, binding)
	_ = e2eutil.MustWaitForClusterUserDeletion(t, targetKube, testCouchbase, user.Name, timeout)

	// Waiting for deletion event
	event := k8sutil.UserDeleteEvent(user.Name, testCouchbase)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, event, timeout)

	// Validation
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUserCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserDeleted},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}
