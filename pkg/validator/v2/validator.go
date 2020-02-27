package v2

import (
	"crypto/x509"
	"fmt"
	"strconv"
	"strings"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	util_x509 "github.com/couchbase/couchbase-operator/pkg/util/x509"
	"github.com/couchbase/couchbase-operator/pkg/validator/types"
	"github.com/couchbase/couchbase-operator/pkg/validator/util"
	"github.com/couchbase/gocbmgr"

	"github.com/go-openapi/errors"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/robfig/cron"
)

const (
	defaultIndexStorageSetting                    = "memory_optimized"
	defaultAutoFailoverTimeout                    = "120s"
	defaultAutoFailoverMaxCount                   = 3
	defaultAutoFailoverOnDataDiskIssuesTimePeriod = "120s"
	defaultServiceMemQuota                        = "256Mi"
	defaultAnalyticsServiceMemQuota               = "1Gi"
	defaultFSGroup                                = 1000
	defaultMetricsImage                           = "couchbase/exporter:1.0.0"
	redhatMetricsImage                            = "http://registry.connect.redhat.com/couchbase/exporter:1.0.0-1"
	defaultBackupImage                            = "couchbase/operator-backup:6.5.0-1"
	defaultBackupServiceAccount                   = "couchbase-backup"
)

var (
	emptyObject = struct{}{}
)

func ApplyDefaults(v *types.Validator, object *unstructured.Unstructured) jsonpatch.PatchList {
	var patch jsonpatch.PatchList

	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster", Value: emptyObject})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "dataServiceMemoryQuota"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/dataServiceMemoryQuota", Value: defaultServiceMemQuota})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "indexServiceMemoryQuota"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/indexServiceMemoryQuota", Value: defaultServiceMemQuota})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "searchServiceMemoryQuota"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/searchServiceMemoryQuota", Value: defaultServiceMemQuota})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "eventingServiceMemoryQuota"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/eventingServiceMemoryQuota", Value: defaultServiceMemQuota})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "analyticsServiceMemoryQuota"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/analyticsServiceMemoryQuota", Value: defaultAnalyticsServiceMemQuota})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "indexStorageSetting"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/indexStorageSetting", Value: defaultIndexStorageSetting})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "autoFailoverTimeout"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/autoFailoverTimeout", Value: defaultAutoFailoverTimeout})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "autoFailoverMaxCount"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/autoFailoverMaxCount", Value: defaultAutoFailoverMaxCount})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "autoFailoverOnDataDiskIssuesTimePeriod"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/autoFailoverOnDataDiskIssuesTimePeriod", Value: defaultAutoFailoverOnDataDiskIssuesTimePeriod})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "autoCompaction"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/autoCompaction", Value: emptyObject})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "autoCompaction", "databaseFragmentationThreshold"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/autoCompaction/databaseFragmentationThreshold", Value: emptyObject})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "autoCompaction", "databaseFragmentationThreshold", "percent"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/autoCompaction/databaseFragmentationThreshold/percent", Value: 30})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "autoCompaction", "viewFragmentationThreshold"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/autoCompaction/viewFragmentationThreshold", Value: emptyObject})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "autoCompaction", "viewFragmentationThreshold", "percent"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/autoCompaction/viewFragmentationThreshold/percent", Value: 30})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "autoCompaction", "tombstonePurgeInterval"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/autoCompaction/tombstonePurgeInterval", Value: "72h"})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "networking"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/networking", Value: emptyObject})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "networking", "adminConsoleServiceType"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/networking/adminConsoleServiceType", Value: corev1.ServiceTypeNodePort})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "networking", "exposedFeatureServiceType"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/networking/exposedFeatureServiceType", Value: corev1.ServiceTypeNodePort})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "securityContext"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/securityContext", Value: emptyObject})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "securityContext", "fsGroup"); !found {
		fsgroup := defaultFSGroup

		// OCP specific hack, set the fsGroup to that defined in the namespace.
		// Otherwise default to the default for the dockerhub container.
		namespace, err := v.Abstraction.GetNamespace(object.GetNamespace())
		if !apierrors.IsForbidden(err) {
			if namespace.Annotations != nil {
				if groups, ok := namespace.Annotations["openshift.io/sa.scc.supplemental-groups"]; ok {
					// This may either look like 1000140000/10000
					// or 1000140000-1000150000, just pick the first
					// group.
					i := strings.Index(groups, "-")
					if i == -1 {
						i = strings.Index(groups, "/")
					}
					if i != -1 {
						if val, err := strconv.Atoi(groups[:i]); err == nil {
							fsgroup = val
						}
					}
				}
			}
		}

		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/securityContext/fsGroup", Value: fsgroup})
	}
	if managedBackup, found, _ := unstructured.NestedBool(object.Object, "spec", "backup", "managed"); found && managedBackup {
		if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "backup", "image"); !found {
			patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/backup/image", Value: defaultBackupImage})
		}
		if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "backup", "serviceAccountName"); !found {
			patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/backup/serviceAccountName", Value: defaultBackupServiceAccount})
		}
	}
	if enableMonitoring, found, _ := unstructured.NestedBool(object.Object, "spec", "monitoring", "prometheus", "enabled"); found && enableMonitoring {
		if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "monitoring", "prometheus", "image"); !found {
			metricsImage := defaultMetricsImage

			namespace, err := v.Abstraction.GetNamespace(object.GetNamespace())
			if !apierrors.IsForbidden(err) {
				if namespace.Annotations != nil {
					for _, annotation := range namespace.Annotations {
						if strings.HasPrefix(annotation, "openshift.io") {
							metricsImage = redhatMetricsImage
							break
						}
					}
				}
			}

			patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/monitoring/prometheus/image", Value: metricsImage})
		}
	}

	return patch
}

func ApplyBucketDefaults(v *types.Validator, object *unstructured.Unstructured) jsonpatch.PatchList {
	var patch jsonpatch.PatchList

	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec", Value: emptyObject})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "memoryQuota"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/memoryQuota", Value: "100Mi"})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "replicas"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/replicas", Value: 1})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "ioPriority"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/ioPriority", Value: couchbasev2.CouchbaseBucketIOPriorityLow})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "evictionPolicy"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/evictionPolicy", Value: couchbasev2.CouchbaseBucketEvictionPolicyValueOnly})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "conflictResolution"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/conflictResolution", Value: couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "compressionMode"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/compressionMode", Value: cbmgr.CompressionModePassive})
	}

	return patch
}

func ApplyEphemeralBucketDefaults(v *types.Validator, object *unstructured.Unstructured) jsonpatch.PatchList {
	var patch jsonpatch.PatchList

	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec", Value: emptyObject})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "memoryQuota"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/memoryQuota", Value: "100Mi"})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "replicas"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/replicas", Value: 1})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "ioPriority"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/ioPriority", Value: couchbasev2.CouchbaseBucketIOPriorityLow})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "evictionPolicy"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/evictionPolicy", Value: couchbasev2.CouchbaseEphemeralBucketEvictionPolicyNoEviction})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "conflictResolution"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/conflictResolution", Value: couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "compressionMode"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/compressionMode", Value: cbmgr.CompressionModePassive})
	}

	return patch
}

func ApplyMemcachedBucketDefaults(v *types.Validator, object *unstructured.Unstructured) jsonpatch.PatchList {
	var patch jsonpatch.PatchList

	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec", Value: emptyObject})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "memoryQuota"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/memoryQuota", Value: "100Mi"})
	}

	return patch
}

func ApplyReplicationDefaults(v *types.Validator, object *unstructured.Unstructured) jsonpatch.PatchList {
	var patch jsonpatch.PatchList

	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "compressionType"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/compressionType", Value: couchbasev2.CompressionTypeAuto})
	}

	return patch
}

func ApplyGroupDefaults(v *types.Validator, object *unstructured.Unstructured) jsonpatch.PatchList {
	var patch jsonpatch.PatchList
	roles, _, _ := unstructured.NestedSlice(object.Object, "spec", "roles")
	for i, role := range roles {
		if r, ok := role.(map[string]interface{}); ok {
			// Apply bucket role to all buckets by default
			bucket, ok := r["bucket"]
			if !ok || (bucket == "") {
				if couchbasev2.IsBucketRole(couchbasev2.RoleName(r["name"].(string))) {
					path := fmt.Sprintf("/spec/roles/%d/bucket", i)
					patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: path, Value: "*"})
				}
			}

		}
	}

	return patch
}

func ApplyBackupDefaults(object *unstructured.Unstructured) jsonpatch.PatchList {
	var patch jsonpatch.PatchList

	if _, found, _ := unstructured.NestedFieldCopy(object.Object, "spec", "strategy"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/strategy", Value: couchbasev2.FullIncremental})
	}

	if _, found, _ := unstructured.NestedFieldCopy(object.Object, "spec", "successfulJobsHistoryLimit"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/successfulJobsHistoryLimit", Value: 3})
	}

	if _, found, _ := unstructured.NestedFieldCopy(object.Object, "spec", "failedJobsHistoryLimit"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/failedJobsHistoryLimit", Value: 5})
	}

	if _, found, _ := unstructured.NestedFieldCopy(object.Object, "spec", "backoffLimit"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/backoffLimit", Value: 2})
	}

	if _, found, _ := unstructured.NestedFieldCopy(object.Object, "spec", "backupRetention"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/backupRetention", Value: "720h"})
	}

	if _, found, _ := unstructured.NestedFieldCopy(object.Object, "spec", "logRetention"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/logRetention", Value: "168h"})
	}

	if _, found, _ := unstructured.NestedFieldCopy(object.Object, "spec", "size"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/size", Value: "20Gi"})
	}

	return patch
}

func ApplyBackupRestoreDefaults(object *unstructured.Unstructured) jsonpatch.PatchList {
	var patch jsonpatch.PatchList

	if start, found, _ := unstructured.NestedFieldCopy(object.Object, "spec", "start"); found {
		if _, found, _ := unstructured.NestedFieldCopy(object.Object, "spec", "end"); !found {
			patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/end", Value: start})
		}
	}

	if _, found, _ := unstructured.NestedFieldCopy(object.Object, "spec", "logRetention"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/logRetention", Value: "168h"})
	}

	if _, found, _ := unstructured.NestedFieldCopy(object.Object, "spec", "backoffLimit"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/backoffLimit", Value: 2})
	}

	return patch
}

func CheckConstraints(v *types.Validator, customResource *couchbasev2.CouchbaseCluster) error {
	errs := []error{}

	// Basic schema openapi v3 validation (not provided by structural schema)
	if !util.UniqueString(customResource.Spec.Networking.AdminConsoleServices.StringSlice()) {
		errs = append(errs, errors.DuplicateItems("spec.networking.adminConsoleServices", "body"))
	}
	if !util.UniqueString(customResource.Spec.Networking.ExposedFeatures) {
		errs = append(errs, errors.DuplicateItems("spec.networking.exposedFeatures", "body"))
	}
	if !util.UniqueString(customResource.Spec.ServerGroups) {
		errs = append(errs, errors.DuplicateItems("spec.serverGroups", "body"))
	}
	for i, class := range customResource.Spec.Servers {
		if !util.UniqueString(class.Services.StringSlice()) {
			errs = append(errs, errors.DuplicateItems(fmt.Sprintf("spec.servers[%d].services", i), "body"))
		}
		if !util.UniqueString(class.ServerGroups) {
			errs = append(errs, errors.DuplicateItems(fmt.Sprintf("spec.servers[%d].serverGroups", i), "body"))
		}
	}

	// Cluster validation
	if customResource.Spec.ClusterSettings.DataServiceMemQuota != nil {
		if customResource.Spec.ClusterSettings.DataServiceMemQuota.Cmp(*k8sutil.NewResourceQuantityMi(256)) < 0 {
			errs = append(errs, fmt.Errorf("spec.cluster.dataServiceMemoryQuota in body should be greater than or equal to 256Mi"))
		}
	}
	if customResource.Spec.ClusterSettings.IndexServiceMemQuota != nil {
		if customResource.Spec.ClusterSettings.IndexServiceMemQuota.Cmp(*k8sutil.NewResourceQuantityMi(256)) < 0 {
			errs = append(errs, fmt.Errorf("spec.cluster.indexServiceMemoryQuota in body should be greater than or equal to 256Mi"))
		}
	}
	if customResource.Spec.ClusterSettings.SearchServiceMemQuota != nil {
		if customResource.Spec.ClusterSettings.SearchServiceMemQuota.Cmp(*k8sutil.NewResourceQuantityMi(256)) < 0 {
			errs = append(errs, fmt.Errorf("spec.cluster.searchServiceMemoryQuota in body should be greater than or equal to 256Mi"))
		}
	}
	if customResource.Spec.ClusterSettings.EventingServiceMemQuota != nil {
		if customResource.Spec.ClusterSettings.EventingServiceMemQuota.Cmp(*k8sutil.NewResourceQuantityMi(256)) < 0 {
			errs = append(errs, fmt.Errorf("spec.cluster.eventingServiceMemoryQuota in body should be greater than or equal to 256Mi"))
		}
	}
	if customResource.Spec.ClusterSettings.AnalyticsServiceMemQuota != nil {
		if customResource.Spec.ClusterSettings.AnalyticsServiceMemQuota.Cmp(*k8sutil.NewResourceQuantityMi(1024)) < 0 {
			errs = append(errs, fmt.Errorf("spec.cluster.analyticsServiceMemoryQuota in body should be greater than or equal to 1Gi"))
		}
	}
	if customResource.Spec.ClusterSettings.AutoFailoverTimeout != nil {
		if customResource.Spec.ClusterSettings.AutoFailoverTimeout.Seconds() < 5.0 {
			errs = append(errs, fmt.Errorf("spec.cluster.autoFailoverTimeout in body should be greater than or equal to 5s"))
		}
		if customResource.Spec.ClusterSettings.AutoFailoverTimeout.Seconds() > 3600.0 {
			errs = append(errs, fmt.Errorf("spec.cluster.autoFailoverTimeout in body should be less than or equal to 1h"))
		}
	}
	if customResource.Spec.ClusterSettings.AutoFailoverOnDataDiskIssuesTimePeriod != nil {
		if customResource.Spec.ClusterSettings.AutoFailoverOnDataDiskIssuesTimePeriod.Seconds() < 5.0 {
			errs = append(errs, fmt.Errorf("spec.cluster.autoFailoverOnDataDiskIssuesTimePeriod in body should be greater than or equal to 5s"))
		}
		if customResource.Spec.ClusterSettings.AutoFailoverOnDataDiskIssuesTimePeriod.Seconds() > 3600.0 {
			errs = append(errs, fmt.Errorf("spec.cluster.autoFailoverOnDataDiskIssuesTimePeriod in body should be less than or equal to 1h"))
		}
	}

	// Referenced object validation
	if secret, err := v.Abstraction.GetSecret(customResource.Namespace, customResource.Spec.Security.AdminSecret); err != nil {
		// Silently ignore permissions errors, some users may not want us seeing these resources.
		if !apierrors.IsForbidden(err) {
			errs = append(errs, err)
		}
	} else if secret == nil {
		errs = append(errs, fmt.Errorf("secret %s referenced by spec.security.adminSecret must exist", customResource.Spec.Security.AdminSecret))
	}

	// Referenced object validation
	if customResource.Spec.Monitoring != nil && customResource.Spec.Monitoring.Prometheus != nil {
		authSecret := customResource.Spec.Monitoring.Prometheus.AuthorizationSecret
		if authSecret != nil {
			if secret, err := v.Abstraction.GetSecret(customResource.Namespace, *authSecret); err != nil {
				// Silently ignore permissions errors, some users may not want us seeing these resources.
				if !apierrors.IsForbidden(err) {
					errs = append(errs, err)
				}
			} else if secret == nil {
				errs = append(errs, fmt.Errorf("secret %s referenced by spec.monitoring.prometheus.authorizationSecret must exist", *customResource.Spec.Monitoring.Prometheus.AuthorizationSecret))
			} else {
				if _, ok := secret.Data["token"]; !ok {
					errs = append(errs, fmt.Errorf("monitoring authorization secret %s must contain key 'token'", *authSecret))
				}
			}
		}
	}

	if customResource.Spec.XDCR.Managed {
		for i, remoteCluster := range customResource.Spec.XDCR.RemoteClusters {
			if remoteCluster.AuthenticationSecret != nil {
				if secret, err := v.Abstraction.GetSecret(customResource.Namespace, *remoteCluster.AuthenticationSecret); err != nil {
					// Silently ignore permissions errors, some users may not want us seeing these resources.
					if !apierrors.IsForbidden(err) {
						errs = append(errs, err)
					}
				} else if secret == nil {
					errs = append(errs, fmt.Errorf("secret %s referenced by spec.xdcr.remoteClusters[%d].authenticationSecret must exist", *remoteCluster.AuthenticationSecret, i))
				}
			}

			replications, err := v.Abstraction.GetCouchbaseReplications(customResource.Namespace, remoteCluster.Replications.Selector)
			if err != nil {
				errs = append(errs, err)
			}

			for _, replication := range replications.Items {
				if err := validateBucketExists(v, customResource, replication.Spec.Bucket); err != nil {
					errs = append(errs, fmt.Errorf("bucket %s referenced by spec.bucket in couchbasereplications.couchbase.com/%s must exist: %v", replication.Spec.Bucket, replication.Name, err))
				}
			}
		}
	}

	// Check to make sure:
	// 1. Server names are unique
	// 2. The data service is specified on at least one node
	unique := make(map[string]bool)
	hasDataService := false
	for i := range customResource.Spec.Servers {
		if _, ok := unique[customResource.Spec.Servers[i].Name]; ok {
			errs = append(errs, errors.DuplicateItems("spec.servers.name", "body"))
		}

		for _, svc := range customResource.Spec.Servers[i].Services {
			if svc == "data" {
				hasDataService = true
			}
		}

		unique[customResource.Spec.Servers[i].Name] = true
	}

	if !hasDataService {
		err := errors.Required("at least one \"data\" service", "spec.servers[*].services")
		errs = append(errs, err)
	}

	// Validate the cluster is supportable.
	// 1. If any server class has a log volume or a default volume they all should.
	// 2. Log volumes can only be used on server classes containing query, search and eventing services.
	//    Data, index and analytics volumes must use the default mount for data persistence.
	anySupportable := false
	for _, class := range customResource.Spec.Servers {
		if class.VolumeMounts != nil {
			if class.VolumeMounts.DefaultClaim != "" || class.VolumeMounts.LogsClaim != "" {
				anySupportable = true
			}
		}
	}

	if anySupportable {
		for index, class := range customResource.Spec.Servers {
			// Volume mounts must be specified if any others are supportable
			if class.VolumeMounts == nil {
				errs = append(errs, errors.Required("volumeMounts", fmt.Sprintf("spec.servers[%d]", index)))
			} else {
				// These stateful services must have a "default" mount
				if class.Services.ContainsAny(couchbasev2.DataService, couchbasev2.IndexService, couchbasev2.AnalyticsService) &&
					class.VolumeMounts.DefaultClaim == "" {
					errs = append(errs, errors.Required("default", fmt.Sprintf("spec.servers[%d].volumeMounts", index)))
				}
			}
		}
	}

	// validate persistent volume spec such that when volumeMounts are specified, claim for
	// `default` must be provided, and all mounts much pair to associated persistentVolumeClaims.
	// `logs` claim cannot be used in conjunction with `default` claim.
	for index, config := range customResource.Spec.Servers {
		if config.VolumeMounts != nil {
			mounts := config.VolumeMounts

			secondaryMounts := []string{}
			if mounts.DataClaim != "" {
				secondaryMounts = append(secondaryMounts, "data")
			}
			if mounts.IndexClaim != "" {
				secondaryMounts = append(secondaryMounts, "index")
			}
			if mounts.AnalyticsClaims != nil {
				secondaryMounts = append(secondaryMounts, "analytics")
			}
			hasSecondaryMounts := len(secondaryMounts) > 0

			// Check the associated service is enabled
			if mounts.DataClaim != "" && !config.Services.Contains(couchbasev2.DataService) {
				errs = append(errs, fmt.Errorf("spec.servers[%d].volumeMounts.data requires the data service to be enabled", index))
			}
			if mounts.IndexClaim != "" && !config.Services.Contains(couchbasev2.IndexService) {
				errs = append(errs, fmt.Errorf("spec.servers[%d].volumeMounts.index requires the index service to be enabled", index))
			}
			if mounts.AnalyticsClaims != nil && !config.Services.Contains(couchbasev2.AnalyticsService) {
				errs = append(errs, fmt.Errorf("spec.servers[%d].volumeMounts.analytics requires the analytics service to be enabled", index))
			}

			templateNames := customResource.Spec.GetVolumeClaimTemplateNames()
			templateNamesEnum := []interface{}{}
			for _, name := range templateNames {
				templateNamesEnum = append(templateNamesEnum, name)
			}

			if mounts.LogsOnly() {
				if template := customResource.Spec.GetVolumeClaimTemplate(mounts.LogsClaim); template == nil {
					errs = append(errs, errors.EnumFail(fmt.Sprintf("spec.servers[%d].logs", index), "", mounts.LogsClaim, templateNamesEnum))
				}
				if mounts.DefaultClaim != "" || hasSecondaryMounts {
					if mounts.DefaultClaim != "" {
						errs = append(errs, errors.PropertyNotAllowed(fmt.Sprintf("spec.servers[%d].volumeMounts", index), "", "default"))
					}
					for _, secondaryMount := range secondaryMounts {
						errs = append(errs, errors.PropertyNotAllowed(fmt.Sprintf("spec.servers[%d].volumeMounts", index), "", secondaryMount))
					}
				}
			} else if mounts.DefaultClaim != "" {
				if template := customResource.Spec.GetVolumeClaimTemplate(mounts.DefaultClaim); template == nil {
					errs = append(errs, errors.EnumFail(fmt.Sprintf("spec.servers[%d].default", index), "", mounts.DefaultClaim, templateNamesEnum))
				}
				if mounts.DataClaim != "" {
					if template := customResource.Spec.GetVolumeClaimTemplate(mounts.DataClaim); template == nil {
						errs = append(errs, errors.EnumFail(fmt.Sprintf("spec.servers[%d].data", index), "", mounts.DataClaim, templateNamesEnum))
					}
				}
				if mounts.IndexClaim != "" {
					if template := customResource.Spec.GetVolumeClaimTemplate(mounts.IndexClaim); template == nil {
						errs = append(errs, errors.EnumFail(fmt.Sprintf("spec.servers[%d].index", index), "", mounts.IndexClaim, templateNamesEnum))
					}
				}
				if len(mounts.AnalyticsClaims) > 0 {
					for analyticsIndex, claim := range mounts.AnalyticsClaims {
						if template := customResource.Spec.GetVolumeClaimTemplate(claim); template == nil {
							errs = append(errs, errors.EnumFail(fmt.Sprintf("spec.servers[%d].analytics[%d]", index, analyticsIndex), "", claim, templateNamesEnum))
						}
					}
				}
			} else if hasSecondaryMounts {
				errs = append(errs, errors.Required("default", fmt.Sprintf("spec.servers[%d].volumeMounts", index)))
			}
		}
	}

	// validate claim templates such that storage class is provided along with valid request
	pvcMap := map[string]bool{}
	for i, pvc := range customResource.Spec.VolumeClaimTemplates {
		hasStorageQuantity := false
		if quantity, ok := pvc.Spec.Resources.Requests["storage"]; ok {
			hasStorageQuantity = hasStorageQuantity || !quantity.IsZero()
		}
		if quantity, ok := pvc.Spec.Resources.Limits["storage"]; ok {
			hasStorageQuantity = hasStorageQuantity || !quantity.IsZero()
		}
		if !hasStorageQuantity {
			err := errors.Required(`"storage"`, "spec.volumeClaimTemplates[*].resources.requests|limits")
			errs = append(errs, err)
		}

		pvcName := pvc.ObjectMeta.Name
		if pvcMap[pvcName] {
			err := errors.DuplicateItems(fmt.Sprintf("spec.volumeClaimTemplates[%d].metadata.name", i), "body")
			errs = append(errs, err)
		} else {
			pvcMap[pvcName] = true
		}

		// Ensure storageClass exists
		if pvc.Spec.StorageClassName != nil {
			storageClass, err := v.Abstraction.GetStorageClass(*pvc.Spec.StorageClassName)
			if err != nil {
				// Silently ignore permissions errors, some users may not want us seeing these resources.
				if !apierrors.IsForbidden(err) {
					errs = append(errs, err)
				}
			} else if storageClass == nil {
				errs = append(errs, fmt.Errorf("storage class %s must exist", *pvc.Spec.StorageClassName))
			}
		}
	}

	// version check
	_, err := k8sutil.CouchbaseVersion(customResource.Spec.Image)
	if err != nil {
		errs = append(errs, fmt.Errorf("unsupported Couchbase version"))
	}

	// Record the zones that the server certificate needs to support as we look at the network configuration.
	subjectAltNames := util_x509.MandatorySANs(customResource.Name, customResource.Namespace)
	if customResource.Spec.Networking.DNS != nil {
		subjectAltNames = append(subjectAltNames, fmt.Sprintf("*.%s", customResource.Spec.Networking.DNS.Domain))
	}

	// Check TLS
	errs = append(errs, validateTLS(v, customResource, subjectAltNames)...)
	errs = append(errs, validateTLSXDCR(v, customResource)...)

	// Require that publically visible service ports have DNS information available.
	if customResource.Spec.IsExposedFeatureServiceTypePublic() || customResource.Spec.IsAdminConsoleServiceTypePublic() {
		if customResource.Spec.Networking.TLS == nil {
			errs = append(errs, errors.Required("spec.tls", "body"))
		}
		if customResource.Spec.Networking.DNS == nil {
			errs = append(errs, errors.Required("spec.dns", "body"))
		}
	}

	if err := validateMemoryConstraints(v, customResource); err != nil {
		errs = append(errs, err)
	}

	// Check mutual verification
	if customResource.Spec.Networking.TLS != nil && customResource.Spec.Networking.TLS.ClientCertificatePolicy != nil {
		if len(customResource.Spec.Networking.TLS.ClientCertificatePaths) == 0 {
			errs = append(errs, errors.TooFewItems("spec.networking.tls.clientCertificatePaths", "", 1))
		}
	}

	// Check auto compaction
	purgeInterval := customResource.Spec.ClusterSettings.AutoCompaction.TombstonePurgeInterval.Duration.Hours()
	if purgeInterval < 1.0 {
		errs = append(errs, fmt.Errorf("spec.cluster.autoCompaction.tombstonePurgeInterval in body should be greater than or equal to 1h"))
	}
	if purgeInterval > 60.0*24.0 {
		errs = append(errs, fmt.Errorf("spec.cluster.autoCompaction.tombstonePurgeInterval in body should be less than or equal to 60d"))
	}

	// Check LDAP Settings
	if ldap := customResource.Spec.Security.LDAP; ldap != nil {
		if len(ldap.Hosts) == 0 {
			errs = append(errs, errors.TooFewItems("spec.security.ldap.hosts", "", 1))
		}

		// If authentication enabled then require username mapping
		if ldap.AuthenticationEnabled {
			if ldap.UserDNMapping.Template == "" {
				errs = append(errs, errors.Required("spec.security.ldap.userDNMapping", "body"))
			}
		}

		// ca is required when tls is enabled
		if ldap.EnableCertValidation {
			tlsSecretName := customResource.Spec.Security.LDAP.TLSSecret
			if tlsSecretName == "" {
				errs = append(errs, errors.Required("spec.security.ldap.tlsSecret", "body"))
			}

			// secret containing ldap ca must exist
			tlsSecret, err := v.Abstraction.GetSecret(customResource.Namespace, tlsSecretName)
			if err != nil {
				// Silently ignore permissions errors, some users may not want us seeing these resources.
				if apierrors.IsForbidden(err) {
					return nil
				}
				errs = append(errs, err)
			} else if tlsSecret == nil {
				errs = append(errs, fmt.Errorf("secret %s referenced by security.ldap.tlsSecret must exist", tlsSecretName))
			} else {
				if _, ok := tlsSecret.Data["ca.crt"]; !ok {
					errs = append(errs, fmt.Errorf("ldap tls secret %s must contain key 'ca.crt'", tlsSecretName))
				}
			}
		}

		// require groups query when group auth enabled
		if ldap.AuthorizationEnabled {
			if ldap.GroupsQuery == "" {
				errs = append(errs, errors.Required("security.ldap.groupsQuery", "body"))
			}
		}
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckConstraintsBucket(v *types.Validator, bucket *couchbasev2.CouchbaseBucket) error {
	errs := []error{}

	if bucket.Spec.MemoryQuota != nil {
		if bucket.Spec.MemoryQuota.Cmp(*k8sutil.NewResourceQuantityMi(100)) < 0 {
			errs = append(errs, fmt.Errorf("spec.memoryQuota in body should be greater than or equal to 100Mi"))
		}
	}

	if err := validateMemoryConstraints(v, bucket); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckConstraintsEphemeralBucket(v *types.Validator, bucket *couchbasev2.CouchbaseEphemeralBucket) error {
	errs := []error{}

	if bucket.Spec.MemoryQuota != nil {
		if bucket.Spec.MemoryQuota.Cmp(*k8sutil.NewResourceQuantityMi(100)) < 0 {
			errs = append(errs, fmt.Errorf("spec.memoryQuota in body should be greater than or equal to 100Mi"))
		}
	}

	if err := validateMemoryConstraints(v, bucket); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckConstraintsMemcachedBucket(v *types.Validator, bucket *couchbasev2.CouchbaseMemcachedBucket) error {
	errs := []error{}

	if bucket.Spec.MemoryQuota != nil {
		if bucket.Spec.MemoryQuota.Cmp(*k8sutil.NewResourceQuantityMi(100)) < 0 {
			errs = append(errs, fmt.Errorf("spec.memoryQuota in body should be greater than or equal to 100Mi"))
		}
	}

	if err := validateMemoryConstraints(v, bucket); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckConstraintsReplication(v *types.Validator, replication *couchbasev2.CouchbaseReplication) error {
	return nil
}

func CheckConstraintsCouchbaseUser(v *types.Validator, user *couchbasev2.CouchbaseUser) error {
	errs := []error{}

	// only 'local' and 'ldap' auth domains accepted
	domain := user.Spec.AuthDomain
	if domain == couchbasev2.InternalAuthDomain {
		// password is required for internal auth domain
		authSecretName := user.Spec.AuthSecret
		if authSecretName == "" {
			emsg := fmt.Sprintf("spec.authSecret for `%s` domain", domain)
			errs = append(errs, errors.Required(emsg, user.Name))
		} else {
			// Check the ldap auth secret exists and has the correct keys
			authSecret, err := v.Abstraction.GetSecret(user.Namespace, authSecretName)
			if err != nil {
				// Silently ignore permissions errors, some users may not want us seeing these resources.
				if apierrors.IsForbidden(err) {
					return nil
				}
				errs = append(errs, err)
			} else if authSecret == nil {
				errs = append(errs, fmt.Errorf("secret %s referenced by user.spec.authSecret for `%s` must exist", authSecretName, user.Name))
			} else {
				if _, ok := authSecret.Data["password"]; !ok {
					errs = append(errs, fmt.Errorf("ldap auth secret %s must contain password", authSecretName))
				}
			}
		}
	} else if domain == couchbasev2.LDAPAuthDomain {
		// authSecret not accepted for LDAP user
		if authSecretName := user.Spec.AuthSecret; authSecretName != "" {
			errs = append(errs, fmt.Errorf("secret %s not allowed for LDAP user `%s`", authSecretName, user.Name))
		}
	} else {
		return fmt.Errorf("unknown auth domain: %s", user.Spec.AuthDomain)
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}
	return nil
}

func CheckConstraintsBackup(v *types.Validator, backup *couchbasev2.CouchbaseBackup) error {
	errs := []error{}

	if err := validateBackupCronSchedules(backup); err != nil {
		errs = err
	}

	if backup.Spec.Size.Value() <= 0 {
		errs = append(errs, fmt.Errorf("size: %d must be greater than 0", backup.Spec.Size.Value()))
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}
	return nil
}

func CheckConstraintsBackupRestore(v *types.Validator, restore *couchbasev2.CouchbaseBackupRestore) error {
	errs := []error{}

	if len(restore.Spec.Backup) == 0 && len(restore.Spec.Repo) == 0 {
		errs = append(errs, fmt.Errorf("both Spec.Backup and Spec.Repo fields are empty. Please supply a value for at least one"))
	}

	start := restore.Spec.Start
	// start is required
	if start == nil {
		errs = append(errs, fmt.Errorf("specify a start point or backup"))
	} else {
		// both str and int are specified
		if start.Str != nil && start.Int != nil {
			errs = append(errs, fmt.Errorf("specify just one value, either Str or Int"))
		}
	}

	end := restore.Spec.End
	// if end has been specified
	if start != nil && end != nil {
		// both str and int are specified
		if end.Str != nil && end.Int != nil {
			errs = append(errs, fmt.Errorf("specify just one value, either Str or Int"))
		}

		// end and start differ
		if end != start {
			// start and end are using string arguments
			if end.Str != nil && start.Str != nil {
				if *end.Str == "oldest" && *start.Str == "newest" {
					errs = append(errs, fmt.Errorf("start point %s is after end point %s", *start.Str, *end.Str))
				}
			}

			// start and end are using integer arguments
			if start.Int != nil && end.Int != nil {
				if *start.Int > *end.Int {
					errs = append(errs, fmt.Errorf("start integer cannot be larger than end integer"))
				}
			}
		}
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}
	return nil
}

func CheckConstraintsCouchbaseGroup(v *types.Validator, group *couchbasev2.CouchbaseGroup) error {
	errs := []error{}

	for index, role := range group.Spec.Roles {
		// role itself must be valid
		isCluterRole := couchbasev2.IsClusterRole(role.Name)
		// Bucket cannot be used with cluster role
		if role.Bucket != "" && isCluterRole {
			errs = append(errs, errors.PropertyNotAllowed(fmt.Sprintf("spec.roles[%d].bucket for cluster role", index), "", string(role.Name)))
		}

	}
	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}
	return nil
}

// validateTLS checks TLS configuration exists and is valid
// * correct secrets exist
// * correct keys exist in the secrets
// * cerificate chain validates with the CA
// * certificates are
//   * in date
//   * have the correct attributes
// * leaf certificate has the correct SANs
func validateTLS(v *types.Validator, cluster *couchbasev2.CouchbaseCluster, subjectAltNames []string) (errs []error) {
	if cluster.Spec.Networking.TLS != nil {
		// CRD validation requires all the necessary fields are populated
		operatorSecretName := cluster.Spec.Networking.TLS.Static.OperatorSecret
		serverSecretName := cluster.Spec.Networking.TLS.Static.ServerSecret

		var key []byte
		var chain []byte
		var ca []byte
		var ok bool

		// Check the operator secret exists and has the correct keys
		operatorSecret, err := v.Abstraction.GetSecret(cluster.Namespace, operatorSecretName)
		if err != nil {
			// Silently ignore permissions errors, some users may not want us seeing these resources.
			if apierrors.IsForbidden(err) {
				return
			}
			errs = append(errs, err)
		} else if operatorSecret == nil {
			errs = append(errs, fmt.Errorf("secret %s referenced by spec.networking.tls.static.operatorSecret must exist", operatorSecretName))
		} else {
			if ca, ok = operatorSecret.Data["ca.crt"]; !ok {
				errs = append(errs, fmt.Errorf("tls operator secret %s must contain ca.crt", operatorSecretName))
			}
		}

		// Check the server secret exists and has the correct keys
		serverSecret, err := v.Abstraction.GetSecret(cluster.Namespace, serverSecretName)
		if err != nil {
			// Silently ignore permissions errors, some users may not want us seeing these resources.
			if apierrors.IsForbidden(err) {
				return
			}
			errs = append(errs, err)
		} else if serverSecret == nil {
			errs = append(errs, fmt.Errorf("secret %s referenced by spec.networking.tls.static.serverSecret must exist", serverSecretName))
		} else {
			if chain, ok = serverSecret.Data["chain.pem"]; !ok {
				errs = append(errs, fmt.Errorf("tls server secret %s must contain chain.pem", serverSecretName))
			}
			if key, ok = serverSecret.Data["pkey.key"]; !ok {
				errs = append(errs, fmt.Errorf("tls server secret %s must contain pkey.key", serverSecretName))
			}
		}

		// Something is wrong, bomb out now
		if len(errs) > 0 {
			return
		}

		// Validate the TLS configuration is going to work
		errs = util_x509.Verify(ca, chain, key, x509.ExtKeyUsageServerAuth, subjectAltNames)

		// Do client certificate verification if necessary
		if cluster.Spec.Networking.TLS.ClientCertificatePolicy != nil {
			if chain, ok = operatorSecret.Data["couchbase-operator.crt"]; !ok {
				errs = append(errs, fmt.Errorf("tls operator secret %s must contain couchbase-operator.crt", operatorSecretName))
			}
			if key, ok = operatorSecret.Data["couchbase-operator.key"]; !ok {
				errs = append(errs, fmt.Errorf("tls operator secret %s must contain couchbase-operator.key", operatorSecretName))
			}

			// Something is wrong, bomb out now
			if len(errs) > 0 {
				return
			}

			// Validate the TLS configuration is going to work
			errs = util_x509.Verify(ca, chain, key, x509.ExtKeyUsageClientAuth, nil)
		}
	}
	return
}

// validateTLSXDCR checks that TLS configuration for a remote cluster is valid.
// * if set the secret must exist
// * if set the secret must contain a CA
func validateTLSXDCR(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) (errs []error) {
	for _, remoteCluster := range cluster.Spec.XDCR.RemoteClusters {
		if remoteCluster.TLS == nil {
			continue
		}
		if remoteCluster.TLS.Secret != nil {
			secret, err := v.Abstraction.GetSecret(cluster.Namespace, *remoteCluster.TLS.Secret)
			if err != nil {
				// Silently ignore permissions errors, some users may not want us seeing these resources.
				if apierrors.IsForbidden(err) {
					return
				}
				errs = append(errs, err)
				continue
			}
			if secret == nil {
				errs = append(errs, fmt.Errorf("xdcr tls secret %s for remote cluster %s must exist", *remoteCluster.TLS.Secret, remoteCluster.Name))
				continue
			}
			if _, ok := secret.Data[couchbasev2.RemoteClusterTLSCA]; !ok {
				errs = append(errs, fmt.Errorf("xdcr tls secret %s for remote cluster %s must contain key 'ca'", *remoteCluster.TLS.Secret, remoteCluster.Name))
				continue
			}
		}
	}
	return
}

// validateMemoryConstraints works in two different ways:
// * If a cluster is specified we are creating or updating cluster. Look up all buckets selected by it
//   and ensure the total memory requirements do not surpass the data service memory quota.
// * If a bucket is specified then a bucket is being created or updated.  Look up all clusters that
//   may select the bucket and ensure the total memory requirements do not surpass the data service memory
//   quota for each cluster.
func validateMemoryConstraints(v *types.Validator, object runtime.Object) error {
	var namespace string
	switch t := object.(type) {
	case *couchbasev2.CouchbaseCluster:
		return validateClusterMemoryConstraints(v, t)
	case *couchbasev2.CouchbaseBucket:
		namespace = t.Namespace
	case *couchbasev2.CouchbaseEphemeralBucket:
		namespace = t.Namespace
	case *couchbasev2.CouchbaseMemcachedBucket:
		namespace = t.Namespace
	default:
		return fmt.Errorf("validate memory constraints: unsupported type")
	}

	clusters, err := v.Abstraction.GetCouchbaseClusters(namespace)
	if err != nil {
		return err
	}

	for _, cluster := range clusters.Items {
		if err := validateClusterMemoryConstraints(v, &cluster); err != nil {
			return err
		}
	}

	return nil
}

// validateClusterMemoryConstraints given a cluster loads all buckets associated with it and
// validates that the allocated memory does not exceed the memory allocated for the
// data service.
func validateClusterMemoryConstraints(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if !cluster.Spec.Buckets.Managed {
		return nil
	}

	buckets, err := v.Abstraction.GetCouchbaseBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return err
	}
	ephemeralBuckets, err := v.Abstraction.GetCouchbaseEphemeralBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return err
	}
	memcachedBuckets, err := v.Abstraction.GetCouchbaseMemcachedBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return err
	}

	allocated := resource.NewQuantity(0, resource.BinarySI)
	for _, bucket := range buckets.Items {
		allocated.Add(*bucket.Spec.MemoryQuota)
	}
	for _, bucket := range ephemeralBuckets.Items {
		allocated.Add(*bucket.Spec.MemoryQuota)
	}
	for _, bucket := range memcachedBuckets.Items {
		allocated.Add(*bucket.Spec.MemoryQuota)
	}

	if allocated.Cmp(*cluster.Spec.ClusterSettings.DataServiceMemQuota) > 0 {
		return fmt.Errorf("bucket memory allocation (%v) exceeds data service quota (%v) on cluster %s", allocated, cluster.Spec.ClusterSettings.DataServiceMemQuota, cluster.Name)
	}

	return nil
}

// validateBucketExists ensures the specified Couchbase bucket exists.
func validateBucketExists(v *types.Validator, cluster *couchbasev2.CouchbaseCluster, name string) error {
	buckets, err := v.Abstraction.GetCouchbaseBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return err
	}
	ephemeralBuckets, err := v.Abstraction.GetCouchbaseEphemeralBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return err
	}

	for _, bucket := range buckets.Items {
		if bucket.Name == name {
			return nil
		}
	}
	for _, bucket := range ephemeralBuckets.Items {
		if bucket.Name == name {
			return nil
		}
	}

	memcachedBuckets, err := v.Abstraction.GetCouchbaseMemcachedBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return err
	}

	for _, bucket := range memcachedBuckets.Items {
		if bucket.Name == name {
			return fmt.Errorf("memcached bucket %s cannot be replicated", name)
		}
	}

	return fmt.Errorf("bucket %s not found", name)
}

// validateBackupCronSchedules ensures that the correct cronjob schedules are valid for the desired backup strategy.
func validateBackupCronSchedules(backup *couchbasev2.CouchbaseBackup) []error {
	errs := []error{}

	switch backup.Spec.Strategy {
	case couchbasev2.FullIncremental:
		if err := validateCronJobString(backup.Spec.Incremental, "spec.incremental"); err != nil {
			errs = append(errs, err)
		}
		if err := validateCronJobString(backup.Spec.Full, "spec.full"); err != nil {
			errs = append(errs, err)
		}
	case couchbasev2.FullOnly:
		if err := validateCronJobString(backup.Spec.Full, "spec.full"); err != nil {
			errs = append(errs, err)
		}
	default:
		errs = append(errs, fmt.Errorf("specified strategy not valid, must be one of %s | %s",
			couchbasev2.FullIncremental, couchbasev2.FullOnly))
	}

	return errs
}

func validateCronJobString(schedule *couchbasev2.CouchbaseBackupSchedule, name string) error {
	if schedule == nil || len(schedule.Schedule) == 0 {
		return fmt.Errorf("cronjob schedule %s cannot be empty", name)
	}

	p := cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	if _, err := p.Parse(schedule.Schedule); err != nil {
		return err
	}

	return nil
}

func CheckImmutableFields(current, updated *couchbasev2.CouchbaseCluster) error {
	errs := []error{}

	if current.Spec.AntiAffinity != updated.Spec.AntiAffinity {
		errs = append(errs, util.NewUpdateError("spec.antiAffinity", "body"))
	}

	if current.Spec.Security.AdminSecret != updated.Spec.Security.AdminSecret {
		err := util.NewUpdateError("spec.authSecret", "body")
		errs = append(errs, err)
	}

	if !util.StringArrayCompare(current.Spec.ServerGroups, updated.Spec.ServerGroups) {
		errs = append(errs, util.NewUpdateError("spec.serverGroups", "body"))
	}

	for _, cur := range current.Spec.Servers {
		for i, up := range updated.Spec.Servers {
			if cur.Name == up.Name {
				if !util.StringArrayCompare(cur.ServerGroups, up.ServerGroups) {
					errs = append(errs, util.NewUpdateError(fmt.Sprintf("spec.servers[%d].serverGroups", i), "body"))
				}
				if !util.StringArrayCompare(cur.Services.StringSlice(), up.Services.StringSlice()) {
					err := util.NewUpdateError(fmt.Sprintf("spec.servers[%d].services", i), "body")
					errs = append(errs, err)
				}
			}
		}
	}

	// Check to see if either the old or new specification have the the index
	// service defined. If they do then we cannot change the indexStorageSetting.
	hasIndexSvc := false
	for _, cur := range current.Spec.Servers {
		for _, svc := range cur.Services {
			if svc == couchbasev2.IndexService {
				hasIndexSvc = true
			}
		}
	}

	for _, up := range updated.Spec.Servers {
		for _, svc := range up.Services {
			if svc == couchbasev2.IndexService {
				hasIndexSvc = true
			}
		}
	}

	if hasIndexSvc && updated.Spec.ClusterSettings.IndexStorageSetting != current.Spec.ClusterSettings.IndexStorageSetting {
		err := util.NewUpdateError("spec.cluster.indexStorageSetting", "body")
		errs = append(errs, err)
	}

	// Upgrade validation
	// * Deny downgrades if no upgrade in progress
	// * Deny upgrade if across major versions
	// * Deny rollback if it doesn't match the current version
	currentVersion, err := k8sutil.CouchbaseVersion(current.Spec.Image)
	if err != nil {
		errs = append(errs, err)
	}
	updatedVersion, err := k8sutil.CouchbaseVersion(updated.Spec.Image)
	if err != nil {
		errs = append(errs, err)
	}

	upgradeCondition := current.Status.GetCondition(couchbasev2.ClusterConditionUpgrading)
	if upgradeCondition == nil && currentVersion != updatedVersion {
		src, err := couchbaseutil.NewVersion(currentVersion)
		if err != nil {
			errs = append(errs, err)
		}
		dst, err := couchbaseutil.NewVersion(updatedVersion)
		if err != nil {
			errs = append(errs, err)
		}
		if dst.Less(src) {
			errs = append(errs, fmt.Errorf("spec.Version in body should be greater than %s", src.Semver()))
		}
		if dst.Major() > src.Major()+1 {
			max, _ := couchbaseutil.NewVersion(fmt.Sprintf("%d.0.0", src.Major()+2))
			errs = append(errs, fmt.Errorf("spec.Version in body should be less than %s", max.Semver()))
		}
	}
	if upgradeCondition != nil && currentVersion != updatedVersion {
		if updatedVersion != current.Status.CurrentVersion {
			errs = append(errs, util.NewUpdateError("spec.version", "body"))
		}
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckImmutableFieldsBucket(prev, curr *couchbasev2.CouchbaseBucket) error {
	errs := []error{}

	if prev.Spec.ConflictResolution != curr.Spec.ConflictResolution {
		errs = append(errs, util.NewUpdateError("spec.conflictResolution", "body"))
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckImmutableFieldsEphemeralBucket(prev, curr *couchbasev2.CouchbaseEphemeralBucket) error {
	errs := []error{}

	if prev.Spec.ConflictResolution != curr.Spec.ConflictResolution {
		errs = append(errs, util.NewUpdateError("spec.conflictResolution", "body"))
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckImmutableFieldsMemcachedBucket(prev, curr *couchbasev2.CouchbaseMemcachedBucket) error {
	return nil
}

func CheckImmutableFieldsReplication(prev, curr *couchbasev2.CouchbaseReplication) error {
	errs := []error{}

	if prev.Spec.Bucket != curr.Spec.Bucket {
		errs = append(errs, util.NewUpdateError("spec.bucket", "body"))
	}
	if prev.Spec.RemoteBucket != curr.Spec.RemoteBucket {
		errs = append(errs, util.NewUpdateError("spec.remoteBucket", "body"))
	}
	if prev.Spec.FilterExpression != curr.Spec.FilterExpression {
		errs = append(errs, util.NewUpdateError("spec.filterExpression", "body"))
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckImmutableFieldsBackup(prev, curr *couchbasev2.CouchbaseBackup) error {
	errs := []error{}

	if prev.Spec.Strategy != curr.Spec.Strategy {
		errs = append(errs, util.NewUpdateError("spec.strategy", "body"))
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}
