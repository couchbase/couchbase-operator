package k8sutil

import (
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
)

var (
	maximumAutofailoverTimeout float64 = 3600
	minimumServiceMemoryQuota  float64 = 256
	minimumBucketQuota         float64 = 100
	minimumAutofailoverTimeout float64 = 1
	minimumServersSize         float64 = 1
	minimumServicesLength      int64   = 1
	minimumServersLength       int64   = 1
	minimumStringLength        int64   = 1
	maximumServicesLength      int64   = 4
)

func getCustomResourceValidation() *apiextensionsv1beta1.CustomResourceValidation {
	return &apiextensionsv1beta1.CustomResourceValidation{
		OpenAPIV3Schema: &apiextensionsv1beta1.JSONSchemaProps{
			Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
				"spec": apiextensionsv1beta1.JSONSchemaProps{
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
							Type: "string",
						},
						"paused": apiextensionsv1beta1.JSONSchemaProps{
							Type: "boolean",
						},
						"antiAffinity": apiextensionsv1beta1.JSONSchemaProps{
							Type: "boolean",
						},
						"authSecret": apiextensionsv1beta1.JSONSchemaProps{
							Type:      "string",
							MinLength: &minimumStringLength,
						},
						"exposeAdminConsole": apiextensionsv1beta1.JSONSchemaProps{
							Type: "boolean",
						},
						"adminConsoleServices": apiextensionsv1beta1.JSONSchemaProps{},
						"cluster": apiextensionsv1beta1.JSONSchemaProps{
							Type: "object",
							Required: []string{
								"dataServiceMemoryQuota",
								"indexServiceMemoryQuota",
								"searchServiceMemoryQuota",
								"indexStorageSetting",
								"autoFailoverTimeout",
							},
							Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
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
								"indexStorageSetting": apiextensionsv1beta1.JSONSchemaProps{
									Type: "string",
									Enum: []apiextensionsv1beta1.JSON{
										{
											Raw: []byte(`"plasma"`),
										},
										{
											Raw: []byte(`"memory_optimized"`),
										},
									},
								},
								"autoFailoverTimeout": apiextensionsv1beta1.JSONSchemaProps{
									Type:    "integer",
									Minimum: &minimumAutofailoverTimeout,
									Maximum: &maximumAutofailoverTimeout,
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
												{
													Raw: []byte(`"couchbase"`),
												},
												{
													Raw: []byte(`"ephemeral"`),
												},
												{
													Raw: []byte(`"memcached"`),
												},
											},
										},
										"memoryQuota": apiextensionsv1beta1.JSONSchemaProps{
											Type:    "integer",
											Minimum: &minimumBucketQuota,
										},
										"replicas":           apiextensionsv1beta1.JSONSchemaProps{},
										"ioPriority":         apiextensionsv1beta1.JSONSchemaProps{},
										"evictionPolicy":     apiextensionsv1beta1.JSONSchemaProps{},
										"conflictResolution": apiextensionsv1beta1.JSONSchemaProps{},
										"enableFlush":        apiextensionsv1beta1.JSONSchemaProps{},
										"enableIndexReplica": apiextensionsv1beta1.JSONSchemaProps{},
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
										"dataPath",
										"indexPath",
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
											Type:      "array",
											MinLength: &minimumServicesLength,
										},
										"dataPath": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
										},
										"indexPath": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
										},
										"pod": apiextensionsv1beta1.JSONSchemaProps{
											Type: "object",
											Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
												"couchbaseEnv": apiextensionsv1beta1.JSONSchemaProps{
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
