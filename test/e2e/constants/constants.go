package constants

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	util_const "github.com/couchbase/couchbase-operator/pkg/util/constants"
)

const (
	// CommunityEditionImage is a version of CE that exists.  Sadly we have to
	// hard code this (not do a regex replace) as this only gets major releases,
	// mo minors or patches.
	CommunityEditionImage = "couchbase/server:community-6.0.0"
)

// These values can be updated from e2espec/crd.go
var (
	CouchbaseServerImage = "couchbase/server:enterprise-5.5.3"
)

// Const Ansible setting string
var (
	CbAppSelectorMap = map[string]string{
		"app": "couchbase",
	}
)

// Couchbase cluster
var (
	ClusterNamePrefix          = "test-couchbase-"
	CouchbaseLabel             = util_const.LabelApp + "=" + util_const.App
	CouchbaseOperatorLabel     = util_const.LabelApp + "=couchbase-operator"
	CouchbaseServerClusterKey  = "couchbase_cluster"
	CouchbaseServerPodLabelStr = CouchbaseLabel + "," + CouchbaseServerClusterKey + "="

	// List of Couchbase-cluster services
	StatefulCbServiceList = couchbasev2.ServiceList{
		couchbasev2.DataService,
		couchbasev2.IndexService,
		couchbasev2.AnalyticsService,
	}
	StatelessCbServiceList = couchbasev2.ServiceList{
		couchbasev2.QueryService,
		couchbasev2.SearchService,
		couchbasev2.EventingService,
	}
)

// Secret name in kube used for testing
var (
	KubeTestSecretName = "basic-test-secret"
	SecretUsernameKey  = util_const.AuthSecretUsernameKey
	SecretPasswordKey  = util_const.AuthSecretPasswordKey
	CbClusterUsername  = "Administrator"
	CbClusterPassword  = "password"
)

// Labels for K8S nodes
var (
	NodeRoleMasterLabel    = "node-role.kubernetes.io/master"
	FailureDomainZoneLabel = util_const.ServerGroupLabel
)

// Operator constants
const (
	OperatorRestPort = 8080

	// Couchbase cluster constants
	CbClusterRestPort int32 = 8091
)

// different size naming
const (
	Size1 = 1
	Size2 = 2
	Size3 = 3
	Size4 = 4
	Size5 = 5
)

//DefaultBucket naming
const (
	DefaultBucket = "default"
)

//DefaultReplication naming
const (
	DefaultReplication = "test-replication"
)

// CRD Object naming
const (
	CouchbaseUserName     = "admin"
	CouchbaseLDAPUserName = "ldap-admin"
	ClusterRoleName       = "admin-role"
	BucketRoleName        = "bucket-role"
	RoleBindingName       = "role-binding"

	// Couchbase specific roles
	ClusterAdminRole = "cluster_admin"
	BucketAdminRole  = "bucket_admin"

	// Binding
	CouchbaseRoleRefKind      = "CouchbaseRole"
	CouchbaseSubjectUserKind  = "CouchbaseUser"
	CouchbaseSubjectGroupKind = "CouchbaseGroup"
)

//ldap naming
const (
	LDAPDomain        = "openldap"
	LDAPLabelSelector = "openldap.couchbase.com"
)
