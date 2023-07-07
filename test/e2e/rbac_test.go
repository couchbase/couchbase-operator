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

func bindGroup(t *testing.T, kubernetes *types.Cluster, group *couchbasev2.CouchbaseGroup) *couchbasev2.CouchbaseGroup {
	e2eutil.MustNewUser(t, kubernetes, e2espec.NewDefaultUser())
	g := e2eutil.MustNewGroup(t, kubernetes, group)
	binding := e2eutil.MustNewRoleBinding(t, kubernetes, e2espec.NewClusterRoleBinding())
	binding.Spec.RoleRef.Kind = couchbasev2.GroupCRDResourceKind
	binding.Spec.RoleRef.Name = group.Name

	return g
}

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

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 1

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Create user
	user, group, _ := mustCreateBoundUser(t, kubernetes)
	e2eutil.MustWaitUntilUserExists(t, kubernetes, cluster, user, 4*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * UserCreated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonGroupCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserCreated},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)

	// Check if group has appropriate role
	role := e2eutil.NewRole(string(couchbasev2.RoleClusterAdmin)).Create()
	e2eutil.MustHaveRoles(t, kubernetes, cluster, group, role)
}

// TestRBACDeleteUser verifies basic user deletion.
func TestRBACDeleteUser(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	timeout := 2 * time.Minute

	// Create Cluster
	clusterSize := 1
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Expect user delete event eventually to occur
	event := k8sutil.UserDeleteEvent(e2e_constants.CouchbaseUserName, cluster)
	echan := e2eutil.WaitForPendingClusterEvent(kubernetes, cluster, event, timeout)

	defer echan.Cancel()

	// Create User
	user, _, _ := mustCreateBoundUser(t, kubernetes)
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

// TestRBACDeleteRole verifies that deleting a role results in deleting User.
func TestRBACDeleteRole(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	timeout := 2 * time.Minute

	// Create Cluster
	clusterSize := 1
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Expect user delete event to occur
	event := k8sutil.UserDeleteEvent(e2e_constants.CouchbaseUserName, cluster)
	echan := e2eutil.WaitForPendingClusterEvent(kubernetes, cluster, event, timeout)

	defer echan.Cancel()

	// Create User
	user, group, _ := mustCreateBoundUser(t, kubernetes)
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

// TestRBACUpdateRole changes cluster role to a bucket role and verifies
// reconciliation with couchbase.
func TestRBACUpdateRole(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	timeout := 2 * time.Minute

	// Cluster
	clusterSize := 1
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// User
	user, group, _ := mustCreateBoundUser(t, kubernetes)
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

// TestRBACRemoveUserFromBinding tests that a user is deleted
// when it is no longer referenced as a subject to any roles.
//
// Test binds 2 users to the same role.  One of the user is
// removed from the binding and since it doesn't have a role
// in any other binding the user is also deleted.
func TestRBACRemoveUserFromBinding(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	timeout := 2 * time.Minute

	// Cluster
	clusterSize := 1
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// User
	user, _, binding := mustCreateBoundUser(t, kubernetes)
	e2eutil.MustWaitUntilUserExists(t, kubernetes, cluster, user, timeout)

	// Create another user
	customUser := e2espec.NewDefaultUser()
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

// TestRBACDeleteBinding tests that user is deleted when entire
// rolebinding is deleted.
func TestRBACDeleteBinding(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	timeout := 2 * time.Minute

	// Create Cluster
	clusterSize := 1
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Expect user delete event to eventually occur
	event := k8sutil.UserDeleteEvent(e2e_constants.CouchbaseUserName, cluster)
	echan := e2eutil.WaitForPendingClusterEvent(kubernetes, cluster, event, timeout)

	defer echan.Cancel()

	// Create User
	user, _, binding := mustCreateBoundUser(t, kubernetes)
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

// Verify RBAC auth can be applied to LDAP users.
func TestRBACWithLDAPAuth(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 1

	// Start LDAP service
	service := e2espec.NewLDAPService()
	_ = e2eutil.MustNewLDAPService(t, kubernetes, service)

	// Start the LDAP server
	tlsOpts := &e2eutil.TLSOpts{
		AltNames: e2espec.LDAPAltNames(kubernetes.Namespace),
	}

	ctx := e2eutil.MustInitLDAPTLS(t, kubernetes, tlsOpts)

	pod := e2espec.NewLDAPServerTLS(kubernetes.Namespace, ctx.ClusterSecretName)
	_ = e2eutil.MustNewLDAPServer(t, kubernetes, pod)

	// Create a cluster with LDAP Auth
	cluster := e2espec.NewLDAPClusterBasic(clusterOptions().WithEphemeralTopology(clusterSize).Options, kubernetes.Namespace, ctx.ClusterSecretName, kubernetes.DefaultSecret.Name)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Verify Connectivity
	e2eutil.MustCheckLDAPStatus(t, kubernetes, cluster, 2*time.Minute)

	// Validation
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestRBACSelection ensures the operator only creates users that match the
// label selector.
func TestRBACSelection(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 1

	// Create first user
	user, group, binding := mustCreateBoundUser(t, kubernetes)

	// Create second user with labels
	customUser := e2espec.NewDefaultUser()
	labels := map[string]string{
		"loves": "nala",
	}
	customUser.Name = "simba"
	customUser.Labels = labels
	customUser = e2eutil.MustNewUser(t, kubernetes, customUser)

	// Patch group with labels
	e2eutil.MustPatchGroup(t, kubernetes, group, jsonpatch.NewPatchSet().Add("/metadata/labels", labels), time.Minute)

	// Add second user to role binding
	subject := couchbasev2.CouchbaseRoleBindingSubject{
		Kind: e2e_constants.CouchbaseSubjectUserKind,
		Name: customUser.Name,
	}
	e2eutil.MustPatchRoleBinding(t, kubernetes, binding, jsonpatch.NewPatchSet().Add("/spec/subjects/-", subject), time.Minute)

	// Create a cluster that selects only labelled users.
	couchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	couchbase.Spec.Security.RBAC.Selector = &metav1.LabelSelector{
		MatchLabels: labels,
	}
	couchbase = e2eutil.MustNewClusterFromSpec(t, kubernetes, couchbase)

	// Ensure the unlabelled user doesn't get created.
	if err := e2eutil.WaitUntilUserExists(kubernetes, couchbase, user, time.Minute); err == nil {
		e2eutil.Die(t, fmt.Errorf("user created unexpectedly"))
	}

	// Second user is created
	e2eutil.MustWaitUntilUserExists(t, kubernetes, couchbase, customUser, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * UserCreated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonGroupCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUserCreated},
	}
	ValidateEvents(t, kubernetes, couchbase, expectedEvents)
}

func TestRBACWithBucketScopedRolePost7(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"
	collectionName := "brain"
	collectionGroupName := "group1"
	collectionGroupName1 := "buttons"
	collectionGroupName2 := "mindy"

	// Create a collection and collection group.
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)
	collectionGroup := e2eutil.NewCollectionGroup(collectionGroupName, collectionGroupName1, collectionGroupName2).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithCollections(collection, collectionGroup).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	group := bindGroup(t, kubernetes, e2espec.NewGroup(couchbasev2.RoleDataReader).WithBuckets(bucket).Create())

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName, collectionGroupName1, collectionGroupName2)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// wait for group to be created
	e2eutil.MustWaitUntilGroupExists(t, kubernetes, cluster, group.Name, 4*time.Minute)

	// Check if group has appropriate role
	role := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).Create()
	e2eutil.MustHaveRoles(t, kubernetes, cluster, group, role)
}

func TestRBACWithBucketScopedRolePre7(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).BeforeVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	group := bindGroup(t, kubernetes, e2espec.NewGroup(couchbasev2.RoleDataReader).WithBuckets(bucket).Create())

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	e2eutil.MustWaitUntilGroupExists(t, kubernetes, cluster, group.Name, 4*time.Minute)
	// check to see if role is created
	role := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).Create()
	e2eutil.MustHaveRoles(t, kubernetes, cluster, group, role)
}

func TestRBACWithScopeScopedRole(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"
	collectionName := "brain"
	collectionGroupName := "group1"
	collectionGroupName1 := "buttons"
	collectionGroupName2 := "mindy"

	// Create a collection and collection group.
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)
	collectionGroup := e2eutil.NewCollectionGroup(collectionGroupName, collectionGroupName1, collectionGroupName2).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithCollections(collection, collectionGroup).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	group := bindGroup(t, kubernetes, e2espec.NewGroup(couchbasev2.RoleDataReader).WithBuckets(bucket).WithScopes(scope).Create())

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName, collectionGroupName1, collectionGroupName2)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// wait for group to be created
	e2eutil.MustWaitUntilGroupExists(t, kubernetes, cluster, group.Name, 4*time.Minute)

	// Check if group has appropriate role
	role := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scope.GetName()).Create()
	e2eutil.MustHaveRoles(t, kubernetes, cluster, group, role)
}

func TestRBACWithScopeScopedRoleNoPermissionWhenNoScopeCreated(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"
	collectionName := "brain"
	collectionGroupName := "group1"
	collectionGroupName1 := "buttons"
	collectionGroupName2 := "mindy"

	// Create a collection and collection group.
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)
	collectionGroup := e2eutil.NewCollectionGroup(collectionGroupName, collectionGroupName1, collectionGroupName2).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithCollections(collection, collectionGroup).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	group := bindGroup(t, kubernetes, e2espec.NewGroup(couchbasev2.RoleDataReader).WithBuckets(bucket).WithScopes(scope).WithCollections(collection).WithCollectionGroups(collectionGroup).Create())

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// wait for group to be created
	e2eutil.MustWaitUntilGroupExists(t, kubernetes, cluster, group.Name, 4*time.Minute)

	// Check if group has appropriate role
	role := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scope.GetName()).Create()
	role2 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).Create()
	e2eutil.MustNotHaveRoles(t, kubernetes, cluster, group, role, role2)
}

func TestRBACWithMultipleScopedRolesViaScopeGroup(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeGroupName := "animaniacs"
	scope1 := "wakko"
	scope2 := "yakko"
	scope3 := "dot"

	// Create a collection and collection group.

	// Create a scope.
	scopes := e2eutil.NewScopeGroup(scopeGroupName, scope1, scope2, scope3).MustCreate(t, kubernetes)
	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scopes)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	group := bindGroup(t, kubernetes, e2espec.NewGroup(couchbasev2.RoleDataReader).WithBuckets(bucket).WithScopeGroups(scopes).Create())

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScopes(scope1, scope2, scope3)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// wait for group to be created
	e2eutil.MustWaitUntilGroupExists(t, kubernetes, cluster, group.Name, 4*time.Minute)

	// Check if group has appropriate role
	role := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scope1).Create()
	role2 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scope2).Create()
	role3 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scope3).Create()
	e2eutil.MustHaveRoles(t, kubernetes, cluster, group, role, role2, role3)
}

func TestRBACWithCollectionScopedRole(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"
	collectionName := "brain"
	collectionGroupName := "group1"
	collectionGroupName1 := "buttons"
	collectionGroupName2 := "mindy"

	// Create a collection and collection group.
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)
	collectionGroup := e2eutil.NewCollectionGroup(collectionGroupName, collectionGroupName1, collectionGroupName2).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithCollections(collection, collectionGroup).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	group := bindGroup(t, kubernetes, e2espec.NewGroup(couchbasev2.RoleDataReader).WithBuckets(bucket).WithScopes(scope).WithCollections(collection).Create())

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName, collectionGroupName1, collectionGroupName2)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// wait for group to be created
	e2eutil.MustWaitUntilGroupExists(t, kubernetes, cluster, group.Name, 4*time.Minute)

	// Check if group has appropriate role
	role := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scope.GetName()).WithCollection(collection.GetName()).Create()

	e2eutil.MustHaveRoles(t, kubernetes, cluster, group, role)
}

func TestRBACWithCollectionScopedRoleDoesNotAddPermissionDuetoNoCollection(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"
	collectionName := "brain"
	collectionGroupName := "group1"
	collectionGroupName1 := "buttons"
	collectionGroupName2 := "mindy"

	// Create a collection and collection group.
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)
	collectionGroup := e2eutil.NewCollectionGroup(collectionGroupName, collectionGroupName1, collectionGroupName2).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithCollections(collection, collectionGroup).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	group := bindGroup(t, kubernetes, e2espec.NewGroup(couchbasev2.RoleDataReader).WithBuckets(bucket).WithScopes(scope).WithCollections(collection).Create())

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// wait for group to be created
	e2eutil.MustWaitUntilGroupExists(t, kubernetes, cluster, group.Name, 4*time.Minute)

	// Check if group has appropriate role
	role := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scope.GetName()).WithCollection(collection.GetName()).Create()
	role2 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scope.GetName()).Create()
	role3 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).Create()
	e2eutil.MustNotHaveRoles(t, kubernetes, cluster, group, role, role2, role3)
}

func TestRBACWithMultipleCollectionScopedRole(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"
	collectionGroupName := "group1"
	collectionGroupName1 := "buttons"
	collectionGroupName2 := "mindy"

	// Create a collection and collection group.
	collectionGroup := e2eutil.NewCollectionGroup(collectionGroupName, collectionGroupName1, collectionGroupName2).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithCollections(collectionGroup).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	group := bindGroup(t, kubernetes, e2espec.NewGroup(couchbasev2.RoleDataReader).WithBuckets(bucket).WithScopes(scope).WithCollectionGroups(collectionGroup).Create())

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionGroupName1, collectionGroupName2)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// wait for group to be created
	e2eutil.MustWaitUntilGroupExists(t, kubernetes, cluster, group.Name, 4*time.Minute)

	// Check if group has appropriate role
	role := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scope.GetName()).WithCollection(collectionGroupName1).Create()
	role2 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scope.GetName()).WithCollection(collectionGroupName2).Create()

	e2eutil.MustHaveRoles(t, kubernetes, cluster, group, role, role2)
}

func TestRBACWithMultipleCollectionAndMultipleScopesScopedRole(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeGroupName := "infinity"
	scopeName1 := "pinky"
	scopeName2 := "brain"
	collectionGroupName := "animaniacs"
	collectionGroupName1 := "buttons"
	collectionGroupName2 := "mindy"

	// Create a collection and collection group.
	collectionGroup := e2eutil.NewCollectionGroup(collectionGroupName, collectionGroupName1, collectionGroupName2).MustCreate(t, kubernetes)

	// Create a scope.
	scopeGroup := e2eutil.NewScopeGroup(scopeGroupName, scopeName1, scopeName2).WithCollections(collectionGroup).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scopeGroup)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	group := bindGroup(t, kubernetes, e2espec.NewGroup(couchbasev2.RoleDataReader).WithBuckets(bucket).WithScopeGroups(scopeGroup).WithCollectionGroups(collectionGroup).Create())

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName1).WithCollections(collectionGroupName1, collectionGroupName2)
	expected.WithScope(scopeName2).WithCollections(collectionGroupName1, collectionGroupName2)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// wait for group to be created
	e2eutil.MustWaitUntilGroupExists(t, kubernetes, cluster, group.Name, 4*time.Minute)

	// Check if group has appropriate role
	role := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName1).WithCollection(collectionGroupName1).Create()
	role2 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName1).WithCollection(collectionGroupName2).Create()
	role3 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName2).WithCollection(collectionGroupName1).Create()
	role4 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName2).WithCollection(collectionGroupName2).Create()
	e2eutil.MustHaveRoles(t, kubernetes, cluster, group, role, role2, role3, role4)
}

func TestRBACWithCollectionScopedRoleBySelector(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"
	collectionName := "brain"
	label := map[string]string{
		"security": "rbac",
	}

	// Create a collection and collection group.
	collection := e2eutil.NewCollection(collectionName).WithLabels(label).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithLabels(label).WithCollections(collection).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	selector := &metav1.LabelSelector{}
	selector = metav1.AddLabelToSelector(selector, "security", "rbac")
	group := bindGroup(t, kubernetes, e2espec.NewGroup(couchbasev2.RoleDataReader).WithBuckets(bucket).WithScopeSelector(selector).WithCollectionSelector(selector).Create())
	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// wait for group to be created
	e2eutil.MustWaitUntilGroupExists(t, kubernetes, cluster, group.Name, 4*time.Minute)

	// Check if group has appropriate role
	role := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scope.GetName()).WithCollection(collection.GetName()).Create()
	e2eutil.MustHaveRoles(t, kubernetes, cluster, group, role)
}

func TestRBACWithCollectionScopedRoleBySelectorForGroups(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeGroupName := "disney"
	scopeName := "donald"
	scopeName2 := "mickey"
	collectionGroupName := "animaniacs"
	collectionName := "wakko"
	collectionName2 := "yakko"
	collectionName3 := "dot"
	label := map[string]string{
		"security": "rbac",
	}

	// Create a collection and collection group.
	collectionGroup := e2eutil.NewCollectionGroup(collectionGroupName, collectionName, collectionName2, collectionName3).WithLabels(label).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScopeGroup(scopeGroupName, scopeName, scopeName2).WithLabels(label).WithCollections(collectionGroup).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	selector := &metav1.LabelSelector{}
	selector = metav1.AddLabelToSelector(selector, "security", "rbac")
	group := bindGroup(t, kubernetes, e2espec.NewGroup(couchbasev2.RoleDataReader).WithBuckets(bucket).WithScopeSelector(selector).WithCollectionSelector(selector).Create())
	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName, collectionName2, collectionName3)
	expected.WithScope(scopeName2).WithCollections(collectionName, collectionName2, collectionName3)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// wait for group to be created
	e2eutil.MustWaitUntilGroupExists(t, kubernetes, cluster, group.Name, 4*time.Minute)

	// Check if group has appropriate role
	role := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName).WithCollection(collectionName).Create()
	role2 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName).WithCollection(collectionName2).Create()
	role3 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName).WithCollection(collectionName3).Create()
	role4 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName2).WithCollection(collectionName).Create()
	role5 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName2).WithCollection(collectionName2).Create()
	role6 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName2).WithCollection(collectionName3).Create()
	e2eutil.MustHaveRoles(t, kubernetes, cluster, group, role, role2, role3, role4, role5, role6)
}

func TestRBACWithCollectionScopedRoleBySelectorForMultipleScopesAndCollections(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "donald"
	scopeName2 := "mickey"
	collectionName := "wakko"
	collectionName2 := "yakko"
	label := map[string]string{
		"security": "rbac",
	}

	// Create a collection and collection group.
	collection := e2eutil.NewCollection(collectionName).WithLabels(label).MustCreate(t, kubernetes)
	collection2 := e2eutil.NewCollection(collectionName2).WithLabels(label).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithLabels(label).WithCollections(collection, collection2).MustCreate(t, kubernetes)
	scope2 := e2eutil.NewScope(scopeName2).WithLabels(label).WithCollections(collection, collection2).MustCreate(t, kubernetes)
	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope2)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	selector := &metav1.LabelSelector{}
	selector = metav1.AddLabelToSelector(selector, "security", "rbac")
	group := bindGroup(t, kubernetes, e2espec.NewGroup(couchbasev2.RoleDataReader).WithBuckets(bucket).WithScopeSelector(selector).WithCollectionSelector(selector).Create())

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName, collectionName2)
	expected.WithScope(scopeName2).WithCollections(collectionName, collectionName2)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// wait for group to be created
	e2eutil.MustWaitUntilGroupExists(t, kubernetes, cluster, group.Name, 4*time.Minute)

	// Check if group has appropriate role
	role := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName).WithCollection(collectionName).Create()
	role2 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName).WithCollection(collectionName2).Create()
	role4 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName2).WithCollection(collectionName).Create()
	role5 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName2).WithCollection(collectionName2).Create()
	e2eutil.MustHaveRoles(t, kubernetes, cluster, group, role, role2, role4, role5)
}

func TestRBACWithWillNotGetPermissionSetWithInvalidSelector(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "donald"
	scopeName2 := "mickey"
	collectionName := "wakko"
	collectionName2 := "yakko"
	label := map[string]string{
		"security": "rbac",
	}

	// Create a collection and collection group.
	collection := e2eutil.NewCollection(collectionName).WithLabels(label).MustCreate(t, kubernetes)
	collection2 := e2eutil.NewCollection(collectionName2).WithLabels(label).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithLabels(label).WithCollections(collection, collection2).MustCreate(t, kubernetes)
	scope2 := e2eutil.NewScope(scopeName2).WithLabels(label).WithCollections(collection, collection2).MustCreate(t, kubernetes)
	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope2)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	// create group targeting incorrect selector.  Should not have any permissions
	selector := &metav1.LabelSelector{}
	selector = metav1.AddLabelToSelector(selector, "security", "test_wrong_on_purpose")
	group := bindGroup(t, kubernetes, e2espec.NewGroup(couchbasev2.RoleDataReader).WithBuckets(bucket).WithScopeSelector(selector).WithCollectionSelector(selector).Create())

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName, collectionName2)
	expected.WithScope(scopeName2).WithCollections(collectionName, collectionName2)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// wait for group to be created
	e2eutil.MustWaitUntilGroupExists(t, kubernetes, cluster, group.Name, 4*time.Minute)

	// Check if group has appropriate role
	role := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName).WithCollection(collectionName).Create()
	role2 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName).WithCollection(collectionName2).Create()
	role4 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName2).WithCollection(collectionName).Create()
	role5 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName2).WithCollection(collectionName2).Create()
	role6 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).Create()
	role7 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName).Create()
	role8 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName2).Create()
	e2eutil.MustNotHaveRoles(t, kubernetes, cluster, group, role, role2, role4, role5, role6, role7, role8)
}

func TestRBACWithMultipleBucketRole(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).CouchbaseBucket()

	clusterSize := 1

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)
	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/spec/memoryQuota", e2espec.NewResourceQuantityMi(128)), time.Minute)

	// Second bucket
	bucket2 := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	bucket2.SetName("mickey")
	bucket2 = e2eutil.MustNewBucket(t, kubernetes, bucket2)
	bucket2 = e2eutil.MustPatchBucket(t, kubernetes, bucket2, jsonpatch.NewPatchSet().Replace("/spec/memoryQuota", e2espec.NewResourceQuantityMi(128)), time.Minute)

	group := bindGroup(t, kubernetes, e2espec.NewGroup(couchbasev2.RoleDataReader).WithBuckets(bucket, bucket2).Create())

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	e2eutil.MustWaitUntilGroupExists(t, kubernetes, cluster, group.Name, 4*time.Minute)
	// check to see if role is created
	role := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).Create()
	role2 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket2.GetName()).Create()
	e2eutil.MustHaveRoles(t, kubernetes, cluster, group, role, role2)
}

func TestRBACWithMultipleBucketSingleScopeRole(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"

	// create scope
	scope := e2eutil.NewScope(scopeName).MustCreate(t, kubernetes)
	// Create first bucket
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	// bind scope to bucket 1
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)
	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/spec/memoryQuota", e2espec.NewResourceQuantityMi(128)), time.Minute)

	// Second bucket
	bucket2 := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	bucket2.SetName("mickey")
	// bind scope to bucket 2
	e2eutil.LinkBucketToScopesExplicit(bucket2, scope)
	bucket2 = e2eutil.MustNewBucket(t, kubernetes, bucket2)
	bucket2 = e2eutil.MustPatchBucket(t, kubernetes, bucket2, jsonpatch.NewPatchSet().Replace("/spec/memoryQuota", e2espec.NewResourceQuantityMi(128)), time.Minute)

	group := bindGroup(t, kubernetes, e2espec.NewGroup(couchbasev2.RoleDataReader).WithBuckets(bucket, bucket2).WithScopes(scope).Create())

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	e2eutil.MustWaitUntilGroupExists(t, kubernetes, cluster, group.Name, 4*time.Minute)
	// check to see if role is created
	role := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName).Create()
	role2 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket2.GetName()).WithScope(scopeName).Create()
	e2eutil.MustHaveRoles(t, kubernetes, cluster, group, role, role2)
}

func TestRBACWithMultipleBucketMultiScopeRole(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"
	scope2Name := "brain"
	// create scope
	scope := e2eutil.NewScope(scopeName).MustCreate(t, kubernetes)
	scope2 := e2eutil.NewScope(scope2Name).MustCreate(t, kubernetes)
	// Create first bucket
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	// bind scope to bucket 1
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope2)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)
	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/spec/memoryQuota", e2espec.NewResourceQuantityMi(128)), time.Minute)

	// Second bucket
	bucket2 := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	bucket2.SetName("mickey")
	// bind scope to bucket 2
	e2eutil.LinkBucketToScopesExplicit(bucket2, scope)
	e2eutil.LinkBucketToScopesExplicit(bucket2, scope2)
	bucket2 = e2eutil.MustNewBucket(t, kubernetes, bucket2)
	bucket2 = e2eutil.MustPatchBucket(t, kubernetes, bucket2, jsonpatch.NewPatchSet().Replace("/spec/memoryQuota", e2espec.NewResourceQuantityMi(128)), time.Minute)

	group := bindGroup(t, kubernetes, e2espec.NewGroup(couchbasev2.RoleDataReader).WithBuckets(bucket, bucket2).WithScopes(scope, scope2).Create())

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	e2eutil.MustWaitUntilGroupExists(t, kubernetes, cluster, group.Name, 4*time.Minute)
	// check to see if role is created
	role := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName).Create()
	role2 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket2.GetName()).WithScope(scopeName).Create()
	role3 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scope2Name).Create()
	role4 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket2.GetName()).WithScope(scope2Name).Create()
	e2eutil.MustHaveRoles(t, kubernetes, cluster, group, role, role2, role3, role4)
}

func TestRBACWithMultipleBucketMultiScopeSingleCollectionRole(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"
	scope2Name := "brain"
	collectionName := "animaniacs"

	// create collection
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)
	// create scope
	scope := e2eutil.NewScope(scopeName).WithCollections(collection).MustCreate(t, kubernetes)
	scope2 := e2eutil.NewScope(scope2Name).WithCollections(collection).MustCreate(t, kubernetes)
	// Create first bucket
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	// bind scope to bucket 1
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope2)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)
	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/spec/memoryQuota", e2espec.NewResourceQuantityMi(128)), time.Minute)

	// Second bucket
	bucket2 := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	bucket2.SetName("mickey")
	// bind scope to bucket 2
	e2eutil.LinkBucketToScopesExplicit(bucket2, scope)
	e2eutil.LinkBucketToScopesExplicit(bucket2, scope2)
	bucket2 = e2eutil.MustNewBucket(t, kubernetes, bucket2)
	bucket2 = e2eutil.MustPatchBucket(t, kubernetes, bucket2, jsonpatch.NewPatchSet().Replace("/spec/memoryQuota", e2espec.NewResourceQuantityMi(128)), time.Minute)

	group := bindGroup(t, kubernetes, e2espec.NewGroup(couchbasev2.RoleDataReader).WithBuckets(bucket, bucket2).WithScopes(scope, scope2).WithCollections(collection).Create())

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	e2eutil.MustWaitUntilGroupExists(t, kubernetes, cluster, group.Name, 4*time.Minute)
	// check to see if role is created
	role := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName).WithCollection(collectionName).Create()
	role2 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket2.GetName()).WithScope(scopeName).WithCollection(collectionName).Create()
	role3 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scope2Name).WithCollection(collectionName).Create()
	role4 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket2.GetName()).WithScope(scope2Name).WithCollection(collectionName).Create()
	e2eutil.MustHaveRoles(t, kubernetes, cluster, group, role, role2, role3, role4)
}

func TestRBACWithMultipleBucketMultiScopeMultiCollectionRole(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"
	scope2Name := "brain"
	collectionName := "animaniacs"
	collection2Name := "wakko"

	// create collection
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)
	collection2 := e2eutil.NewCollection(collection2Name).MustCreate(t, kubernetes)
	// create scope
	scope := e2eutil.NewScope(scopeName).WithCollections(collection, collection2).MustCreate(t, kubernetes)
	scope2 := e2eutil.NewScope(scope2Name).WithCollections(collection, collection2).MustCreate(t, kubernetes)
	// Create first bucket
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	// bind scope to bucket 1
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope2)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)
	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/spec/memoryQuota", e2espec.NewResourceQuantityMi(128)), time.Minute)

	// Second bucket
	bucket2 := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	bucket2.SetName("mickey")
	// bind scope to bucket 2
	e2eutil.LinkBucketToScopesExplicit(bucket2, scope)
	e2eutil.LinkBucketToScopesExplicit(bucket2, scope2)
	bucket2 = e2eutil.MustNewBucket(t, kubernetes, bucket2)
	bucket2 = e2eutil.MustPatchBucket(t, kubernetes, bucket2, jsonpatch.NewPatchSet().Replace("/spec/memoryQuota", e2espec.NewResourceQuantityMi(128)), time.Minute)

	group := bindGroup(t, kubernetes, e2espec.NewGroup(couchbasev2.RoleDataReader).WithBuckets(bucket, bucket2).WithScopes(scope, scope2).WithCollections(collection, collection2).Create())

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	e2eutil.MustWaitUntilGroupExists(t, kubernetes, cluster, group.Name, 4*time.Minute)
	// check to see if role is created
	role := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName).WithCollection(collectionName).Create()
	role2 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket2.GetName()).WithScope(scopeName).WithCollection(collectionName).Create()
	role3 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scope2Name).WithCollection(collectionName).Create()
	role4 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket2.GetName()).WithScope(scope2Name).WithCollection(collectionName).Create()
	role5 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scopeName).WithCollection(collection2Name).Create()
	role6 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket2.GetName()).WithScope(scopeName).WithCollection(collection2Name).Create()
	role7 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).WithScope(scope2Name).WithCollection(collection2Name).Create()
	role8 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket2.GetName()).WithScope(scope2Name).WithCollection(collection2Name).Create()
	e2eutil.MustHaveRoles(t, kubernetes, cluster, group, role, role2, role3, role4, role5, role6, role7, role8)
}

func TestRBACWithBucketSelector(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).CouchbaseBucket()

	clusterSize := 1
	label := map[string]string{
		"security": "rbac",
	}
	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	bucket.SetLabels(label)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)
	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/spec/memoryQuota", e2espec.NewResourceQuantityMi(128)), time.Minute)

	// Second bucket
	bucket2 := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	bucket2.SetName("mickey")
	bucket2.SetLabels(label)
	bucket2 = e2eutil.MustNewBucket(t, kubernetes, bucket2)
	bucket2 = e2eutil.MustPatchBucket(t, kubernetes, bucket2, jsonpatch.NewPatchSet().Replace("/spec/memoryQuota", e2espec.NewResourceQuantityMi(128)), time.Minute)

	selector := &metav1.LabelSelector{}
	selector = metav1.AddLabelToSelector(selector, "security", "rbac")
	group := bindGroup(t, kubernetes, e2espec.NewGroup(couchbasev2.RoleDataReader).WithBucketSelector(selector).Create())

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	e2eutil.MustWaitUntilGroupExists(t, kubernetes, cluster, group.Name, 4*time.Minute)
	// check to see if role is created
	role := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket.GetName()).Create()
	role2 := e2eutil.NewRole(string(couchbasev2.RoleDataReader)).WithBucket(bucket2.GetName()).Create()
	e2eutil.MustHaveRoles(t, kubernetes, cluster, group, role, role2)
}
