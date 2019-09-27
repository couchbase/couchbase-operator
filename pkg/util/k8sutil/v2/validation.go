package v2

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"

	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	maximumAutofailoverTimeout         float64 = 3600
	minimumServiceMemoryQuota          float64 = 256
	minimumAnalyticsServiceMemoryQuota float64 = 1024
	minimumBucketQuota                 float64 = 100
	minimumAutofailoverTimeout         float64 = 5
	minimumServersSize                 float64 = 1
	minimumItemLength                  int64   = 1
	minimumServicesLength              int64   = 1
	minimumServersLength               int64   = 1
	minimumStringLength                int64   = 1
	minimumBucketReplicas              float64 = 0
	maximumBucketReplicas              float64 = 3
	minimumAutofailoverMaxCount        float64 = 1
	maximumAutofailoverMaxCount        float64 = 3
	minimumLogRetentionCount           float64 = 0
	minimumAutoCompactionPercent       float64 = 2
	maximumAutoCompactionPercent       float64 = 100
)

const (
	ImagePattern  = `^[\w_\-/]+:([\w\d]+-)?\d+\.\d+.\d+(-[\w\d]+)?$`
	wallClockTime = `^(2[0-3]|[01]?[0-9]):([0-5]?[0-9])$`
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
				Plural: couchbasev2.BucketCRDResourcePlural,
				Kind:   couchbasev2.BucketCRDResourceKind,
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
								"memoryQuota",
								"ioPriority",
								"evictionPolicy",
								"conflictResolution",
								"compressionMode",
							},
							Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
								"memoryQuota": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "integer",
									Minimum: &minimumBucketQuota,
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
				Plural: couchbasev2.EphemeralBucketCRDResourcePlural,
				Kind:   couchbasev2.EphemeralBucketCRDResourceKind,
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
								"memoryQuota",
								"ioPriority",
								"evictionPolicy",
								"conflictResolution",
								"compressionMode",
							},
							Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
								"memoryQuota": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "integer",
									Minimum: &minimumBucketQuota,
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
				Plural: couchbasev2.MemcachedBucketCRDResourcePlural,
				Kind:   couchbasev2.MemcachedBucketCRDResourceKind,
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
								"memoryQuota",
							},
							Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
								"memoryQuota": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "integer",
									Minimum: &minimumBucketQuota,
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
				Plural: couchbasev2.ReplicationCRDResourcePlural,
				Kind:   couchbasev2.ReplicationCRDResourceKind,
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
									Pattern: "^none|auto|snappy$",
								},
								"filterExpression": apiextensionsv1beta1.JSONSchemaProps{
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

func GetCouchbaseClusterSchema() *apiextensionsv1beta1.CustomResourceValidation {
	return &apiextensionsv1beta1.CustomResourceValidation{
		OpenAPIV3Schema: &apiextensionsv1beta1.JSONSchemaProps{
			Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
				"spec": apiextensionsv1beta1.JSONSchemaProps{
					Type: "object",
					Required: []string{
						"image",
						"security",
						"cluster",
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
							},
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
												"member",
												"operatorSecret",
											},
											Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
												"member": apiextensionsv1beta1.JSONSchemaProps{
													Type: "object",
													Required: []string{
														"serverSecret",
													},
													Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
														"serverSecret": apiextensionsv1beta1.JSONSchemaProps{
															Type: "string",
														},
													},
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
									Type:    "integer",
									Minimum: &minimumServiceMemoryQuota,
								},
								"indexServiceMemoryQuota": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "integer",
									Minimum: &minimumServiceMemoryQuota,
								},
								"searchServiceMemoryQuota": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "integer",
									Minimum: &minimumServiceMemoryQuota,
								},
								"eventingServiceMemoryQuota": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "integer",
									Minimum: &minimumServiceMemoryQuota,
								},
								"analyticsServiceMemoryQuota": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "integer",
									Minimum: &minimumAnalyticsServiceMemoryQuota,
								},
								"indexStorageSetting": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "string",
									Pattern: "^plasma|memory_optimized$",
								},
								"autoFailoverTimeout": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "integer",
									Minimum: &minimumAutofailoverTimeout,
									Maximum: &maximumAutofailoverTimeout,
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
									Type:    "integer",
									Minimum: &minimumAutofailoverTimeout,
									Maximum: &maximumAutofailoverTimeout,
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
										"pod": apiextensionsv1beta1.JSONSchemaProps{
											Type: "object",
											Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
												"couchbaseEnv": apiextensionsv1beta1.JSONSchemaProps{
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
																"storage": apiextensionsv1beta1.JSONSchemaProps{
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
																"storage": apiextensionsv1beta1.JSONSchemaProps{
																	Type: "string",
																},
															},
														},
													},
												},
												"labels": apiextensionsv1beta1.JSONSchemaProps{
													Type: "object",
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
												"storageClassName",
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
			Name:   couchbasev2.UserCRDName,
			Labels: map[string]string{"group": "couchbase.com"},
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group: couchbasev2.SchemeGroupVersion.Group,
			Scope: apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural: couchbasev2.UserCRDResourcePlural,
				Kind:   couchbasev2.UserCRDResourceKind,
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
								"authDomain",
							},
							Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
								"fullName": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"authDomain": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "string",
									Pattern: "^local|ldap$",
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

func GetRoleCRD() *apiextensionsv1beta1.CustomResourceDefinition {
	return &apiextensionsv1beta1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiextensionsv1beta1.SchemeGroupVersion.String(),
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   couchbasev2.RoleCRDName,
			Labels: map[string]string{"group": "couchbase.com"},
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group: couchbasev2.SchemeGroupVersion.Group,
			Scope: apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural: couchbasev2.RoleCRDResourcePlural,
				Kind:   couchbasev2.RoleCRDResourceKind,
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
													MinLength: &minimumStringLength,
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
			Name:   couchbasev2.RoleBindingCRDName,
			Labels: map[string]string{"group": "couchbase.com"},
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group: couchbasev2.SchemeGroupVersion.Group,
			Scope: apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural: couchbasev2.RoleBindingCRDResourcePlural,
				Kind:   couchbasev2.RoleBindingCRDResourceKind,
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
													Pattern: "^CouchbaseUser|CouchbaseGroup$",
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
