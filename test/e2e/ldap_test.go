package e2e

import (
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
)

// mustCreateBoundUser creates user bound to cluster and bucket admin roles.
func mustCreateLDAPBoundUser(t *testing.T, k8s *types.Cluster) (*couchbasev2.CouchbaseUser, *couchbasev2.CouchbaseGroup, *couchbasev2.CouchbaseRoleBinding) {
	user := e2eutil.MustNewUser(t, k8s, e2espec.NewDefaultLDAPUser())
	group := e2eutil.MustNewGroup(t, k8s, e2espec.NewClusterAdminGroup())
	bindSpec := e2espec.NewRoleBinding(e2e_constants.RoleBindingName, []string{user.Name}, group.Name)
	binding := e2eutil.MustNewRoleBinding(t, k8s, bindSpec)

	return user, group, binding
}

func setupLDAP(t *testing.T, k8s *types.Cluster) *couchbasev2.CouchbaseCluster {
	// Static configuration.
	clusterSize := 1

	// Start LDAP service
	service := e2espec.NewLDAPService()
	_ = e2eutil.MustNewLDAPService(t, k8s, service)

	// Start the LDAP server
	tlsOpts := &e2eutil.TLSOpts{
		AltNames: e2espec.LDAPAltNames(k8s.Namespace),
	}
	ctx := e2eutil.MustInitLDAPTLS(t, k8s, tlsOpts)

	pod := e2espec.NewLDAPServerTLS(k8s.Namespace, ctx.ClusterSecretName)
	_ = e2eutil.MustNewLDAPServer(t, k8s, pod)

	e2eutil.MustCheckLDAPServer(t, k8s, pod.Name, ctx, 10*time.Minute)

	// Create a cluster with LDAP Auth
	cluster := e2espec.NewLDAPClusterBasic(clusterOptions().WithEphemeralTopology(clusterSize).Options, k8s.Namespace, ctx.ClusterSecretName, k8s.DefaultSecret.Name)
	cluster = e2eutil.MustNewClusterFromSpec(t, k8s, cluster)
	e2eutil.MustWaitClusterStatusHealthy(t, k8s, cluster, 5*time.Minute)

	// Verify Connectivity
	e2eutil.MustCheckLDAPStatus(t, k8s, cluster, 2*time.Minute)

	return cluster
}

func TestLDAPCreateAdminUser(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 1

	cluster := setupLDAP(t, kubernetes)

	// Create LDAP User with Cluster admin privileges
	user, _, _ := mustCreateLDAPBoundUser(t, kubernetes)
	e2eutil.MustWaitUntilUserExists(t, kubernetes, cluster, user, 4*time.Minute)

	// Validation
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonGroupCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserCreated},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestRBACDeleteUser verifies basic user deletion.
func TestLDAPDeleteUser(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 1
	cluster := setupLDAP(t, kubernetes)
	timeout := 2 * time.Minute

	// Expect user delete event eventually to occur
	event := k8sutil.UserDeleteEvent(e2e_constants.CouchbaseLDAPUserName, cluster)
	echan := e2eutil.WaitForPendingClusterEvent(kubernetes, cluster, event, timeout)

	defer echan.Cancel()

	// Create User
	user, _, _ := mustCreateLDAPBoundUser(t, kubernetes)
	e2eutil.MustWaitUntilUserExists(t, kubernetes, cluster, user, timeout)

	// Delete user deletion
	e2eutil.MustDeleteUser(t, kubernetes, user)
	_ = e2eutil.MustWaitForClusterUserDeletion(t, kubernetes, cluster, user.Name, timeout)

	// Ensure user delete event emitted
	e2eutil.MustReceiveErrorValue(t, echan)

	// Check the events match what we expect:
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonGroupCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserDeleted},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestLDAPDeleteRole verifies that deleting a group results in deleting User.
func TestLDAPDeleteRole(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 1
	cluster := setupLDAP(t, kubernetes)
	timeout := 2 * time.Minute

	// Expect user delete event to occur
	event := k8sutil.UserDeleteEvent(e2e_constants.CouchbaseLDAPUserName, cluster)
	echan := e2eutil.WaitForPendingClusterEvent(kubernetes, cluster, event, timeout)

	defer echan.Cancel()

	// Create User
	user, group, _ := mustCreateLDAPBoundUser(t, kubernetes)
	e2eutil.MustWaitUntilUserExists(t, kubernetes, cluster, user, timeout)

	// Delete group and wait for user deletion from cluster
	e2eutil.MustDeleteGroup(t, kubernetes, group)
	_ = e2eutil.MustWaitForClusterUserDeletion(t, kubernetes, cluster, user.Name, timeout)

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
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestLDAPUpdateRole changes cluster role to a bucket role and verifies
// reconciliation with couchbase.
func TestLDAPUpdateRole(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	timeout := 2 * time.Minute

	// Cluster
	clusterSize := 1
	cluster := setupLDAP(t, kubernetes)

	// User
	user, group, _ := mustCreateLDAPBoundUser(t, kubernetes)
	e2eutil.MustWaitUntilUserExists(t, kubernetes, cluster, user, timeout)

	// Change to bucket role user
	e2eutil.MustPatchGroup(t, kubernetes, group, jsonpatch.NewPatchSet().Replace("/spec/roles/0/name", couchbasev2.RoleBucketAdmin), time.Minute)
	e2eutil.MustPatchUserInfo(t, kubernetes, cluster, user.Name, couchbaseutil.AuthDomain(user.Spec.AuthDomain), jsonpatch.NewPatchSet().Replace("/Roles/0/Role", string(couchbasev2.RoleBucketAdmin)), time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * UserCreated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonGroupCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserCreated},
		eventschema.Event{Reason: k8sutil.EventReasonGroupEdited},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestLDAPRemoveUserFromBinding tests that a user is deleted
// when it is no longer referenced as a subject to any roles.
//
// Test binds 2 users to the same role.  One of the user is
// removed from the binding and since it doesn't have a role
// in any other binding the user is also deleted.
func TestLDAPRemoveUserFromBinding(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	timeout := 2 * time.Minute

	// Cluster
	clusterSize := 1
	cluster := setupLDAP(t, kubernetes)

	// User
	user, _, binding := mustCreateLDAPBoundUser(t, kubernetes)
	e2eutil.MustWaitUntilUserExists(t, kubernetes, cluster, user, timeout)

	// Create another user
	customUser := e2espec.NewDefaultLDAPUser()
	customUser.Name = "alt-user"
	customUser = e2eutil.MustNewUser(t, kubernetes, customUser)

	// Expect user delete event eventually occur
	event := k8sutil.UserDeleteEvent(user.Name, cluster)
	echan := e2eutil.WaitForPendingClusterEvent(kubernetes, cluster, event, timeout)

	defer echan.Cancel()

	// Add new user to role binding
	subject := couchbasev2.CouchbaseRoleBindingSubject{
		Kind: e2e_constants.CouchbaseSubjectUserKind,
		Name: customUser.Name,
	}
	e2eutil.MustPatchRoleBinding(t, kubernetes, binding, jsonpatch.NewPatchSet().Add("/spec/subjects/1", subject), time.Minute)

	// New user is created
	e2eutil.MustWaitUntilUserExists(t, kubernetes, cluster, customUser, timeout)

	// Remove original user from binding
	e2eutil.MustPatchRoleBinding(t, kubernetes, binding, jsonpatch.NewPatchSet().Remove("/spec/subjects/0"), time.Minute)
	_ = e2eutil.MustWaitForClusterUserDeletion(t, kubernetes, cluster, user.Name, timeout)

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
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestLDAPDeleteBinding tests that user is deleted when entire
// rolebinding is deleted.
func TestLDAPDeleteBinding(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	timeout := 2 * time.Minute

	// Create Cluster
	clusterSize := 1
	cluster := setupLDAP(t, kubernetes)

	// Expect user delete event to eventually occur
	event := k8sutil.UserDeleteEvent(e2e_constants.CouchbaseLDAPUserName, cluster)
	echan := e2eutil.WaitForPendingClusterEvent(kubernetes, cluster, event, timeout)

	defer echan.Cancel()

	// Create User
	user, _, binding := mustCreateLDAPBoundUser(t, kubernetes)
	e2eutil.MustWaitUntilUserExists(t, kubernetes, cluster, user, timeout)

	// Delete binding and wait for user deletion from cluster
	e2eutil.MustDeleteRoleBinding(t, kubernetes, binding)
	_ = e2eutil.MustWaitForClusterUserDeletion(t, kubernetes, cluster, user.Name, timeout)

	// Ensure user delete event emitted
	e2eutil.MustReceiveErrorValue(t, echan)

	// Validation
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonGroupCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserDeleted},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestVerifyLDAPConfigRetention tests that LDAP configuration is retained when RBAC is unmanaged.
func TestVerifyLDAPConfigRetention(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	timeout := 2 * time.Minute

	// Create Cluster
	clusterSize := 1
	cluster := setupLDAP(t, kubernetes)

	// Expect user delete event to eventually occur
	event := k8sutil.UserDeleteEvent(e2e_constants.CouchbaseLDAPUserName, cluster)
	echan := e2eutil.WaitForPendingClusterEvent(kubernetes, cluster, event, timeout)

	defer echan.Cancel()

	// Create User
	user, _, _ := mustCreateLDAPBoundUser(t, kubernetes)
	e2eutil.MustWaitUntilUserExists(t, kubernetes, cluster, user, timeout)

	// Check that couchbase LDAP url setting exists
	e2eutil.MustVerifyLDAPConfigured(t, kubernetes, cluster)

	// Set RBAC to unmanaged
	patches := jsonpatch.NewPatchSet().Replace("/spec/security/rbac/managed", false)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patches, time.Minute)

	// Verify that LDAP url settings still exist
	e2eutil.MustCheckLDAPSettingsPersisted(t, kubernetes, cluster, 15*time.Second)

	// Remove LDAP settings
	patches = jsonpatch.NewPatchSet().Remove("/spec/security/ldap")
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patches, time.Minute)

	// Verify that LDAP url settings still exist
	e2eutil.MustCheckLDAPSettingsPersisted(t, kubernetes, cluster, 15*time.Second)

	// Validation
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonGroupCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserCreated},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestVerifyLDAPManualManagement tests that LDAP can be manually managed by user while
// RBAC is fully managed by the operator.
func TestVerifyLDAPManualManagement(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	timeout := 2 * time.Minute

	// Create Cluster
	clusterSize := 1
	cluster := setupLDAP(t, kubernetes)

	// Expect user delete event to eventually occur
	event := k8sutil.UserDeleteEvent(e2e_constants.CouchbaseLDAPUserName, cluster)
	echan := e2eutil.WaitForPendingClusterEvent(kubernetes, cluster, event, timeout)

	defer echan.Cancel()

	// Create User
	user, _, _ := mustCreateLDAPBoundUser(t, kubernetes)
	e2eutil.MustWaitUntilUserExists(t, kubernetes, cluster, user, timeout)

	// Check that couchbase LDAP url setting exists
	e2eutil.MustVerifyLDAPConfigured(t, kubernetes, cluster)

	// Remove LDAP settings to allow for manual management.
	patches := jsonpatch.NewPatchSet().Remove("/spec/security/ldap")
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patches, time.Minute)

	// Verify that the operator does not attempt to sync the k8s LDAP settings with server.
	// This implies manual management is in check.
	e2eutil.MustCheckLDAPSettingsPersisted(t, kubernetes, cluster, 15*time.Second)

	// Validation
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonGroupCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserCreated},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
