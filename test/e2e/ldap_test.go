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

func skipLDAP(t *testing.T) {
	tag, err := k8sutil.CouchbaseVersion(framework.Global.CouchbaseServerImage)

	if err != nil {
		e2eutil.Die(t, err)
	}

	version, err := couchbaseutil.NewVersion(tag)

	if err != nil {
		e2eutil.Die(t, err)
	}

	threshold, _ := couchbaseutil.NewVersion("6.5.0")

	if version.Less(threshold) {
		t.Skip("Unsupported couchbase version: LDAP requires >6.5.0")
	}
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

	pod := e2espec.NewLDAPServerTLS(k8s.Namespace, ctx.LDAPSecretName)
	_ = e2eutil.MustNewLDAPServer(t, k8s, pod)

	e2eutil.MustCheckLDAPServer(t, k8s, pod.Name, ctx, 5*time.Minute)

	// Create a cluster with LDAP Auth
	testCouchbase := e2espec.NewLDAPClusterBasic(k8s.Namespace, clusterSize, ctx.LDAPSecretName, k8s.DefaultSecret.Name)
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, k8s, testCouchbase)
	e2eutil.MustWaitClusterStatusHealthy(t, k8s, testCouchbase, 5*time.Minute)

	// Verify Connectivity
	e2eutil.MustCheckLDAPStatus(t, k8s, testCouchbase, 2*time.Minute)

	return testCouchbase
}

func TestLDAPCreateAdminUser(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipLDAP(t)

	// Static configuration.
	clusterSize := 1

	testCouchbase := setupLDAP(t, targetKube)

	// Create LDAP User with Cluster admin privileges
	user, _, _ := mustCreateLDAPBoundUser(t, targetKube)
	e2eutil.MustWaitUntilUserExists(t, targetKube, testCouchbase, user, 4*time.Minute)

	// Validation
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonGroupCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserCreated},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestRBACDeleteUser verifies basic user deletion.
func TestLDAPCDeleteUser(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipLDAP(t)

	// Static configuration.
	clusterSize := 1
	testCouchbase := setupLDAP(t, targetKube)
	timeout := 2 * time.Minute

	// Expect user delete event eventually to occur
	event := k8sutil.UserDeleteEvent(e2e_constants.CouchbaseLDAPUserName, testCouchbase)
	echan := e2eutil.WaitForPendingClusterEvent(targetKube.KubeClient, testCouchbase, event, timeout)

	defer echan.Cancel()

	// Create User
	user, _, _ := mustCreateLDAPBoundUser(t, targetKube)
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

// TestLDAPDeleteRole verifies that deleting a group results in deleting User.
func TestLDAPDeleteRole(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipLDAP(t)

	// Static configuration.
	clusterSize := 1
	testCouchbase := setupLDAP(t, targetKube)
	timeout := 2 * time.Minute

	// Expect user delete event to occur
	event := k8sutil.UserDeleteEvent(e2e_constants.CouchbaseLDAPUserName, testCouchbase)
	echan := e2eutil.WaitForPendingClusterEvent(targetKube.KubeClient, testCouchbase, event, timeout)

	defer echan.Cancel()

	// Create User
	user, group, _ := mustCreateLDAPBoundUser(t, targetKube)
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

// TestLDAPUpdateRole changes cluster role to a bucket role and verifies
// reconciliation with couchbase.
func TestLDAPUpdateRole(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipLDAP(t)

	timeout := 2 * time.Minute

	// Cluster
	clusterSize := 1
	testCouchbase := setupLDAP(t, targetKube)

	// User
	user, group, _ := mustCreateLDAPBoundUser(t, targetKube)
	e2eutil.MustWaitUntilUserExists(t, targetKube, testCouchbase, user, timeout)

	// Change to bucket role user
	e2eutil.MustPatchGroup(t, targetKube, group, jsonpatch.NewPatchSet().Replace("/Spec/Roles/0/Name", couchbasev2.RoleBucketAdmin), time.Minute)
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

// TestLDAPRemoveUserFromBinding tests that a user is deleted
// when it is no longer referenced as a subject to any roles.
//
// Test binds 2 users to the same role.  One of the user is
// removed from the binding and since it doesn't have a role
// in any other binding the user is also deleted.
func TestLDAPRemoveUserFromBinding(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipLDAP(t)

	timeout := 2 * time.Minute

	// Cluster
	clusterSize := 1
	testCouchbase := setupLDAP(t, targetKube)

	// User
	user, _, binding := mustCreateLDAPBoundUser(t, targetKube)
	e2eutil.MustWaitUntilUserExists(t, targetKube, testCouchbase, user, timeout)

	// Create another user
	customUser := e2espec.NewDefaultLDAPUser()
	customUser.Name = "alt-user"
	customUser = e2eutil.MustNewUser(t, targetKube, customUser)

	// Expect user delete event eventually occur
	event := k8sutil.UserDeleteEvent(user.Name, testCouchbase)
	echan := e2eutil.WaitForPendingClusterEvent(targetKube.KubeClient, testCouchbase, event, timeout)

	defer echan.Cancel()

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

// TestLDAPDeleteBinding tests that user is deleted when entire
// rolebinding is deleted.
func TestLDAPDeleteBinding(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	skipLDAP(t)

	timeout := 2 * time.Minute

	// Create Cluster
	clusterSize := 1
	testCouchbase := setupLDAP(t, targetKube)

	// Expect user delete event to eventually occur
	event := k8sutil.UserDeleteEvent(e2e_constants.CouchbaseLDAPUserName, testCouchbase)
	echan := e2eutil.WaitForPendingClusterEvent(targetKube.KubeClient, testCouchbase, event, timeout)

	defer echan.Cancel()

	// Create User
	user, _, binding := mustCreateLDAPBoundUser(t, targetKube)
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
