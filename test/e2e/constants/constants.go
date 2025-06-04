package constants

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	util_const "github.com/couchbase/couchbase-operator/pkg/util/constants"
)

// Couchbase cluster.
var (
	ClusterNamePrefix          = "test-couchbase-"
	CouchbaseLabel             = util_const.LabelApp + "=" + util_const.App
	CouchbaseOperatorLabel     = util_const.LabelApp + "=couchbase-operator"
	CouchbaseServerClusterKey  = "couchbase_cluster"
	CouchbaseServerPodLabelStr = CouchbaseLabel + "," + CouchbaseServerClusterKey + "="
	CouchbaseServerConfig      = "test_config_1"
	CouchbaseServerAltConfig   = "test_config_2"
	CouchbaseNodeLabel         = "couchbase_node"
	CouchbaseVolumeLabel       = "couchbase_volume"
	CouchbaseNodeConfKey       = "couchbase_node_conf"

	CouchbaseVersionAnnotationKey = "server.couchbase.com/version"

	// List of Couchbase-cluster services.
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

// Secret name in kube used for testing.
var (
	KubeTestSecretName = "basic-test-secret"
	SecretUsernameKey  = util_const.AuthSecretUsernameKey
	SecretPasswordKey  = util_const.AuthSecretPasswordKey
	CbClusterUsername  = "Administrator"
	CbClusterPassword  = "password"
)

// Labels for K8S nodes.
var (
	NodeRoleMasterLabel      = "node-role.kubernetes.io/master"
	FailureDomainZoneLabel   = util_const.ServerGroupLabel
	FailureDomainRegionLabel = util_const.TopologyRegionLabel
)

// Operator constants.
const (
	OperatorRestPort = 8080

	// Couchbase cluster constants.
	CbClusterRestPort int32 = 8091
)

// different size naming.
const (
	Size1 = 1
	Size2 = 2
	Size3 = 3
	Size4 = 4
	Size5 = 5
)

// DefaultBucket naming.
const (
	DefaultBucket          = "default"
	DefaultEphemeralBucket = "ephemeral-bucket"
)

// DefaultReplication naming.
const (
	DefaultReplication = "test-replication"
)

// CRD Object naming.
const (
	CouchbaseUserName     = "admin"
	CouchbaseLDAPUserName = "ldap-admin"
	ClusterRoleName       = "admin-role"
	BucketRoleName        = "bucket-role"
	RoleBindingName       = "role-binding"

	// Couchbase specific cluster roles.
	ClusterAdminRole                        = "cluster_admin"
	RoleFullAdmin                           = "admin"
	RoleReadOnlyAdmin                       = "ro_admin"
	RoleSecurityAdmin                       = "security_admin"
	RoleXDCRAdmin                           = "replication_admin"
	RoleQueryCurlAccess                     = "query_external_access"
	RoleQuestySystemAccess                  = "query_system_catalog"
	RoleAnalyticsReader                     = "analytics_reader"
	RoleSecurityAdminExternal               = "security_admin_external"
	RoleSecurityAdminLocal                  = "security_admin_local"
	RoleBackupAdmin                         = "backup_admin"
	RoleQueryManageGlobalFunctions          = "query_manage_global_functions"
	RoleQueryExecuteGlobalFunctions         = "query_execute_global_functions"
	RoleQueryManageGlobalExternalFunctions  = "query_manage_global_external_functions"
	RoleQueryExecuteGlobalExternalFunctions = "query_execute_global_external_functions"
	RoleAnalyticsAdmin                      = "analytics_admin"
	RoleExternalStatsReader                 = "external_stats_reader"
	RoleEventingAdmin                       = "eventing_admin"
	RoleUserAdminExternal                   = "user_admin_external"
	RoleUserAdminLocal                      = "user_admin_local"
	// Couchbase specific bucket roles.
	BucketAdminRole = "bucket_admin"

	// Binding.
	CouchbaseSubjectUserKind  = "CouchbaseUser"
	CouchbaseSubjectGroupKind = "CouchbaseGroup"
)

// ldap naming.
const (
	LDAPDomain        = "openldap"
	LDAPLabelSelector = "openldap.couchbase.com"
)

// test secrets Label.
const (
	TestLabelSelector = "couchbaseqe"
)

// Pod level keys and fields.
const (
	PodSpecAnnotation = "pod.couchbase.com/spec"
)

// Couchbase server image tags.
const (
	// Couchbase server image version 7.1.0.
	CouchbaseImageVersion710 = "couchbase/server:7.1.0"
)
