package k8sutil

import (
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
)

var (
	maximumAutofailoverTimeout float64 = 3600
	minimumServiceMemoryQuota  float64 = 256
	minimumBucketQuota         float64 = 100
	minimumAutofailoverTimeout float64 = 1
	minimumBucketReplicaCount  float64 = 0
	minimumServicesLength      int64   = 1
	minimumServersLength       int64   = 1
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
							Type: "string",
						},
						"tls": apiextensionsv1beta1.JSONSchemaProps{
							Type: "object",
							Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
								"static": apiextensionsv1beta1.JSONSchemaProps{
									Type: "object",
									Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
										"member": apiextensionsv1beta1.JSONSchemaProps{
											Type: "object",
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
										"replicas",
										"ioPriority",
										"evictionPolicy",
										"conflictResolution",
										"enableFlush",
										"enableIndexReplica",
									},
									Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
										"name": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
										},
										"type": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
										},
										"memoryQuota": apiextensionsv1beta1.JSONSchemaProps{
											Type:    "integer",
											Minimum: &minimumBucketQuota,
										},
										"replicas": apiextensionsv1beta1.JSONSchemaProps{
											Type:    "integer",
											Minimum: &minimumBucketReplicaCount,
										},
										"ioPriority": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
										},
										"evictionPolicy": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
										},
										"conflictResolution": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
										},
										"enableFlush": apiextensionsv1beta1.JSONSchemaProps{
											Type: "boolean",
										},
										"enableIndexReplica": apiextensionsv1beta1.JSONSchemaProps{
											Type: "boolean",
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
										"services",
										"dataPath",
										"indexPath",
									},
									Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
										"size": apiextensionsv1beta1.JSONSchemaProps{
											Type: "integer",
										},
										"name": apiextensionsv1beta1.JSONSchemaProps{
											Type: "string",
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
												"persistentVolumes": apiextensionsv1beta1.JSONSchemaProps{
													Type: "object",
												},
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
