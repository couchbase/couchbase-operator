package v1

import (
	couchbasev1 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/gocbmgr"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
)

var (
	maximumAutofailoverTimeout         float64 = 3600
	minimumServiceMemoryQuota          float64 = 256
	minimumAnalyticsServiceMemoryQuota float64 = 1024
	minimumBucketQuota                 float64 = 100
	minimumAutofailoverTimeout         float64 = 5
	minimumServersSize                 float64 = 1
	minimumServicesLength              int64   = 1
	minimumServersLength               int64   = 1
	minimumStringLength                int64   = 1
	minimumBucketReplicas              float64 = 0
	maximumBucketReplicas              float64 = 3
	minimumAutofailoverMaxCount        float64 = 1
	maximumAutofailoverMaxCount        float64 = 3
	minimumLogRetentionCount           float64 = 0
)

const (
	VersionPattern = `^([\w\d]+-)?\d+\.\d+.\d+(-[\w\d]+)?$`
)

func GetCouchbaseClusterSchema() *apiextensionsv1beta1.CustomResourceValidation {
	return &apiextensionsv1beta1.CustomResourceValidation{
		OpenAPIV3Schema: &apiextensionsv1beta1.JSONSchemaProps{
			Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
				"spec": apiextensionsv1beta1.JSONSchemaProps{
					Type: "object",
					Required: []string{
						"baseImage",
						"version",
						"authSecret",
						"cluster",
						"servers",
					},
					Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
						"baseImage": apiextensionsv1beta1.JSONSchemaProps{
							Type: "string",
						},
						"version": apiextensionsv1beta1.JSONSchemaProps{
							Type:    "string",
							Pattern: VersionPattern,
						},
						"paused": apiextensionsv1beta1.JSONSchemaProps{
							Type: "boolean",
						},
						"antiAffinity": apiextensionsv1beta1.JSONSchemaProps{
							Type: "boolean",
						},
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
							},
						},
						"authSecret": apiextensionsv1beta1.JSONSchemaProps{
							Type:      "string",
							MinLength: &minimumStringLength,
						},
						"exposeAdminConsole": apiextensionsv1beta1.JSONSchemaProps{
							Type: "boolean",
						},
						"adminConsoleServices": apiextensionsv1beta1.JSONSchemaProps{
							Type: "array",
							Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
								Schema: &apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
									Enum: []apiextensionsv1beta1.JSON{
										{Raw: []byte(`"` + string(couchbasev1.DataService) + `"`)},
										{Raw: []byte(`"` + string(couchbasev1.IndexService) + `"`)},
										{Raw: []byte(`"` + string(couchbasev1.QueryService) + `"`)},
										{Raw: []byte(`"` + string(couchbasev1.SearchService) + `"`)},
										{Raw: []byte(`"` + string(couchbasev1.EventingService) + `"`)},
										{Raw: []byte(`"` + string(couchbasev1.AnalyticsService) + `"`)},
									},
								},
							},
						},
						"exposedFeatures": apiextensionsv1beta1.JSONSchemaProps{
							Type: "array",
							Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
								Schema: &apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
									Enum: []apiextensionsv1beta1.JSON{
										{Raw: []byte(`"admin"`)},
										{Raw: []byte(`"xdcr"`)},
										{Raw: []byte(`"client"`)},
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
						"disableBucketManagement": apiextensionsv1beta1.JSONSchemaProps{
							Type: "boolean",
						},
						"logRetentionTime": apiextensionsv1beta1.JSONSchemaProps{
							Type:    "string",
							Pattern: `^\d+(ns|us|ms|s|m|h)$`,
						},
						"logRetentionCount": apiextensionsv1beta1.JSONSchemaProps{
							Type:    "integer",
							Minimum: &minimumLogRetentionCount,
						},
						"exposedFeatureServiceType": apiextensionsv1beta1.JSONSchemaProps{
							Type: "string",
							Enum: []apiextensionsv1beta1.JSON{
								{Raw: []byte(`"` + corev1.ServiceTypeNodePort + `"`)},
								{Raw: []byte(`"` + corev1.ServiceTypeLoadBalancer + `"`)},
							},
						},
						"adminConsoleServiceType": apiextensionsv1beta1.JSONSchemaProps{
							Type: "string",
							Enum: []apiextensionsv1beta1.JSON{
								{Raw: []byte(`"` + corev1.ServiceTypeNodePort + `"`)},
								{Raw: []byte(`"` + corev1.ServiceTypeLoadBalancer + `"`)},
							},
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
						"platform": apiextensionsv1beta1.JSONSchemaProps{
							Type: "string",
							Enum: []apiextensionsv1beta1.JSON{
								{Raw: []byte(`"` + couchbasev1.PlatformTypeAWS + `"`)},
								{Raw: []byte(`"` + couchbasev1.PlatformTypeGCE + `"`)},
								{Raw: []byte(`"` + couchbasev1.PlatformTypeAzure + `"`)},
							},
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
									Type: "string",
									Enum: []apiextensionsv1beta1.JSON{
										{Raw: []byte(`"plasma"`)},
										{Raw: []byte(`"memory_optimized"`)},
									},
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
							},
						},
						"buckets": apiextensionsv1beta1.JSONSchemaProps{
							Type: "array",
							Items: &apiextensionsv1beta1.JSONSchemaPropsOrArray{
								Schema: &apiextensionsv1beta1.JSONSchemaProps{
									Type: "object",
									Required: []string{
										"name",
										"type",
										"memoryQuota",
									},
									Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
										"name": apiextensionsv1beta1.JSONSchemaProps{
											Type:    "string",
											Pattern: `^[a-zA-Z0-9._\-%]*$`,
										},
										"type": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
											Enum: []apiextensionsv1beta1.JSON{
												{Raw: []byte(`"couchbase"`)},
												{Raw: []byte(`"ephemeral"`)},
												{Raw: []byte(`"memcached"`)},
											},
										},
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
											Type: "string",
											Enum: []apiextensionsv1beta1.JSON{
												{Raw: []byte(`"high"`)},
												{Raw: []byte(`"low"`)},
											},
										},
										"evictionPolicy": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
											Enum: []apiextensionsv1beta1.JSON{
												{Raw: []byte(`"valueOnly"`)},
												{Raw: []byte(`"fullEviction"`)},
												{Raw: []byte(`"noEviction"`)},
												{Raw: []byte(`"nruEviction"`)},
											},
										},
										"conflictResolution": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
											Enum: []apiextensionsv1beta1.JSON{
												{Raw: []byte(`"seqno"`)},
												{Raw: []byte(`"lww"`)},
											},
										},
										"enableFlush": apiextensionsv1beta1.JSONSchemaProps{
											Type: "boolean",
										},
										"enableIndexReplica": apiextensionsv1beta1.JSONSchemaProps{
											Type: "boolean",
										},
										"compressionMode": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
											Enum: []apiextensionsv1beta1.JSON{
												{Raw: []byte(`"` + cbmgr.CompressionModeOff + `"`)},
												{Raw: []byte(`"` + cbmgr.CompressionModePassive + `"`)},
												{Raw: []byte(`"` + cbmgr.CompressionModeActive + `"`)},
											},
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
													Type: "string",
													Enum: []apiextensionsv1beta1.JSON{
														{Raw: []byte(`"` + string(couchbasev1.DataService) + `"`)},
														{Raw: []byte(`"` + string(couchbasev1.IndexService) + `"`)},
														{Raw: []byte(`"` + string(couchbasev1.QueryService) + `"`)},
														{Raw: []byte(`"` + string(couchbasev1.SearchService) + `"`)},
														{Raw: []byte(`"` + string(couchbasev1.EventingService) + `"`)},
														{Raw: []byte(`"` + string(couchbasev1.AnalyticsService) + `"`)},
													},
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
