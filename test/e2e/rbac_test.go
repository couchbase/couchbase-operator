package e2e

import (
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	e2e_constants "github.com/couchbase/couchbase-operator/test/e2e/constants"

	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// mustCreateBoundUser creates user bound to cluster and bucket admin roles.
func mustCreateBoundUser(t *testing.T, k8s *types.Cluster) (*couchbasev2.CouchbaseUser, *couchbasev2.CouchbaseGroup, *couchbasev2.CouchbaseRoleBinding) {
	user := e2eutil.MustNewUser(t, k8s, e2espec.NewDefaultUser())
	group := e2eutil.MustNewGroup(t, k8s, e2espec.NewClusterAdminGroup())
	binding := e2eutil.MustNewRoleBinding(t, k8s, e2espec.NewClusterRoleBinding())

	return user, group, binding
}

// Create cluster with user and cluster admin binding.
func TestRBACCreateAdminUser(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipRBAC(t)

	// Static configuration.
	clusterSize := 1

	// Create the cluster.
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)

	// Create user
	user, _, _ := mustCreateBoundUser(t, targetKube)
	e2eutil.MustWaitUntilUserExists(t, targetKube, testCouchbase, user, 4*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * UserCreated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonGroupCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserCreated},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestRBACDeleteUser verifies basic user deletion.
func TestRBACDeleteUser(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipRBAC(t)

	timeout := 2 * time.Minute

	// Create Cluster
	clusterSize := 1
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)

	// Expect user delete event eventually to occur
	event := k8sutil.UserDeleteEvent(e2e_constants.CouchbaseUserName, testCouchbase)
	echan := e2eutil.WaitForPendingClusterEvent(targetKube, testCouchbase, event, timeout)

	defer echan.Cancel()

	// Create User
	user, _, _ := mustCreateBoundUser(t, targetKube)
	e2eutil.MustWaitUntilUserExists(t, targetKube, testCouchbase, user, timeout)

	// Delete user deletion
	e2eutil.MustDeleteUser(t, targetKube, user)
	_ = e2eutil.MustWaitForClusterUserDeletion(t, targetKube, testCouchbase, user.Name, timeout)

	// Ensure user delete event emitted
	e2eutil.MustReceiveErrorValue(t, echan)

	// Check the events match what we expect:
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonGroupCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserDeleted},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestRBACDeleteRole verifies that deleting a role results in deleting User.
func TestRBACDeleteRole(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipRBAC(t)

	timeout := 2 * time.Minute

	// Create Cluster
	clusterSize := 1
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)

	// Expect user delete event to occur
	event := k8sutil.UserDeleteEvent(e2e_constants.CouchbaseUserName, testCouchbase)
	echan := e2eutil.WaitForPendingClusterEvent(targetKube, testCouchbase, event, timeout)

	defer echan.Cancel()

	// Create User
	user, group, _ := mustCreateBoundUser(t, targetKube)
	e2eutil.MustWaitUntilUserExists(t, targetKube, testCouchbase, user, timeout)

	// Delete group and wait for user deletion from cluster
	e2eutil.MustDeleteGroup(t, targetKube, group)
	_ = e2eutil.MustWaitForClusterUserDeletion(t, targetKube, testCouchbase, user.Name, timeout)

	// Ensure user delete event emitted
	e2eutil.MustReceiveErrorValue(t, echan)

	// Validation
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonGroupCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserCreated},
		eventschema.Event{Reason: k8sutil.EventReasonGroupDeleted},
		eventschema.Event{Reason: k8sutil.EventReasonUserDeleted},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestRBACUpdateRole changes cluster role to a bucket role and verifies
// reconciliation with couchbase.
func TestRBACUpdateRole(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipRBAC(t)

	timeout := 2 * time.Minute

	// Cluster
	clusterSize := 1
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)

	// User
	user, group, _ := mustCreateBoundUser(t, targetKube)
	e2eutil.MustWaitUntilUserExists(t, targetKube, testCouchbase, user, timeout)

	// Change to bucket role user
	e2eutil.MustPatchGroup(t, targetKube, group, jsonpatch.NewPatchSet().Replace("/spec/roles/0/name", couchbasev2.RoleBucketAdmin), time.Minute)
	e2eutil.MustPatchUserInfo(t, targetKube, testCouchbase, user.Name, couchbaseutil.AuthDomain(user.Spec.AuthDomain), jsonpatch.NewPatchSet().Replace("/Roles/0/Role", string(couchbasev2.RoleBucketAdmin)), time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * UserCreated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonGroupCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserCreated},
		eventschema.Event{Reason: k8sutil.EventReasonGroupEdited},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestRBACRemoveUserFromBinding tests that a user is deleted
// when it is no longer referenced as a subject to any roles.
//
// Test binds 2 users to the same role.  One of the user is
// removed from the binding and since it doesn't have a role
// in any other binding the user is also deleted.
func TestRBACRemoveUserFromBinding(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipRBAC(t)

	timeout := 2 * time.Minute

	// Cluster
	clusterSize := 1
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)

	// User
	user, _, binding := mustCreateBoundUser(t, targetKube)
	e2eutil.MustWaitUntilUserExists(t, targetKube, testCouchbase, user, timeout)

	// Create another user
	customUser := e2espec.NewDefaultUser()
	customUser.Name = "alt-user"
	customUser = e2eutil.MustNewUser(t, targetKube, customUser)

	// Expect user delete event eventually occur
	event := k8sutil.UserDeleteEvent(user.Name, testCouchbase)
	echan := e2eutil.WaitForPendingClusterEvent(targetKube, testCouchbase, event, timeout)

	defer echan.Cancel()

	// Add new user to role binding
	subject := couchbasev2.CouchbaseRoleBindingSubject{
		Kind: e2e_constants.CouchbaseSubjectUserKind,
		Name: customUser.Name,
	}
	e2eutil.MustPatchRoleBinding(t, targetKube, binding, jsonpatch.NewPatchSet().Add("/spec/subjects/1", subject), time.Minute)

	// New user is created
	e2eutil.MustWaitUntilUserExists(t, targetKube, testCouchbase, customUser, timeout)

	// Remove original user from binding
	e2eutil.MustPatchRoleBinding(t, targetKube, binding, jsonpatch.NewPatchSet().Remove("/spec/subjects/0"), time.Minute)
	_ = e2eutil.MustWaitForClusterUserDeletion(t, targetKube, testCouchbase, user.Name, timeout)

	// Ensure user delete event emitted
	e2eutil.MustReceiveErrorValue(t, echan)

	// Check the events match what we expect:
	// * Cluster created
	// * UserCreated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonGroupCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserDeleted},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestRBACDeleteBinding tests that user is deleted when entire
// rolebinding is deleted.
func TestRBACDeleteBinding(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipRBAC(t)

	timeout := 2 * time.Minute

	// Create Cluster
	clusterSize := 1
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)

	// Expect user delete event to eventually occur
	event := k8sutil.UserDeleteEvent(e2e_constants.CouchbaseUserName, testCouchbase)
	echan := e2eutil.WaitForPendingClusterEvent(targetKube, testCouchbase, event, timeout)

	defer echan.Cancel()

	// Create User
	user, _, binding := mustCreateBoundUser(t, targetKube)
	e2eutil.MustWaitUntilUserExists(t, targetKube, testCouchbase, user, timeout)

	// Delete binding and wait for user deletion from cluster
	e2eutil.MustDeleteRoleBinding(t, targetKube, binding)
	_ = e2eutil.MustWaitForClusterUserDeletion(t, targetKube, testCouchbase, user.Name, timeout)

	// Ensure user delete event emitted
	e2eutil.MustReceiveErrorValue(t, echan)

	// Validation
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonGroupCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserDeleted},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Verify RBAC auth can be applied to LDAP users.
func TestRBACWithLDAPAuth(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipRBAC(t)

	// Static configuration.
	clusterSize := 1

	// Start LDAP service
	service := e2espec.NewLDAPService()
	_ = e2eutil.MustNewLDAPService(t, targetKube, service)

	// Start the LDAP server
	tlsOpts := &e2eutil.TLSOpts{
		AltNames: e2espec.LDAPAltNames(targetKube.Namespace),
	}

	ctx := e2eutil.MustInitLDAPTLS(t, targetKube, tlsOpts)

	pod := e2espec.NewLDAPServerTLS(targetKube.Namespace, ctx.ClusterSecretName)
	_ = e2eutil.MustNewLDAPServer(t, targetKube, pod)

	// Create a cluster with LDAP Auth
	testCouchbase := e2espec.NewLDAPClusterBasic(targetKube.Namespace, clusterSize, ctx.ClusterSecretName, targetKube.DefaultSecret.Name)
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Verify Connectivity
	e2eutil.MustCheckLDAPStatus(t, targetKube, testCouchbase, 2*time.Minute)

	// Validation
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestRBACSelection ensures the operator only creates users that match the
// label selector.
func TestRBACSelection(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipRBAC(t)

	// Static configuration.
	clusterSize := 1

	// Create first user
	user, group, binding := mustCreateBoundUser(t, targetKube)

	// Create second user with labels
	customUser := e2espec.NewDefaultUser()
	labels := map[string]string{
		"loves": "nala",
	}
	customUser.Name = "simba"
	customUser.Labels = labels
	customUser = e2eutil.MustNewUser(t, targetKube, customUser)

	// Patch group with labels
	e2eutil.MustPatchGroup(t, targetKube, group, jsonpatch.NewPatchSet().Add("/metadata/labels", labels), time.Minute)

	// Add second user to role binding
	subject := couchbasev2.CouchbaseRoleBindingSubject{
		Kind: e2e_constants.CouchbaseSubjectUserKind,
		Name: customUser.Name,
	}
	e2eutil.MustPatchRoleBinding(t, targetKube, binding, jsonpatch.NewPatchSet().Add("/spec/subjects/-", subject), time.Minute)

	// Create a cluster that selects only labelled users.
	couchbase := e2espec.NewBasicCluster(clusterSize)
	couchbase.Spec.Security.RBAC.Selector = &metav1.LabelSelector{
		MatchLabels: labels,
	}
	couchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, couchbase)

	// Ensure the unlabelled user doesn't get created.
	if err := e2eutil.WaitUntilUserExists(targetKube, couchbase, user, time.Minute); err == nil {
		e2eutil.Die(t, fmt.Errorf("user created unexpectedly"))
	}

	// Second user is created
	e2eutil.MustWaitUntilUserExists(t, targetKube, couchbase, customUser, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * UserCreated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonGroupCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserCreated},
	}
	ValidateEvents(t, targetKube, couchbase, expectedEvents)
}
