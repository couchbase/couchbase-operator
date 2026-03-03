/*
Copyright 2019-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2espec

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	e2e_constants "github.com/couchbase/couchbase-operator/test/e2e/constants"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetAllClusterRoles(v *couchbaseutil.Version) ([]string, error) {
	version7, err := couchbaseutil.NewVersion("7.0.0")
	if err != nil {
		return nil, err
	}

	version8, err := couchbaseutil.NewVersion("8.0.0")
	if err != nil {
		return nil, err
	}

	if v.Less(version7) {
		return clusterRoles6, nil
	}

	if v.Less(version8) {
		return clusterRoles7, nil
	}

	return clusterRoles8, nil
}

var clusterRoles6 = []string{
	e2e_constants.RoleFullAdmin,
	e2e_constants.ClusterAdminRole,
	e2e_constants.RoleReadOnlyAdmin,
	e2e_constants.RoleXDCRAdmin,
	e2e_constants.RoleQueryCurlAccess,
	e2e_constants.RoleQuestySystemAccess,
	e2e_constants.RoleAnalyticsReader,
	e2e_constants.RoleSecurityAdmin,
	e2e_constants.RoleAnalyticsAdmin,
}

var clusterRoles7 = []string{
	e2e_constants.RoleFullAdmin,
	e2e_constants.ClusterAdminRole,
	e2e_constants.RoleReadOnlyAdmin,
	e2e_constants.RoleXDCRAdmin,
	e2e_constants.RoleQueryCurlAccess,
	e2e_constants.RoleQuestySystemAccess,
	e2e_constants.RoleAnalyticsReader,
	//nolint:staticcheck // Using deprecated roles here is intentional for 7.x compatibility tests.
	e2e_constants.RoleSecurityAdminExternal,
	//nolint:staticcheck // Using deprecated roles here is intentional for 7.x compatibility tests.
	e2e_constants.RoleSecurityAdminLocal,
	e2e_constants.RoleBackupAdmin,
	e2e_constants.RoleQueryManageGlobalFunctions,
	e2e_constants.RoleQueryExecuteGlobalFunctions,
	e2e_constants.RoleQueryManageGlobalExternalFunctions,
	e2e_constants.RoleQueryExecuteGlobalExternalFunctions,
	e2e_constants.RoleAnalyticsAdmin,
	e2e_constants.RoleExternalStatsReader,
	e2e_constants.RoleEventingAdmin,
}

var clusterRoles8 = []string{
	e2e_constants.RoleFullAdmin,
	e2e_constants.ClusterAdminRole,
	e2e_constants.RoleReadOnlyAdmin,
	e2e_constants.RoleReadOnlySecurityAdmin,
	e2e_constants.RoleXDCRAdmin,
	e2e_constants.RoleQueryCurlAccess,
	e2e_constants.RoleQuestySystemAccess,
	e2e_constants.RoleQueryManageSystemCatalog,
	e2e_constants.RoleAnalyticsReader,
	// security + user admin roles for 8.0+
	e2e_constants.RoleSecurityAdmin,
	e2e_constants.RoleUserAdminExternal,
	e2e_constants.RoleUserAdminLocal,
	e2e_constants.RoleBackupAdmin,
	e2e_constants.RoleQueryManageGlobalFunctions,
	e2e_constants.RoleQueryExecuteGlobalFunctions,
	e2e_constants.RoleQueryManageGlobalExternalFunctions,
	e2e_constants.RoleQueryExecuteGlobalExternalFunctions,
	e2e_constants.RoleAnalyticsAdmin,
	e2e_constants.RoleExternalStatsReader,
	e2e_constants.RoleEventingAdmin,
	e2e_constants.RoleApplicationTelemetryWriter,
}

// NewDefaultUser creates a new default user.
func NewDefaultUser() *couchbasev2.CouchbaseUser {
	return &couchbasev2.CouchbaseUser{
		ObjectMeta: metav1.ObjectMeta{
			Name: e2e_constants.CouchbaseUserName,
		},
		Spec: couchbasev2.CouchbaseUserSpec{
			AuthDomain: couchbasev2.InternalAuthDomain,
			AuthSecret: e2e_constants.KubeTestSecretName,
		},
	}
}

// NewDefaultLDAPUser creates a new LDAP user.
func NewDefaultLDAPUser() *couchbasev2.CouchbaseUser {
	return &couchbasev2.CouchbaseUser{
		ObjectMeta: metav1.ObjectMeta{
			Name: e2e_constants.CouchbaseLDAPUserName,
		},
		Spec: couchbasev2.CouchbaseUserSpec{
			AuthDomain: couchbasev2.LDAPAuthDomain,
		},
	}
}

// NewClusterAdminGroup creates group to grant user cluster admin privilege.
func NewClusterAdminGroup() *couchbasev2.CouchbaseGroup {
	// couchbase cluster privilege
	clusterAdminRole := couchbasev2.Role{
		Name: e2e_constants.ClusterAdminRole,
	}
	spec := couchbasev2.CouchbaseGroupSpec{
		Roles: []couchbasev2.Role{clusterAdminRole},
	}

	// crd
	return &couchbasev2.CouchbaseGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: e2e_constants.ClusterRoleName,
		},
		Spec: spec,
	}
}

// NewGroupWithRoles creates group with specified roles.
func NewGroupWithRoles(roleNames []string) *couchbasev2.CouchbaseGroup {
	spec := couchbasev2.CouchbaseGroupSpec{}

	for _, roleName := range roleNames {
		spec.Roles = append(spec.Roles, couchbasev2.Role{
			Name: couchbasev2.RoleName(roleName),
		})
	}

	// crd
	return &couchbasev2.CouchbaseGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: e2e_constants.ClusterRoleName,
		},
		Spec: spec,
	}
}

// NewBucketAdminGroup creates group to grant user admin privilege to all bucket.
func NewBucketAdminGroup() *couchbasev2.CouchbaseGroup {
	// couchbase bucket role
	bucketAdminRole := couchbasev2.Role{
		Name:   e2e_constants.BucketAdminRole,
		Bucket: "*",
	}

	spec := couchbasev2.CouchbaseGroupSpec{
		Roles: []couchbasev2.Role{bucketAdminRole},
	}

	// crd
	return &couchbasev2.CouchbaseGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: e2e_constants.BucketRoleName,
		},
		Spec: spec,
	}
}

type GroupDefinition struct {
	Buckets            []metav1.Object
	BucketSelector     *metav1.LabelSelector
	Scopes             []*couchbasev2.CouchbaseScope
	ScopeGroups        []*couchbasev2.CouchbaseScopeGroup
	ScopeSelector      *metav1.LabelSelector
	Collections        []*couchbasev2.CouchbaseCollection
	CollectionGroups   []*couchbasev2.CouchbaseCollectionGroup
	CollectionSelector *metav1.LabelSelector
	StringBucket       string
	Name               couchbasev2.RoleName
}

func NewGroup(name couchbasev2.RoleName) *GroupDefinition {
	def := &GroupDefinition{
		Buckets:            []metav1.Object{},
		BucketSelector:     nil,
		Scopes:             []*couchbasev2.CouchbaseScope{},
		ScopeGroups:        []*couchbasev2.CouchbaseScopeGroup{},
		ScopeSelector:      nil,
		Collections:        []*couchbasev2.CouchbaseCollection{},
		CollectionGroups:   []*couchbasev2.CouchbaseCollectionGroup{},
		CollectionSelector: nil,
		StringBucket:       "",
		Name:               name,
	}

	return def
}

func (g *GroupDefinition) WithBuckets(buckets ...metav1.Object) *GroupDefinition {
	g.Buckets = append(g.Buckets, buckets...)
	return g
}

func (g *GroupDefinition) WithScopes(scopes ...*couchbasev2.CouchbaseScope) *GroupDefinition {
	g.Scopes = append(g.Scopes, scopes...)
	return g
}

func (g *GroupDefinition) WithCollections(collections ...*couchbasev2.CouchbaseCollection) *GroupDefinition {
	g.Collections = append(g.Collections, collections...)
	return g
}

func (g *GroupDefinition) WithBucketSelector(selector *metav1.LabelSelector) *GroupDefinition {
	g.BucketSelector = selector
	return g
}

func (g *GroupDefinition) WithScopeSelector(selector *metav1.LabelSelector) *GroupDefinition {
	g.ScopeSelector = selector
	return g
}

func (g *GroupDefinition) WithCollectionSelector(selector *metav1.LabelSelector) *GroupDefinition {
	g.CollectionSelector = selector
	return g
}

func (g *GroupDefinition) WithScopeGroups(group ...*couchbasev2.CouchbaseScopeGroup) *GroupDefinition {
	g.ScopeGroups = append(g.ScopeGroups, group...)
	return g
}

func (g *GroupDefinition) WithCollectionGroups(group ...*couchbasev2.CouchbaseCollectionGroup) *GroupDefinition {
	g.CollectionGroups = append(g.CollectionGroups, group...)
	return g
}

func (g *GroupDefinition) Create() *couchbasev2.CouchbaseGroup {
	scopeSpec := couchbasev2.ScopeRoleSpec{}

	if len(g.Scopes) > 0 || len(g.ScopeGroups) > 0 {
		scopeSpec.Resources = []couchbasev2.ScopeLocalObjectReference{}

		for _, scope := range g.Scopes {
			ref := couchbasev2.ScopeLocalObjectReference{
				Kind: couchbasev2.CouchbaseScopeKindScope,
				Name: couchbasev2.ScopeOrCollectionName(scope.GetName()),
			}
			scopeSpec.Resources = append(scopeSpec.Resources, ref)
		}

		for _, group := range g.ScopeGroups {
			ref := couchbasev2.ScopeLocalObjectReference{
				Kind: couchbasev2.CouchbaseScopeKindScopeGroup,
				Name: couchbasev2.ScopeOrCollectionName(group.GetName()),
			}
			scopeSpec.Resources = append(scopeSpec.Resources, ref)
		}
	}

	if g.ScopeSelector != nil {
		scopeSpec.Selector = g.ScopeSelector
	}

	collectionSpec := couchbasev2.CollectionRoleSpec{}

	if len(g.CollectionGroups) > 0 || len(g.Collections) > 0 {
		collectionSpec.Resources = []couchbasev2.CollectionLocalObjectReference{}

		for _, collection := range g.Collections {
			ref := couchbasev2.CollectionLocalObjectReference{
				Kind: couchbasev2.CouchbaseCollectionKindCollection,
				Name: couchbasev2.ScopeOrCollectionName(collection.GetName()),
			}
			collectionSpec.Resources = append(collectionSpec.Resources, ref)
		}

		for _, group := range g.CollectionGroups {
			ref := couchbasev2.CollectionLocalObjectReference{
				Kind: couchbasev2.CouchbaseCollectionKindCollectionGroup,
				Name: couchbasev2.ScopeOrCollectionName(group.GetName()),
			}
			collectionSpec.Resources = append(collectionSpec.Resources, ref)
		}
	}

	if g.CollectionSelector != nil {
		collectionSpec.Selector = g.CollectionSelector
	}

	bucketSpec := &couchbasev2.BucketRoleSpec{}

	if len(g.Buckets) > 0 {
		bucketSpec.Resources = []couchbasev2.BucketLocalObjectReference{}

		for _, bucket := range g.Buckets {
			ref := couchbasev2.BucketLocalObjectReference{
				Kind: "CouchbaseBucket",
				Name: bucket.GetName(),
			}
			bucketSpec.Resources = append(bucketSpec.Resources, ref)
		}
	}

	if g.BucketSelector != nil {
		bucketSpec.Selector = g.BucketSelector
	}

	if g.BucketSelector == nil && len(g.Buckets) == 0 {
		bucketSpec = nil
	}

	bucketRole := couchbasev2.Role{
		Name:        g.Name,
		Bucket:      g.StringBucket,
		Buckets:     *bucketSpec,
		Scopes:      scopeSpec,
		Collections: collectionSpec,
	}

	spec := couchbasev2.CouchbaseGroupSpec{
		Roles: []couchbasev2.Role{bucketRole},
	}

	// crd
	return &couchbasev2.CouchbaseGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: e2e_constants.BucketRoleName,
		},
		Spec: spec,
	}
}

func NewBucketScopeCollectionGroup(role couchbasev2.RoleName, bucket string, scope *couchbasev2.CouchbaseScope, collection *couchbasev2.CouchbaseCollection) *couchbasev2.CouchbaseGroup {
	group := NewBucketScopeGroup(role, bucket, scope)
	collectionSpec := couchbasev2.CollectionRoleSpec{
		Resources: []couchbasev2.CollectionLocalObjectReference{
			{
				Kind: couchbasev2.CouchbaseCollectionKindCollection,
				Name: couchbasev2.ScopeOrCollectionName(collection.Name),
			},
		},
	}
	group.Spec.Roles[0].Collections = collectionSpec

	return group
}

func NewBucketScopeCollectionGroupViaSelector(role couchbasev2.RoleName, bucket string, labelKey string, labelValue string) *couchbasev2.CouchbaseGroup {
	scopeSpec := couchbasev2.ScopeRoleSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				labelKey: labelValue,
			},
		},
	}

	collectionSpec := couchbasev2.CollectionRoleSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				labelKey: labelValue,
			},
		},
	}

	bucketRole := couchbasev2.Role{
		Name:        role,
		Bucket:      bucket,
		Scopes:      scopeSpec,
		Collections: collectionSpec,
	}

	spec := couchbasev2.CouchbaseGroupSpec{
		Roles: []couchbasev2.Role{bucketRole},
	}

	// crd
	return &couchbasev2.CouchbaseGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: e2e_constants.BucketRoleName,
		},
		Spec: spec,
	}
}

func NewBucketScopeCollectionsGroup(role couchbasev2.RoleName, bucket string, scope *couchbasev2.CouchbaseScope, collections *couchbasev2.CouchbaseCollectionGroup) *couchbasev2.CouchbaseGroup {
	group := NewBucketScopeGroup(role, bucket, scope)
	collectionSpec := couchbasev2.CollectionRoleSpec{
		Resources: []couchbasev2.CollectionLocalObjectReference{
			{
				Kind: couchbasev2.CouchbaseCollectionKindCollectionGroup,
				Name: couchbasev2.ScopeOrCollectionName(collections.Name),
			},
		},
	}
	group.Spec.Roles[0].Collections = collectionSpec

	return group
}

func NewBucketScopesCollectionsGroup(role couchbasev2.RoleName, bucket string, scopes *couchbasev2.CouchbaseScopeGroup, collections *couchbasev2.CouchbaseCollectionGroup) *couchbasev2.CouchbaseGroup {
	scopeSpec := couchbasev2.ScopeRoleSpec{
		Resources: []couchbasev2.ScopeLocalObjectReference{
			{
				Kind: couchbasev2.CouchbaseScopeKindScopeGroup,
				Name: couchbasev2.ScopeOrCollectionName(scopes.Name),
			},
		},
	}

	collectionSpec := couchbasev2.CollectionRoleSpec{
		Resources: []couchbasev2.CollectionLocalObjectReference{
			{
				Kind: couchbasev2.CouchbaseCollectionKindCollectionGroup,
				Name: couchbasev2.ScopeOrCollectionName(collections.Name),
			},
		},
	}

	bucketRole := couchbasev2.Role{
		Name:        role,
		Bucket:      bucket,
		Scopes:      scopeSpec,
		Collections: collectionSpec,
	}

	spec := couchbasev2.CouchbaseGroupSpec{
		Roles: []couchbasev2.Role{bucketRole},
	}

	// crd
	return &couchbasev2.CouchbaseGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: e2e_constants.BucketRoleName,
		},
		Spec: spec,
	}
}

func NewBucketScopesGroup(role couchbasev2.RoleName, bucket string, scopes *couchbasev2.CouchbaseScopeGroup) *couchbasev2.CouchbaseGroup {
	scopeSpec := couchbasev2.ScopeRoleSpec{
		Resources: []couchbasev2.ScopeLocalObjectReference{
			{
				Kind: couchbasev2.CouchbaseScopeKindScopeGroup,
				Name: couchbasev2.ScopeOrCollectionName(scopes.Name),
			},
		},
	}

	bucketRole := couchbasev2.Role{
		Name:   role,
		Bucket: bucket,
		Scopes: scopeSpec,
	}

	spec := couchbasev2.CouchbaseGroupSpec{
		Roles: []couchbasev2.Role{bucketRole},
	}

	// crd
	return &couchbasev2.CouchbaseGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: e2e_constants.BucketRoleName,
		},
		Spec: spec,
	}
}

func NewBucketScopeGroup(role couchbasev2.RoleName, bucket string, scope *couchbasev2.CouchbaseScope) *couchbasev2.CouchbaseGroup {
	scopeSpec := couchbasev2.ScopeRoleSpec{
		Resources: []couchbasev2.ScopeLocalObjectReference{
			{
				Kind: couchbasev2.CouchbaseScopeKindScope,
				Name: couchbasev2.ScopeOrCollectionName(scope.Name),
			},
		},
	}

	bucketRole := couchbasev2.Role{
		Name:   role,
		Bucket: bucket,
		Scopes: scopeSpec,
	}

	spec := couchbasev2.CouchbaseGroupSpec{
		Roles: []couchbasev2.Role{bucketRole},
	}

	// crd
	return &couchbasev2.CouchbaseGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: e2e_constants.BucketRoleName,
		},
		Spec: spec,
	}
}

func NewBucketGroup(role couchbasev2.RoleName, bucket string) *couchbasev2.CouchbaseGroup {
	// couchbase bucket role
	bucketRole := couchbasev2.Role{
		Name:   role,
		Bucket: bucket,
	}

	spec := couchbasev2.CouchbaseGroupSpec{
		Roles: []couchbasev2.Role{bucketRole},
	}

	// crd
	return &couchbasev2.CouchbaseGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: e2e_constants.BucketRoleName,
		},
		Spec: spec,
	}
}

// NewClusterRoleBinding creates spec with default user bound to the cluster admin role.
func NewClusterRoleBinding() *couchbasev2.CouchbaseRoleBinding {
	users := []string{e2e_constants.CouchbaseUserName}
	return NewRoleBinding(e2e_constants.RoleBindingName, users, e2e_constants.ClusterRoleName)
}

// NewBucketRoleBinding creates spec with default user bound to the bucket admin role.
func NewBucketRoleBinding() *couchbasev2.CouchbaseRoleBinding {
	users := []string{e2e_constants.CouchbaseUserName}
	return NewRoleBinding(e2e_constants.RoleBindingName, users, e2e_constants.BucketRoleName)
}

// NewDefaultRoleBinding binds list of users to a role.
func NewRoleBinding(name string, users []string, role string) *couchbasev2.CouchbaseRoleBinding {
	subjects := []couchbasev2.CouchbaseRoleBindingSubject{}

	for _, user := range users {
		subjects = append(subjects,
			couchbasev2.CouchbaseRoleBindingSubject{
				Kind: couchbasev2.RoleBindingSubjectTypeUser,
				Name: user,
			})
	}

	roleRef := couchbasev2.CouchbaseRoleBindingRef{
		Kind: couchbasev2.RoleBindingReferenceTypeGroup,
		Name: role,
	}

	return &couchbasev2.CouchbaseRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: couchbasev2.CouchbaseRoleBindingSpec{
			Subjects: subjects,
			RoleRef:  roleRef,
		},
	}
}
