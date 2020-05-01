package e2espec

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	e2e_constants "github.com/couchbase/couchbase-operator/test/e2e/constants"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NewDefaultUser creates a new default user
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

// NewDefaultLDAPUser creates a new LDAP user
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

// NewClusterAdminGroup creates group to grant user cluster admin privilege
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

// NewBucketAdminGroup creates group to grant user admin privilege to all bucket
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

// NewClusterRoleBinding creates spec with default user bound to the cluster admin role
func NewClusterRoleBinding() *couchbasev2.CouchbaseRoleBinding {
	users := []string{e2e_constants.CouchbaseUserName}
	return NewRoleBinding(e2e_constants.RoleBindingName, users, e2e_constants.ClusterRoleName)
}

// NewBucketRoleBinding creates spec with default user bound to the bucket admin role
func NewBucketRoleBinding() *couchbasev2.CouchbaseRoleBinding {
	users := []string{e2e_constants.CouchbaseUserName}
	return NewRoleBinding(e2e_constants.RoleBindingName, users, e2e_constants.BucketRoleName)
}

// NewDefaultRoleBinding binds list of users to a role
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
