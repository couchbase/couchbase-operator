package collector

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/info/resource"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

var (
	// Add a resource type here for it to be collected.
	Resources = []resource.Collector{
		{Resource: &couchbasev2.CouchbaseCluster{}, Scope: resource.ScopeClusterName, LogLevel: resource.LogLevelRequired},
		{Resource: &couchbasev2.CouchbaseBucket{}, Scope: resource.ScopeAll, LogLevel: resource.LogLevelRequired},
		{Resource: &couchbasev2.CouchbaseEphemeralBucket{}, Scope: resource.ScopeAll, LogLevel: resource.LogLevelRequired},
		{Resource: &couchbasev2.CouchbaseMemcachedBucket{}, Scope: resource.ScopeAll, LogLevel: resource.LogLevelRequired},
		{Resource: &couchbasev2.CouchbaseReplication{}, Scope: resource.ScopeAll, LogLevel: resource.LogLevelRequired},
		{Resource: &couchbasev2.CouchbaseUser{}, Scope: resource.ScopeAll, LogLevel: resource.LogLevelRequired},
		{Resource: &couchbasev2.CouchbaseGroup{}, Scope: resource.ScopeAll, LogLevel: resource.LogLevelRequired},
		{Resource: &couchbasev2.CouchbaseRoleBinding{}, Scope: resource.ScopeAll, LogLevel: resource.LogLevelRequired},
		{Resource: &couchbasev2.CouchbaseBackup{}, Scope: resource.ScopeAll, LogLevel: resource.LogLevelRequired},
		{Resource: &couchbasev2.CouchbaseBackupRestore{}, Scope: resource.ScopeAll, LogLevel: resource.LogLevelRequired},
		{Resource: &couchbasev2.CouchbaseAutoscaler{}, Scope: resource.ScopeAll, LogLevel: resource.LogLevelRequired},
		{Resource: &couchbasev2.CouchbaseScope{}, Scope: resource.ScopeAll, LogLevel: resource.LogLevelRequired},
		{Resource: &couchbasev2.CouchbaseScopeGroup{}, Scope: resource.ScopeAll, LogLevel: resource.LogLevelRequired},
		{Resource: &couchbasev2.CouchbaseCollection{}, Scope: resource.ScopeAll, LogLevel: resource.LogLevelRequired},
		{Resource: &couchbasev2.CouchbaseCollectionGroup{}, Scope: resource.ScopeAll, LogLevel: resource.LogLevelRequired},
		{
			Resource: &corev1.ConfigMap{}, Scope: resource.ScopeCluster, LogLevel: resource.LogLevelRequired,
			Reason: "Used to determine issues with Couchbase Cluster state, server environment variables, and logging configuration",
		},
		{
			Resource: &corev1.Endpoints{}, Scope: resource.ScopeCluster, LogLevel: resource.LogLevelRequired,
			Reason: "",
		},
		{
			Resource: &corev1.Namespace{}, Scope: resource.ScopeNamespace, LogLevel: resource.LogLevelRequired,
			Reason: "",
		},
		{
			Resource: &corev1.Node{}, Scope: resource.ScopeAll, LogLevel: resource.LogLevelSensitive,
			Reason: "Used to determine issues with orchestration platform and identify potential images problems",
		},
		{
			Resource: &corev1.PersistentVolume{}, Scope: resource.ScopeAll, LogLevel: resource.LogLevelSensitive,
			Reason: "Used to determine compatibility issues with underlying persistent volume",
		},
		{
			Resource: &corev1.PersistentVolumeClaim{}, Scope: resource.ScopeCluster, LogLevel: resource.LogLevelRequired,
			Reason: "Used to determine compatibility issues with underlying persistent volume",
		},
		{
			Resource: &corev1.Pod{}, Scope: resource.ScopeCluster, LogLevel: resource.LogLevelRequired,
			Reason: "",
		},
		{
			Resource: &corev1.Secret{}, Scope: resource.ScopeAll, LogLevel: resource.LogLevelSensitive,
			Reason: "Used to determine issues with stored cluster passwords, TLS configurations and other private keys stored in secrets",
		},
		{
			Resource: &corev1.Service{}, Scope: resource.ScopeCluster, LogLevel: resource.LogLevelRequired,
			Reason: "",
		},
		{
			Resource: &corev1.ServiceAccount{}, Scope: resource.ScopeAll, LogLevel: resource.LogLevelSensitive,
			Reason: "",
		},
		{
			Resource: &appsv1.Deployment{}, Scope: resource.ScopeOperatorDeployment, LogLevel: resource.LogLevelRequired,
			Reason: "Used to determine issues with Operator and Dynamic Admission Control deployments",
		},
		{
			Resource: &appsv1.Deployment{}, Scope: resource.ScopeEventCollectorDeployment, LogLevel: resource.LogLevelRequired,
			Reason: "Used to determine issues with the entire cluster",
		},
		{
			Resource: &rbacv1.ClusterRole{}, Scope: resource.ScopeAll, LogLevel: resource.LogLevelSensitive,
			Reason: "Used to determine whether RBAC Is correctly setup for the running Operator version.",
		},
		{
			Resource: &rbacv1.ClusterRoleBinding{}, Scope: resource.ScopeAll, LogLevel: resource.LogLevelSensitive,
			Reason: "Used to determine whether RBAC Is correctly setup for the running Operator version.",
		},
		{
			Resource: &rbacv1.Role{}, Scope: resource.ScopeAll, LogLevel: resource.LogLevelSensitive,
			Reason: "Used to determine whether RBAC Is correctly setup for the running Operator version.",
		},
		{
			Resource: &rbacv1.RoleBinding{}, Scope: resource.ScopeAll, LogLevel: resource.LogLevelSensitive,
			Reason: "Used to determine whether RBAC Is correctly setup for the running Operator version.",
		},
		{
			Resource: &batchv1.Job{}, Scope: resource.ScopeCluster, LogLevel: resource.LogLevelRequired,
			Reason: "Used to determine issues with Jobs created for restoring from backup",
		},
		{
			Resource: &batchv1.CronJob{}, Scope: resource.ScopeCluster, LogLevel: resource.LogLevelRequired,
			Reason: "Used to determine issues with Cronjobs for scheduled backups",
		},
		{
			Resource: &apiextensionsv1.CustomResourceDefinition{}, Scope: resource.ScopeCouchbaseGroup, LogLevel: resource.LogLevelRequired,
			Reason: "Used to determine issues with installed CRD version against installed Operator and DAC version",
		},
		{
			Resource: &policyv1.PodDisruptionBudget{}, Scope: resource.ScopeCluster, LogLevel: resource.LogLevelRequired,
			Reason: "Used to determine issues with automatic Kubernetes upgrades",
		},
	}

	// Define all implied sub-resources we can collect information for.
	Initializers = []Initializer{
		NewEventCollector,
		NewLogCollector,
		NewOperatorCollector,
	}
)
