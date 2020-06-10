package v2

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"

	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	minimumServersSize           float64 = 1
	minimumItemLength            int64   = 1
	minimumServicesLength        int64   = 1
	minimumServersLength         int64   = 1
	minimumStringLength          int64   = 1
	minimumBucketReplicas        float64 = 0
	maximumBucketReplicas        float64 = 3
	maximumBucketNameLength      int64   = 100
	minimumJobsHistorySize       float64 = 0
	minimumBackupIndexSize       float64 = 1
	minimumAutofailoverMaxCount  float64 = 1
	maximumAutofailoverMaxCount  float64 = 3
	minimumLogRetentionCount     float64 = 0
	minimumAutoCompactionPercent float64 = 2
	maximumAutoCompactionPercent float64 = 100
)

const (
	ImagePattern      = `^(.*?(:\d+)?/)?.*?/.*?(:.*?\d+\.\d+\.\d+.*|@sha256:[0-9a-f]{64})$`
	wallClockTime     = `^(2[0-3]|[01]?[0-9]):([0-5]?[0-9])$`
	bucketNamePattern = `^[a-zA-Z0-9-_%\.]+$`
	BucketPattern     = `^\*$|^[a-zA-Z0-9-_%\.]+$`
)

var (
	categories = []string{
		"all",
		"couchbase",
	}
)

func GetCouchbaseBucketCRD() *apiextensionsv1beta1.CustomResourceDefinition {
	return &apiextensionsv1beta1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiextensionsv1beta1.SchemeGroupVersion.String(),
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: couchbasev2.BucketCRDName,
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group: couchbasev2.SchemeGroupVersion.Group,
			Scope: apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural:     couchbasev2.BucketCRDResourcePlural,
				Kind:       couchbasev2.BucketCRDResourceKind,
				Categories: categories,
			},
			AdditionalPrinterColumns: []apiextensionsv1beta1.CustomResourceColumnDefinition{
				{
					Name:        "Memory Quota",
					Type:        "string",
					Description: "Maximum memory size in MiB of the bucket",
					JSONPath:    ".spec.memoryQuota",
				},
				{
					Name:        "Replicas",
					Type:        "integer",
					Description: "Number of document replications",
					JSONPath:    ".spec.replicas",
				},
				{
					Name:        "IO Priority",
					Type:        "string",
					Description: "IO Priority",
					JSONPath:    ".spec.ioPriority",
				},
				{
					Name:        "Eviction Policy",
					Type:        "string",
					Description: "Policy used to evict documents from memory onto storage",
					JSONPath:    ".spec.evictionPolicy",
				},
				{
					Name:        "Conflict resolution",
					Type:        "string",
					Description: "How to choose a winner when document revisions collide",
					JSONPath:    ".spec.conflictResolution",
				},
				{
					Name:     "Age",
					Type:     "date",
					JSONPath: ".metadata.creationTimestamp",
				},
			},
			Versions: []apiextensionsv1beta1.CustomResourceDefinitionVersion{
				{
					Name: "v1",
				},
				{
					Name:    "v2",
					Served:  true,
					Storage: true,
				},
			},
			Validation: &apiextensionsv1beta1.CustomResourceValidation{
				OpenAPIV3Schema: &apiextensionsv1beta1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
						"spec": apiextensionsv1beta1.JSONSchemaProps{
							Type: "object",
							Required: []string{
								"memoryQuota",
								"ioPriority",
								"evictionPolicy",
								"conflictResolution",
								"compressionMode",
							},
							Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
								"name": apiextensionsv1beta1.JSONSchemaProps{
									Type:      "string",
									Pattern:   bucketNamePattern,
									MaxLength: &maximumBucketNameLength,
								},
								"memoryQuota": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"replicas": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "integer",
									Minimum: &minimumBucketReplicas,
									Maximum: &maximumBucketReplicas,
								},
								"ioPriority": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "string",
									Pattern: "^high|low$",
								},
								"evictionPolicy": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "string",
									Pattern: "^valueOnly|fullEviction$",
								},
								"conflictResolution": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "string",
									Pattern: "^seqno|lww$",
								},
								"enableFlush": apiextensionsv1beta1.JSONSchemaProps{
									Type: "boolean",
								},
								"enableIndexReplica": apiextensionsv1beta1.JSONSchemaProps{
									Type: "boolean",
								},
								"compressionMode": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "string",
									Pattern: "^off|passive|active$",
								},
							},
						},
					},
				},
			},
		},
	}
}

func GetCouchbaseEphemeralBucketCRD() *apiextensionsv1beta1.CustomResourceDefinition {
	return &apiextensionsv1beta1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiextensionsv1beta1.SchemeGroupVersion.String(),
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: couchbasev2.EphemeralBucketCRDName,
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group: couchbasev2.SchemeGroupVersion.Group,
			Scope: apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural:     couchbasev2.EphemeralBucketCRDResourcePlural,
				Kind:       couchbasev2.EphemeralBucketCRDResourceKind,
				Categories: categories,
			},
			AdditionalPrinterColumns: []apiextensionsv1beta1.CustomResourceColumnDefinition{
				{
					Name:        "Memory Quota",
					Type:        "string",
					Description: "Maximum memory size in MiB of the bucket",
					JSONPath:    ".spec.memoryQuota",
				},
				{
					Name:        "Replicas",
					Type:        "integer",
					Description: "Number of document replications",
					JSONPath:    ".spec.replicas",
				},
				{
					Name:        "IO Priority",
					Type:        "string",
					Description: "IO Priority",
					JSONPath:    ".spec.ioPriority",
				},
				{
					Name:        "Eviction Policy",
					Type:        "string",
					Description: "Policy used to evict documents from memory onto storage",
					JSONPath:    ".spec.evictionPolicy",
				},
				{
					Name:        "Conflict resolution",
					Type:        "string",
					Description: "How to choose a winner when document revisions collide",
					JSONPath:    ".spec.conflictResolution",
				},
				{
					Name:     "Age",
					Type:     "date",
					JSONPath: ".metadata.creationTimestamp",
				},
			},
			Versions: []apiextensionsv1beta1.CustomResourceDefinitionVersion{
				{
					Name: "v1",
				},
				{
					Name:    "v2",
					Served:  true,
					Storage: true,
				},
			},
			Validation: &apiextensionsv1beta1.CustomResourceValidation{
				OpenAPIV3Schema: &apiextensionsv1beta1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
						"spec": apiextensionsv1beta1.JSONSchemaProps{
							Type: "object",
							Required: []string{
								"memoryQuota",
								"ioPriority",
								"evictionPolicy",
								"conflictResolution",
								"compressionMode",
							},
							Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
								"name": apiextensionsv1beta1.JSONSchemaProps{
									Type:      "string",
									Pattern:   bucketNamePattern,
									MaxLength: &maximumBucketNameLength,
								},
								"memoryQuota": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"replicas": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "integer",
									Minimum: &minimumBucketReplicas,
									Maximum: &maximumBucketReplicas,
								},
								"ioPriority": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "string",
									Pattern: "^high|low$",
								},
								"evictionPolicy": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "string",
									Pattern: "^noEviction|nruEviction$",
								},
								"conflictResolution": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "string",
									Pattern: "^seqno|lww$",
								},
								"enableFlush": apiextensionsv1beta1.JSONSchemaProps{
									Type: "boolean",
								},
								"compressionMode": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "string",
									Pattern: "^off|passive|active$",
								},
							},
						},
					},
				},
			},
		},
	}
}

func GetCouchbaseMemcachedBucketCRD() *apiextensionsv1beta1.CustomResourceDefinition {
	return &apiextensionsv1beta1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiextensionsv1beta1.SchemeGroupVersion.String(),
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: couchbasev2.MemcachedBucketCRDName,
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group: couchbasev2.SchemeGroupVersion.Group,
			Scope: apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural:     couchbasev2.MemcachedBucketCRDResourcePlural,
				Kind:       couchbasev2.MemcachedBucketCRDResourceKind,
				Categories: categories,
			},
			AdditionalPrinterColumns: []apiextensionsv1beta1.CustomResourceColumnDefinition{
				{
					Name:        "Memory Quota",
					Type:        "string",
					Description: "Maximum memory size in MiB of the bucket",
					JSONPath:    ".spec.memoryQuota",
				},
				{
					Name:     "Age",
					Type:     "date",
					JSONPath: ".metadata.creationTimestamp",
				},
			},
			Versions: []apiextensionsv1beta1.CustomResourceDefinitionVersion{
				{
					Name: "v1",
				},
				{
					Name:    "v2",
					Served:  true,
					Storage: true,
				},
			},
			Validation: &apiextensionsv1beta1.CustomResourceValidation{
				OpenAPIV3Schema: &apiextensionsv1beta1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
						"spec": apiextensionsv1beta1.JSONSchemaProps{
							Type: "object",
							Required: []string{
								"memoryQuota",
							},
							Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
								"name": apiextensionsv1beta1.JSONSchemaProps{
									Type:      "string",
									Pattern:   bucketNamePattern,
									MaxLength: &maximumBucketNameLength,
								},
								"memoryQuota": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"enableFlush": apiextensionsv1beta1.JSONSchemaProps{
									Type: "boolean",
								},
							},
						},
					},
				},
			},
		},
	}
}

func GetCouchbaseReplicationCRD() *apiextensionsv1beta1.CustomResourceDefinition {
	return &apiextensionsv1beta1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiextensionsv1beta1.SchemeGroupVersion.String(),
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: couchbasev2.ReplicationCRDName,
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group: couchbasev2.SchemeGroupVersion.Group,
			Scope: apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural:     couchbasev2.ReplicationCRDResourcePlural,
				Kind:       couchbasev2.ReplicationCRDResourceKind,
				Categories: categories,
			},
			AdditionalPrinterColumns: []apiextensionsv1beta1.CustomResourceColumnDefinition{
				{
					Name:        "Bucket",
					Type:        "string",
					Description: "Bucket to replicate from",
					JSONPath:    ".spec.bucket",
				},
				{
					Name:        "Remote Bucket",
					Type:        "string",
					Description: "Bucket to replicate to",
					JSONPath:    ".spec.bucket",
				},
				{
					Name:        "Paused",
					Type:        "boolean",
					Description: "Whether the replication is paused",
					JSONPath:    ".spec.paused",
				},
				{
					Name:     "Age",
					Type:     "date",
					JSONPath: ".metadata.creationTimestamp",
				},
			},
			Versions: []apiextensionsv1beta1.CustomResourceDefinitionVersion{
				{
					Name: "v1",
				},
				{
					Name:    "v2",
					Served:  true,
					Storage: true,
				},
			},
			Validation: &apiextensionsv1beta1.CustomResourceValidation{
				OpenAPIV3Schema: &apiextensionsv1beta1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
						"spec": apiextensionsv1beta1.JSONSchemaProps{
							Type: "object",
							Required: []string{
								"bucket",
								"remoteBucket",
								"compressionType",
							},
							Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
								"bucket": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"remoteBucket": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"compressionType": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "string",
									Pattern: "^None|Auto|Snappy$",
								},
								"filterExpression": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"paused": apiextensionsv1beta1.JSONSchemaProps{
									Type: "boolean",
								},
							},
						},
					},
				},
			},
		},
	}
}

func GetCouchbaseBackupCRD() *apiextensionsv1beta1.CustomResourceDefinition {
	return &apiextensionsv1beta1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiextensionsv1beta1.SchemeGroupVersion.String(),
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: couchbasev2.BackupCRDName,
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group: couchbasev2.SchemeGroupVersion.Group,
			Scope: apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural:     couchbasev2.BackupCRDResourcePlural,
				Kind:       couchbasev2.BackupCRDResourceKind,
				ShortNames: []string{couchbasev2.BackupCRDResourceShortName},
				Categories: categories,
			},
			AdditionalPrinterColumns: []apiextensionsv1beta1.CustomResourceColumnDefinition{
				{
					Name:        "Strategy",
					Type:        "string",
					Description: "what backup strategy is being followed",
					JSONPath:    ".spec.strategy",
				},
				{
					Name:        "Volume Size",
					Type:        "string",
					Description: "total size of the backup volume",
					JSONPath:    ".spec.size",
				},
				{
					Name:        "Capacity Used",
					Type:        "string",
					Description: "total size of the backup volume",
					JSONPath:    ".status.capacityUsed",
				},
				{
					Name:        "Last Run",
					Type:        "string",
					Description: "last time backup was run",
					JSONPath:    ".status.lastRun",
				},
				{
					Name:        "Last Success",
					Type:        "string",
					Description: "last time backup was successful",
					JSONPath:    ".status.lastSuccess",
				},
				{
					Name:     "Running",
					Type:     "boolean",
					JSONPath: ".status.running",
				},
				{
					Name:     "Age",
					Type:     "date",
					JSONPath: ".metadata.creationTimestamp",
				},
			},
			Versions: []apiextensionsv1beta1.CustomResourceDefinitionVersion{
				{
					Name: "v1",
				},
				{
					Name:    "v2",
					Served:  true,
					Storage: true,
				},
			},
			Validation: &apiextensionsv1beta1.CustomResourceValidation{
				OpenAPIV3Schema: &apiextensionsv1beta1.JSONSchemaProps{
					Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
						"spec": apiextensionsv1beta1.JSONSchemaProps{
							Type: "object",
							Required: []string{
								"strategy",
							},
							Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
								"strategy": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "string",
									Pattern: "^full_incremental|full_only$",
								},
								"incremental": apiextensionsv1beta1.JSONSchemaProps{
									Type: "object",
									Required: []string{
										"schedule",
									},
									Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
										"schedule": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
										},
									},
								},
								"full": apiextensionsv1beta1.JSONSchemaProps{
									Type: "object",
									Required: []string{
										"schedule",
									},
									Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
										"schedule": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
										},
									},
								},
								"successfulJobsHistoryLimit": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "integer",
									Minimum: &minimumJobsHistorySize,
								},
								"failedJobsHistoryLimit": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "integer",
									Minimum: &minimumJobsHistorySize,
								},
								"backupRetention": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "string",
									Pattern: `^\d+(ns|us|ms|s|m|h)$`,
								},
								"logRetention": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "string",
									Pattern: `^\d+(ns|us|ms|s|m|h)$`,
								},
								"size": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
							},
						},
						"status": apiextensionsv1beta1.JSONSchemaProps{
							Type: "object",
							Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
								"capacityUsed": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"archive": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"repo": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"repoList": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"running": apiextensionsv1beta1.JSONSchemaProps{
									Type: "boolean",
								},
								"failed": apiextensionsv1beta1.JSONSchemaProps{
									Type: "boolean",
								},
								"output": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"pod": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"job": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"cronjob": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"duration": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"lastFailure": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"lastSuccess": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"lastRun": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
							},
						},
					},
				},
			},
		},
	}
}

func GetCouchbaseBackupRestoreCRD() *apiextensionsv1beta1.CustomResourceDefinition {
	return &apiextensionsv1beta1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiextensionsv1beta1.SchemeGroupVersion.String(),
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: couchbasev2.BackupRestoreCRDName,
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group: couchbasev2.SchemeGroupVersion.Group,
			Scope: apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural:     couchbasev2.BackupRestoreCRDResourcePlural,
				Kind:       couchbasev2.BackupRestoreCRDResourceKind,
				ShortNames: []string{couchbasev2.BackupRestoreCRDResourceShortName},
				Categories: categories,
			},
			AdditionalPrinterColumns: []apiextensionsv1beta1.CustomResourceColumnDefinition{
				{
					Name:        "Capacity Used",
					Type:        "string",
					Description: "total size of the backup volume",
					JSONPath:    ".status.capacityUsed",
				},
				{
					Name:        "Last Run",
					Type:        "string",
					Description: "last time backup was run",
					JSONPath:    ".status.lastRun",
				},
				{
					Name:        "Last Success",
					Type:        "string",
					Description: "last time restore was successful",
					JSONPath:    ".status.lastSuccess",
				},
				{
					Name:     "Duration",
					Type:     "string",
					JSONPath: ".status.duration",
				},
				{
					Name:     "Running",
					Type:     "boolean",
					JSONPath: ".status.running",
				},
				{
					Name:     "Age",
					Type:     "date",
					JSONPath: ".metadata.creationTimestamp",
				},
			},
			Versions: []apiextensionsv1beta1.CustomResourceDefinitionVersion{
				{
					Name: "v1",
				},
				{
					Name:    "v2",
					Served:  true,
					Storage: true,
				},
			},
			Validation: &apiextensionsv1beta1.CustomResourceValidation{
				OpenAPIV3Schema: &apiextensionsv1beta1.JSONSchemaProps{
					Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
						"spec": apiextensionsv1beta1.JSONSchemaProps{
							Type: "object",
							Required: []string{
								"backup",
								"start",
							},
							Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
								"backup": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"repo": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"start": apiextensionsv1beta1.JSONSchemaProps{
									Type: "object",
									Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
										"int": apiextensionsv1beta1.JSONSchemaProps{
											Type:    "integer",
											Minimum: &minimumBackupIndexSize,
										},
										"str": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
										},
									},
								},
								"end": apiextensionsv1beta1.JSONSchemaProps{
									Type: "object",
									Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
										"int": apiextensionsv1beta1.JSONSchemaProps{
											Type:    "integer",
											Minimum: &minimumBackupIndexSize,
										},
										"str": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
										},
									},
								},
								"logRetention": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "string",
									Pattern: `^\d+(ns|us|ms|s|m|h)$`,
								},
							},
						},
						"status": apiextensionsv1beta1.JSONSchemaProps{
							Type: "object",
							Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
								"archive": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"repo": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"repoList": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"running": apiextensionsv1beta1.JSONSchemaProps{
									Type: "boolean",
								},
								"failed": apiextensionsv1beta1.JSONSchemaProps{
									Type: "boolean",
								},
								"output": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"pod": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"job": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"completed": apiextensionsv1beta1.JSONSchemaProps{
									Type: "boolean",
								},
								"duration": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"lastFailure": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"lastSuccess": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"lastRun": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
							},
						},
					},
				},
			},
		},
	}
}

func GetCouchbaseClusterCRD() *apiextensionsv1beta1.CustomResourceDefinition {
	return &apiextensionsv1beta1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiextensionsv1beta1.SchemeGroupVersion.String(),
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: couchbasev2.ClusterCRDName,
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group: couchbasev2.SchemeGroupVersion.Group,
			Scope: apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural:     couchbasev2.ClusterCRDResourcePlural,
				Kind:       couchbasev2.ClusterCRDResourceKind,
				ShortNames: []string{"cbc"},
				Categories: categories,
			},
			// Not supported by GKE 1.13 for some odd reason
			//Subresources: &apiextensionsv1beta1.CustomResourceSubresources{
			//	Status: &apiextensionsv1beta1.CustomResourceSubresourceStatus{},
			//},
			AdditionalPrinterColumns: []apiextensionsv1beta1.CustomResourceColumnDefinition{
				{
					Name:        "Version",
					Type:        "string",
					Description: "Couchbase version",
					JSONPath:    ".status.currentVersion",
				},
				{
					Name:        "Size",
					Type:        "string",
					Description: "Cluster size",
					JSONPath:    ".status.size",
				},
				{
					Name:        "Status",
					Type:        "string",
					Description: "Cluster status",
					JSONPath:    ".status.phase",
				},
				{
					Name:        "UUID",
					Type:        "string",
					Description: "Cluster UUID",
					JSONPath:    ".status.clusterId",
				},
				{
					Name:     "Age",
					Type:     "date",
					JSONPath: ".metadata.creationTimestamp",
				},
			},
			Versions: []apiextensionsv1beta1.CustomResourceDefinitionVersion{
				{
					Name:    "v2",
					Served:  true,
					Storage: true,
				},
				{
					Name:   "v1",
					Served: true,
				},
			},
			Validation: &apiextensionsv1beta1.CustomResourceValidation{
				OpenAPIV3Schema: &apiextensionsv1beta1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
						"spec": apiextensionsv1beta1.JSONSchemaProps{
							Type: "object",
							Required: []string{
								"image",
								"security",
								"servers",
							},
							Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
								"image": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "string",
									Pattern: ImagePattern,
								},
								"paused": apiextensionsv1beta1.JSONSchemaProps{
									Type: "boolean",
								},
								"antiAffinity": apiextensionsv1beta1.JSONSchemaProps{
									Type: "boolean",
								},
								"security": apiextensionsv1beta1.JSONSchemaProps{
									Type: "object",
									Required: []string{
										"adminSecret",
									},
									Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
										"adminSecret": apiextensionsv1beta1.JSONSchemaProps{
											Type:      "string",
											MinLength: &minimumStringLength,
										},
										"rbac": apiextensionsv1beta1.JSONSchemaProps{
											Type: "object",
											Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
												"managed": apiextensionsv1beta1.JSONSchemaProps{
													Type: "boolean",
												},
												"selector": apiextensionsv1beta1.JSONSchemaProps{
													Type: "object",
												},
											},
										},
										"ldap": apiextensionsv1beta1.JSONSchemaProps{
											Type: "object",
											Required: []string{
												"hosts",
											},
											Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
												"bindSecret": apiextensionsv1beta1.JSONSchemaProps{
													Type: "string",
												},
												"tlsSecret": apiextensionsv1beta1.JSONSchemaProps{
													Type: "string",
												},
												"authenticationEnabled": apiextensionsv1beta1.JSONSchemaProps{
													Type: "boolean",
												},
												"authorizationEnabled": apiextensionsv1beta1.JSONSchemaProps{
													Type: "boolean",
												},
												"hosts": apiextensionsv1beta1.JSONSchemaProps{
													Type: "array",
													Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
														Schema: &apiextensionsv1beta1.JSONSchemaProps{
															Type: "string",
														},
													},
												},
												"port": apiextensionsv1beta1.JSONSchemaProps{
													Type: "integer",
												},
												"encryption": apiextensionsv1beta1.JSONSchemaProps{
													Type:    "string",
													Pattern: "^None|StartTLSExtension|TLS$",
												},
												"serverCertValidation": apiextensionsv1beta1.JSONSchemaProps{
													Type: "boolean",
												},
												"groupsQuery": apiextensionsv1beta1.JSONSchemaProps{
													Type: "string",
												},
												"bindDN": apiextensionsv1beta1.JSONSchemaProps{
													Type: "string",
												},
												"userDNMapping": apiextensionsv1beta1.JSONSchemaProps{
													Type: "object",
													Required: []string{
														"template",
													},
													Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
														"template": apiextensionsv1beta1.JSONSchemaProps{
															Type: "string",
														},
													},
												},
												"nestedGroupsEnabled": apiextensionsv1beta1.JSONSchemaProps{
													Type: "boolean",
												},
												"nestedGroupsMaxDepth": apiextensionsv1beta1.JSONSchemaProps{
													Type: "integer",
												},
												"cacheValueLifetime": apiextensionsv1beta1.JSONSchemaProps{
													Type: "integer",
												},
											},
										},
									},
								},
								"securityContext": apiextensionsv1beta1.JSONSchemaProps{
									Type: "object",
								},
								"networking": apiextensionsv1beta1.JSONSchemaProps{
									Type: "object",
									Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
										"tls": apiextensionsv1beta1.JSONSchemaProps{
											Type: "object",
											Required: []string{
												"static",
											},
											Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
												"static": apiextensionsv1beta1.JSONSchemaProps{
													Type: "object",
													Required: []string{
														"serverSecret",
														"operatorSecret",
													},
													Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
														"serverSecret": apiextensionsv1beta1.JSONSchemaProps{
															Type: "string",
														},
														"operatorSecret": apiextensionsv1beta1.JSONSchemaProps{
															Type: "string",
														},
													},
												},
												"clientCertificatePolicy": apiextensionsv1beta1.JSONSchemaProps{
													Type:    "string",
													Pattern: "^enable|mandatory$",
												},
												"clientCertificatePaths": apiextensionsv1beta1.JSONSchemaProps{
													Type: "array",
													Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
														Schema: &apiextensionsv1beta1.JSONSchemaProps{
															Type: "object",
															Required: []string{
																"path",
															},
															Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
																"path": apiextensionsv1beta1.JSONSchemaProps{
																	Type:    "string",
																	Pattern: `^subject\.cn|san\.uri|san\.dnsname|san\.email$`,
																},
																"prefix": apiextensionsv1beta1.JSONSchemaProps{
																	Type: "string",
																},
																"delimiter": apiextensionsv1beta1.JSONSchemaProps{
																	Type: "string",
																},
															},
														},
													},
												},
												"nodeToNodeEncryption": apiextensionsv1beta1.JSONSchemaProps{
													Type:    "string",
													Pattern: "^(All|ControlPlaneOnly)$",
												},
											},
										},
										"exposeAdminConsole": apiextensionsv1beta1.JSONSchemaProps{
											Type: "boolean",
										},
										"adminConsoleServices": apiextensionsv1beta1.JSONSchemaProps{
											Type: "array",
											Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
												Schema: &apiextensionsv1beta1.JSONSchemaProps{
													Type:    "string",
													Pattern: "^data|index|query|search|eventing|analytics$",
												},
											},
										},
										"exposedFeatures": apiextensionsv1beta1.JSONSchemaProps{
											Type: "array",
											Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
												Schema: &apiextensionsv1beta1.JSONSchemaProps{
													Type:    "string",
													Pattern: "^admin|xdcr|client$",
												},
											},
										},
										"exposedFeatureServiceType": apiextensionsv1beta1.JSONSchemaProps{
											Type:    "string",
											Pattern: "^NodePort|LoadBalancer$",
										},
										"exposedFeatureTrafficPolicy": apiextensionsv1beta1.JSONSchemaProps{
											Type:    "string",
											Pattern: "^Cluster|Local$",
										},
										"adminConsoleServiceType": apiextensionsv1beta1.JSONSchemaProps{
											Type:    "string",
											Pattern: "^NodePort|LoadBalancer$",
										},
										"dns": apiextensionsv1beta1.JSONSchemaProps{
											Type: "object",
											Required: []string{
												"domain",
											},
											Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
												"domain": apiextensionsv1beta1.JSONSchemaProps{
													Type: "string",
												},
											},
										},
										"serviceAnnotations": apiextensionsv1beta1.JSONSchemaProps{
											Type: "object",
										},
										"loadBalancerSourceRanges": apiextensionsv1beta1.JSONSchemaProps{
											Type: "array",
											Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
												Schema: &apiextensionsv1beta1.JSONSchemaProps{
													Type:    "string",
													Pattern: `^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}/\d{1,2}$`,
												},
											},
										},
									},
								},
								"logging": apiextensionsv1beta1.JSONSchemaProps{
									Type: "object",
									Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
										"logRetentionTime": apiextensionsv1beta1.JSONSchemaProps{
											Type:    "string",
											Pattern: `^\d+(ns|us|ms|s|m|h)$`,
										},
										"logRetentionCount": apiextensionsv1beta1.JSONSchemaProps{
											Type:    "integer",
											Minimum: &minimumLogRetentionCount,
										},
									},
								},
								"buckets": apiextensionsv1beta1.JSONSchemaProps{
									Type: "object",
									Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
										"managed": apiextensionsv1beta1.JSONSchemaProps{
											Type: "boolean",
										},
										"selector": apiextensionsv1beta1.JSONSchemaProps{
											Type: "object",
										},
									},
								},
								"xdcr": apiextensionsv1beta1.JSONSchemaProps{
									Type: "object",
									Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
										"managed": apiextensionsv1beta1.JSONSchemaProps{
											Type: "boolean",
										},
										"remoteClusters": apiextensionsv1beta1.JSONSchemaProps{
											Type: "array",
											Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
												Schema: &apiextensionsv1beta1.JSONSchemaProps{
													Type: "object",
													Required: []string{
														"name",
														"uuid",
														"hostname",
													},
													Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
														"name": apiextensionsv1beta1.JSONSchemaProps{
															Type: "string",
														},
														"uuid": apiextensionsv1beta1.JSONSchemaProps{
															Type:    "string",
															Pattern: `^[0-9a-f]{32}$`,
														},
														"hostname": apiextensionsv1beta1.JSONSchemaProps{
															Type:    "string",
															Pattern: `^[0-9a-zA-Z\-\.]+(:\d+)?$`, // good enough :D
														},
														"authenticationSecret": apiextensionsv1beta1.JSONSchemaProps{
															Type: "string",
														},
														"replications": apiextensionsv1beta1.JSONSchemaProps{
															Type: "object",
															Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
																"selector": apiextensionsv1beta1.JSONSchemaProps{
																	Type: "object",
																},
															},
														},
														"tls": apiextensionsv1beta1.JSONSchemaProps{
															Type: "object",
															Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
																"secret": apiextensionsv1beta1.JSONSchemaProps{
																	Type: "string",
																},
															},
														},
													},
												},
											},
										},
									},
								},
								"softwareUpdateNotifications": apiextensionsv1beta1.JSONSchemaProps{
									Type: "boolean",
								},
								"serverGroups": apiextensionsv1beta1.JSONSchemaProps{
									Type: "array",
									Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
										Schema: &apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
										},
									},
								},
								"platform": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "string",
									Pattern: "^aws|gce|azure$",
								},
								"cluster": apiextensionsv1beta1.JSONSchemaProps{
									Type: "object",
									Required: []string{
										"dataServiceMemoryQuota",
										"indexServiceMemoryQuota",
										"searchServiceMemoryQuota",
										"eventingServiceMemoryQuota",
										"analyticsServiceMemoryQuota",
										"indexStorageSetting",
										"autoFailoverTimeout",
										"autoFailoverMaxCount",
									},
									Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
										"clusterName": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
										},
										"dataServiceMemoryQuota": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
										},
										"indexServiceMemoryQuota": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
										},
										"searchServiceMemoryQuota": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
										},
										"eventingServiceMemoryQuota": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
										},
										"analyticsServiceMemoryQuota": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
										},
										"indexStorageSetting": apiextensionsv1beta1.JSONSchemaProps{
											Type:    "string",
											Pattern: "^plasma|memory_optimized$",
										},
										"autoFailoverTimeout": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
										},
										"autoFailoverMaxCount": apiextensionsv1beta1.JSONSchemaProps{
											Type:    "integer",
											Minimum: &minimumAutofailoverMaxCount,
											Maximum: &maximumAutofailoverMaxCount,
										},
										"autoFailoverOnDataDiskIssues": apiextensionsv1beta1.JSONSchemaProps{
											Type: "boolean",
										},
										"autoFailoverOnDataDiskIssuesTimePeriod": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
										},
										"autoFailoverServerGroup": apiextensionsv1beta1.JSONSchemaProps{
											Type: "boolean",
										},
										"autoCompaction": apiextensionsv1beta1.JSONSchemaProps{
											Type: "object",
											Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
												"databaseFragmentationThreshold": apiextensionsv1beta1.JSONSchemaProps{
													Type: "object",
													Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
														"percent": apiextensionsv1beta1.JSONSchemaProps{
															Type:    "integer",
															Minimum: &minimumAutoCompactionPercent,
															Maximum: &maximumAutoCompactionPercent,
														},
														"size": apiextensionsv1beta1.JSONSchemaProps{
															Type: "string",
														},
													},
												},
												"viewFragmentationThreshold": apiextensionsv1beta1.JSONSchemaProps{
													Type: "object",
													Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
														"percent": apiextensionsv1beta1.JSONSchemaProps{
															Type:    "integer",
															Minimum: &minimumAutoCompactionPercent,
															Maximum: &maximumAutoCompactionPercent,
														},
														"size": apiextensionsv1beta1.JSONSchemaProps{
															Type: "string",
														},
													},
												},
												"parallelCompaction": apiextensionsv1beta1.JSONSchemaProps{
													Type: "boolean",
												},
												"timeWindow": apiextensionsv1beta1.JSONSchemaProps{
													Type: "object",
													Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
														"start": apiextensionsv1beta1.JSONSchemaProps{
															Type:    "string",
															Pattern: wallClockTime,
														},
														"end": apiextensionsv1beta1.JSONSchemaProps{
															Type:    "string",
															Pattern: wallClockTime,
														},
														"abortCompactionOutsideWindow": apiextensionsv1beta1.JSONSchemaProps{
															Type: "boolean",
														},
													},
												},
												"tombstonePurgeInterval": apiextensionsv1beta1.JSONSchemaProps{
													Type: "string",
												},
											},
										},
									},
								},
								"servers": apiextensionsv1beta1.JSONSchemaProps{
									Type:      "array",
									MinLength: &minimumServersLength,
									Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
										Schema: &apiextensionsv1beta1.JSONSchemaProps{
											Type: "object",
											Required: []string{
												"size",
												"name",
												"services",
											},
											Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
												"size": apiextensionsv1beta1.JSONSchemaProps{
													Type:    "integer",
													Minimum: &minimumServersSize,
												},
												"name": apiextensionsv1beta1.JSONSchemaProps{
													Type:      "string",
													Pattern:   `^[-_a-zA-Z0-9]+$`,
													MinLength: &minimumStringLength,
												},
												"services": apiextensionsv1beta1.JSONSchemaProps{
													Type: "array",
													Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
														Schema: &apiextensionsv1beta1.JSONSchemaProps{
															Type:    "string",
															Pattern: "^data|index|query|search|eventing|analytics$",
														},
													},
													MinLength: &minimumServicesLength,
												},
												"serverGroups": apiextensionsv1beta1.JSONSchemaProps{
													Type: "array",
													Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
														Schema: &apiextensionsv1beta1.JSONSchemaProps{
															Type: "string",
														},
													},
												},
												"env": apiextensionsv1beta1.JSONSchemaProps{
													Type: "array",
													Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
														Schema: &apiextensionsv1beta1.JSONSchemaProps{
															Type: "object",
															Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
																"name": apiextensionsv1beta1.JSONSchemaProps{
																	Type: "string",
																},
																"value": apiextensionsv1beta1.JSONSchemaProps{
																	Type: "string",
																},
															},
														},
													},
												},
												"envFrom": apiextensionsv1beta1.JSONSchemaProps{
													Type: "object",
												},
												"resources": apiextensionsv1beta1.JSONSchemaProps{
													Type: "object",
													Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
														"limits": apiextensionsv1beta1.JSONSchemaProps{
															Type: "object",
															Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
																"cpu": apiextensionsv1beta1.JSONSchemaProps{
																	Type: "string",
																},
																"memory": apiextensionsv1beta1.JSONSchemaProps{
																	Type: "string",
																},
															},
														},
														"requests": apiextensionsv1beta1.JSONSchemaProps{
															Type: "object",
															Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
																"cpu": apiextensionsv1beta1.JSONSchemaProps{
																	Type: "string",
																},
																"memory": apiextensionsv1beta1.JSONSchemaProps{
																	Type: "string",
																},
															},
														},
													},
												},
												"volumeMounts": apiextensionsv1beta1.JSONSchemaProps{
													Type: "object",
													Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
														"default": apiextensionsv1beta1.JSONSchemaProps{
															Type: "string",
														},
														"data": apiextensionsv1beta1.JSONSchemaProps{
															Type: "string",
														},
														"index": apiextensionsv1beta1.JSONSchemaProps{
															Type: "string",
														},
														"analytics": apiextensionsv1beta1.JSONSchemaProps{
															Type: "array",
															Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
																Schema: &apiextensionsv1beta1.JSONSchemaProps{
																	Type: "string",
																},
															},
														},
														"logs": apiextensionsv1beta1.JSONSchemaProps{
															Type: "string",
														},
													},
												},
												"pod": apiextensionsv1beta1.JSONSchemaProps{
													Type: "object",
													Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
														"metadata": apiextensionsv1beta1.JSONSchemaProps{
															Type: "object",
															Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
																"labels": apiextensionsv1beta1.JSONSchemaProps{
																	Type: "object",
																},
																"annotations": apiextensionsv1beta1.JSONSchemaProps{
																	Type: "object",
																},
															},
														},
														"spec": apiextensionsv1beta1.JSONSchemaProps{
															Type: "object",
															Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
																"imagePullSecrets": apiextensionsv1beta1.JSONSchemaProps{
																	Type: "array",
																	Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
																		Schema: &apiextensionsv1beta1.JSONSchemaProps{
																			Type: "object",
																		},
																	},
																},
																"nodeSelector": apiextensionsv1beta1.JSONSchemaProps{
																	Type: "object",
																},
																"tolerations": apiextensionsv1beta1.JSONSchemaProps{
																	Type: "array",
																	Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
																		Schema: &apiextensionsv1beta1.JSONSchemaProps{
																			Type: "object",
																			Required: []string{
																				"key",
																				"operator",
																				"value",
																				"effect",
																			},
																			Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
																				"key": apiextensionsv1beta1.JSONSchemaProps{
																					Type: "string",
																				},
																				"operator": apiextensionsv1beta1.JSONSchemaProps{
																					Type: "string",
																				},
																				"value": apiextensionsv1beta1.JSONSchemaProps{
																					Type: "string",
																				},
																				"effect": apiextensionsv1beta1.JSONSchemaProps{
																					Type: "string",
																				},
																				"tolerationSeconds": apiextensionsv1beta1.JSONSchemaProps{
																					Type: "integer",
																				},
																			},
																		},
																	},
																},
																"automountServiceAccountToken": apiextensionsv1beta1.JSONSchemaProps{
																	Type: "boolean",
																},
																"dnsPolicy": apiextensionsv1beta1.JSONSchemaProps{
																	Type:    "string",
																	Pattern: `^None$`,
																},
																"dnsConfig": apiextensionsv1beta1.JSONSchemaProps{
																	Type: "object",
																},
															},
														},
													},
												},
											},
										},
									},
								},
								"volumeClaimTemplates": apiextensionsv1beta1.JSONSchemaProps{
									Type: "array",
									Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
										Schema: &apiextensionsv1beta1.JSONSchemaProps{
											Type: "object",
											Required: []string{
												"metadata",
												"spec",
											},
											Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
												"metadata": apiextensionsv1beta1.JSONSchemaProps{
													Type: "object",
													Required: []string{
														"name",
													},
													Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
														"name": apiextensionsv1beta1.JSONSchemaProps{
															Type: "string",
														},
													},
												},
												"spec": apiextensionsv1beta1.JSONSchemaProps{
													Type: "object",
													Required: []string{
														"resources",
													},
													Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
														"storageClassName": apiextensionsv1beta1.JSONSchemaProps{
															Type: "string",
														},
														"resources": apiextensionsv1beta1.JSONSchemaProps{
															Type: "object",
															Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
																"requests": apiextensionsv1beta1.JSONSchemaProps{
																	Type: "object",
																	Required: []string{
																		"storage",
																	},
																	Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
																		"storage": apiextensionsv1beta1.JSONSchemaProps{
																			Type: "string",
																		},
																	},
																},
																"limits": apiextensionsv1beta1.JSONSchemaProps{
																	Type: "object",
																	Required: []string{
																		"storage",
																	},
																	Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
																		"storage": apiextensionsv1beta1.JSONSchemaProps{
																			Type: "string",
																		},
																	},
																},
															},
														},
													},
												},
											},
										},
									},
								},
								"backup": apiextensionsv1beta1.JSONSchemaProps{
									Type: "object",
									Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
										"managed": apiextensionsv1beta1.JSONSchemaProps{
											Type: "boolean",
										},
										"image": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
										},
										"serviceAccountName": apiextensionsv1beta1.JSONSchemaProps{
											Type:      "string",
											MinLength: &minimumItemLength,
										},
										"resources": apiextensionsv1beta1.JSONSchemaProps{
											Type: "object",
											Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
												"limits": apiextensionsv1beta1.JSONSchemaProps{
													Type: "object",
													Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
														"cpu": apiextensionsv1beta1.JSONSchemaProps{
															Type: "string",
														},
														"memory": apiextensionsv1beta1.JSONSchemaProps{
															Type: "string",
														},
													},
												},
												"requests": apiextensionsv1beta1.JSONSchemaProps{
													Type: "object",
													Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
														"cpu": apiextensionsv1beta1.JSONSchemaProps{
															Type: "string",
														},
														"memory": apiextensionsv1beta1.JSONSchemaProps{
															Type: "string",
														},
													},
												},
											},
										},
									},
								},
								"monitoring": apiextensionsv1beta1.JSONSchemaProps{
									Type: "object",
									Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
										"prometheus": apiextensionsv1beta1.JSONSchemaProps{
											Type: "object",
											Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
												"enabled": apiextensionsv1beta1.JSONSchemaProps{
													Type: "boolean",
												},
												"image": apiextensionsv1beta1.JSONSchemaProps{
													Type:    "string",
													Pattern: ImagePattern,
												},
												"resources": apiextensionsv1beta1.JSONSchemaProps{
													Type: "object",
													Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
														"limits": apiextensionsv1beta1.JSONSchemaProps{
															Type: "object",
															Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
																"cpu": apiextensionsv1beta1.JSONSchemaProps{
																	Type: "string",
																},
																"memory": apiextensionsv1beta1.JSONSchemaProps{
																	Type: "string",
																},
															},
														},
														"requests": apiextensionsv1beta1.JSONSchemaProps{
															Type: "object",
															Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
																"cpu": apiextensionsv1beta1.JSONSchemaProps{
																	Type: "string",
																},
																"memory": apiextensionsv1beta1.JSONSchemaProps{
																	Type: "string",
																},
															},
														},
													},
												},
												"authorizationSecret": apiextensionsv1beta1.JSONSchemaProps{
													Type: "string",
												},
											},
										},
									},
								},
							},
						},
						"status": apiextensionsv1beta1.JSONSchemaProps{
							Type: "object",
							Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
								"phase": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"reason": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"controlPaused": apiextensionsv1beta1.JSONSchemaProps{
									Type: "boolean",
								},
								"conditions": apiextensionsv1beta1.JSONSchemaProps{
									Type: "array",
									Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
										Schema: &apiextensionsv1beta1.JSONSchemaProps{
											Type: "object",
											Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
												"type": apiextensionsv1beta1.JSONSchemaProps{
													Type: "string",
												},
												"status": apiextensionsv1beta1.JSONSchemaProps{
													Type: "string",
												},
												"lastUpdateTime": apiextensionsv1beta1.JSONSchemaProps{
													Type: "string",
												},
												"lastTransitionTime": apiextensionsv1beta1.JSONSchemaProps{
													Type: "string",
												},
												"reason": apiextensionsv1beta1.JSONSchemaProps{
													Type: "string",
												},
												"message": apiextensionsv1beta1.JSONSchemaProps{
													Type: "string",
												},
											},
										},
									},
								},
								"size": apiextensionsv1beta1.JSONSchemaProps{
									Type: "integer",
								},
								"members": apiextensionsv1beta1.JSONSchemaProps{
									Type: "object",
									Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
										"ready": apiextensionsv1beta1.JSONSchemaProps{
											Type: "array",
											Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
												Schema: &apiextensionsv1beta1.JSONSchemaProps{
													Type: "string",
												},
											},
										},
										"unready": apiextensionsv1beta1.JSONSchemaProps{
											Type: "array",
											Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
												Schema: &apiextensionsv1beta1.JSONSchemaProps{
													Type: "string",
												},
											},
										},
									},
								},
								"currentVersion": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"adminConsolePort": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"adminConsolePortSSL": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"exposedFeatures": apiextensionsv1beta1.JSONSchemaProps{
									Type: "array",
									Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
										Schema: &apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func GetUserCRD() *apiextensionsv1beta1.CustomResourceDefinition {
	return &apiextensionsv1beta1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiextensionsv1beta1.SchemeGroupVersion.String(),
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: couchbasev2.UserCRDName,
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group: couchbasev2.SchemeGroupVersion.Group,
			Scope: apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural:     couchbasev2.UserCRDResourcePlural,
				Kind:       couchbasev2.UserCRDResourceKind,
				Categories: categories,
			},
			Versions: []apiextensionsv1beta1.CustomResourceDefinitionVersion{
				{
					Name: "v1",
				},
				{
					Name:    "v2",
					Served:  true,
					Storage: true,
				},
			},
			Validation: &apiextensionsv1beta1.CustomResourceValidation{
				OpenAPIV3Schema: &apiextensionsv1beta1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
						"spec": apiextensionsv1beta1.JSONSchemaProps{
							Type: "object",
							Required: []string{
								"authDomain",
							},
							Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
								"fullName": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"authDomain": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "string",
									Pattern: "^local|external$",
								},
								"authSecret": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
							},
						},
					},
				},
			},
		},
	}
}

func GetGroupCRD() *apiextensionsv1beta1.CustomResourceDefinition {
	return &apiextensionsv1beta1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiextensionsv1beta1.SchemeGroupVersion.String(),
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: couchbasev2.GroupCRDName,
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group: couchbasev2.SchemeGroupVersion.Group,
			Scope: apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural:     couchbasev2.GroupCRDResourcePlural,
				Kind:       couchbasev2.GroupCRDResourceKind,
				Categories: categories,
			},
			Versions: []apiextensionsv1beta1.CustomResourceDefinitionVersion{
				{
					Name: "v1",
				},
				{
					Name:    "v2",
					Served:  true,
					Storage: true,
				},
			},
			Validation: &apiextensionsv1beta1.CustomResourceValidation{
				OpenAPIV3Schema: &apiextensionsv1beta1.JSONSchemaProps{
					Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
						"spec": apiextensionsv1beta1.JSONSchemaProps{
							Type: "object",
							Required: []string{
								"roles",
							},
							Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
								"ldapGroupRef": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"roles": apiextensionsv1beta1.JSONSchemaProps{
									Type:      "array",
									MinLength: &minimumItemLength,
									Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
										Schema: &apiextensionsv1beta1.JSONSchemaProps{
											Type: "object",
											Required: []string{
												"name",
											},
											Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
												"name": apiextensionsv1beta1.JSONSchemaProps{
													Type:      "string",
													MinLength: &minimumStringLength,
													Pattern:   couchbasev2.ValidRolePattern(),
												},
												"bucket": apiextensionsv1beta1.JSONSchemaProps{
													Type:      "string",
													Pattern:   BucketPattern,
													MaxLength: &maximumBucketNameLength,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func GetRoleBindingCRD() *apiextensionsv1beta1.CustomResourceDefinition {
	return &apiextensionsv1beta1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiextensionsv1beta1.SchemeGroupVersion.String(),
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: couchbasev2.RoleBindingCRDName,
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group: couchbasev2.SchemeGroupVersion.Group,
			Scope: apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural:     couchbasev2.RoleBindingCRDResourcePlural,
				Kind:       couchbasev2.RoleBindingCRDResourceKind,
				Categories: categories,
			},
			Versions: []apiextensionsv1beta1.CustomResourceDefinitionVersion{
				{
					Name: "v1",
				},
				{
					Name:    "v2",
					Served:  true,
					Storage: true,
				},
			},
			Validation: &apiextensionsv1beta1.CustomResourceValidation{
				OpenAPIV3Schema: &apiextensionsv1beta1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
						"spec": apiextensionsv1beta1.JSONSchemaProps{
							Type: "object",
							Required: []string{
								"subjects",
								"roleRef",
							},
							Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
								"subjects": apiextensionsv1beta1.JSONSchemaProps{
									Type:      "array",
									MinLength: &minimumItemLength,
									Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
										Schema: &apiextensionsv1beta1.JSONSchemaProps{
											Type: "object",
											Required: []string{
												"name",
												"kind",
											},
											Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
												"name": apiextensionsv1beta1.JSONSchemaProps{
													Type:      "string",
													MinLength: &minimumStringLength,
												},
												"kind": apiextensionsv1beta1.JSONSchemaProps{
													Type:    "string",
													Pattern: "^CouchbaseUser$",
												},
											},
										},
									},
								},
								"roleRef": apiextensionsv1beta1.JSONSchemaProps{
									Type: "object",
									Required: []string{
										"name",
									},
									Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
										"name": apiextensionsv1beta1.JSONSchemaProps{
											Type:      "string",
											MinLength: &minimumStringLength,
										},
										"kind": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}
