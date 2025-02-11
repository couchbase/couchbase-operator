
### Available Validators

Each validator can be added as part of a config of an action. The validators available are:

 * [AdmissionControllerValidator](#admissioncontrollervalidator)
 * [CAOCRDValidator](#caocrdvalidator)
 * [DestroyAdmissionValidator](#destroyadmissionvalidator)
 * [DestroyCRDValidator](#destroycrdvalidator)
 * [DestroyKubernetesClusterValidator](#destroykubernetesclustervalidator)
 * [DestroyOperatorValidator](#destroyoperatorvalidator)
 * [KubernetesClusterValidator](#kubernetesclustervalidator)
 * [NamespaceValidator](#namespacevalidator)
 * [OperatorValidator](#operatorvalidator)
 * [collectLogs](#collectlogs)
 * [couchbaseClusterSize](#couchbaseclustersize)
 * [couchbaseReadiness](#couchbasereadiness)
 * [couchbaseVersion](#couchbaseversion)
 * [kubeconfigContext](#kubeconfigcontext)
 * [preFlight](#preflight)

---
#### AdmissionControllerValidator

Config symbol: `AdmissionControllerValidator`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Name` | `string` | `yaml:name` | `caoCli:required`  |
| `ClusterName` | `string` | `yaml:clusterName` | `caoCli:required,context`  |
| `Namespace` | `string` | `yaml:namespace` | `caoCli:required,context`  |
| `State` | `string` | `yaml:state` | `caoCli:required`  |
| `AdmissionControllerConfig` | `ptr` | `yaml:admissionControllerConfig` |  |

---
#### CAOCRDValidator

Config symbol: `CAOCRDValidator`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Name` | `string` | `yaml:name` | `caoCli:required`  |
| `ClusterName` | `string` | `yaml:clusterName` | `caoCli:required,context`  |
| `State` | `string` | `yaml:state` | `caoCli:required`  |
| `CAOPath` | `string` | `yaml:caoPath` |  |
| `OperatorVersion` | `string` | `yaml:operatorVersion` |  |

---
#### DestroyAdmissionValidator

Config symbol: `DestroyAdmissionControllerValidator`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Name` | `string` | `yaml:name` | `caoCli:required`  |
| `Namespace` | `string` | `yaml:namespace` | `caoCli:required,context`  |
| `ClusterName` | `string` | `yaml:clusterName` | `caoCli:required,context`  |
| `State` | `string` | `yaml:state` | `caoCli:required`  |

---
#### DestroyCRDValidator

Config symbol: `DestroyCRDValidator`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Name` | `string` | `yaml:name` | `caoCli:required`  |
| `ClusterName` | `string` | `yaml:clusterName` | `caoCli:required,context`  |
| `State` | `string` | `yaml:state` | `caoCli:required`  |

---
#### DestroyKubernetesClusterValidator

Config symbol: `KubernetesClusterValidator`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Name` | `string` | `yaml:name` | `caoCli:required`  |
| `ClusterName` | `string` | `yaml:clusterName` | `caoCli:required,context`  |
| `State` | `string` | `yaml:state` | `caoCli:required`  |

---
#### DestroyOperatorValidator

Config symbol: `DestroyOperatorValidator`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Name` | `string` | `yaml:name` | `caoCli:required`  |
| `Namespace` | `string` | `yaml:namespace` | `caoCli:required,context`  |
| `ClusterName` | `string` | `yaml:clusterName` | `caoCli:required,context`  |
| `State` | `string` | `yaml:state` | `caoCli:required`  |

---
#### KubernetesClusterValidator

Config symbol: `KubernetesClusterValidator`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Name` | `string` | `yaml:name` | `caoCli:required`  |
| `ClusterName` | `string` | `yaml:clusterName` | `caoCli:required,context`  |
| `State` | `string` | `yaml:state` | `caoCli:required`  |
| `KindConfig` | `ptr` | `yaml:kindConfig` |  |
| `EKSConfig` | `ptr` | `yaml:eksConfig` |  |
| `AKSConfig` | `ptr` | `yaml:aksConfig` |  |
| `GKEConfig` | `ptr` | `yaml:gkeConfig` |  |

---
#### NamespaceValidator

Config symbol: `NamespaceValidator`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Name` | `string` | `yaml:name` | `caoCli:required`  |
| `ClusterName` | `string` | `yaml:clusterName` | `caoCli:required,context`  |
| `State` | `string` | `yaml:state` | `caoCli:required`  |
| `Namespaces` | `slice` | `yaml:namespaces` |  |

---
#### OperatorValidator

Config symbol: `OperatorValidator`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Name` | `string` | `yaml:name` | `caoCli:required`  |
| `ClusterName` | `string` | `yaml:clusterName` | `caoCli:required,context`  |
| `Namespace` | `string` | `yaml:namespace` | `caoCli:required,context`  |
| `State` | `string` | `yaml:state` | `caoCli:required`  |
| `OperatorConfig` | `ptr` | `yaml:operatorConfig` |  |

---
#### collectLogs

Config symbol: `CollectLogs`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Name` | `string` | `yaml:name` | `caoCli:required`  |
| `State` | `string` | `yaml:state` |  |
| `CBServerLogs` | `bool` | `yaml:cbServerLogs` | `caoCli:required`  |
| `OperatorLogs` | `bool` | `yaml:operatorLogs` | `caoCli:required`  |
| `LogSpecPath` | `string` | `yaml:logSpecPath` |  |
| `LogName` | `string` | `yaml:logName` | `caoCli:required`  |
| `CAOBinaryPath` | `string` | `yaml:caoBinaryPath` | `caoCli:context`  |

---
#### couchbaseClusterSize

Config symbol: `CouchbaseClusterSize`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Name` | `string` | `yaml:name` | `caoCli:required`  |
| `State` | `string` | `yaml:state` | `caoCli:required`  |
| `MapServerNameToSize` | `map` | `yaml:mapServerNameToSize` |  |
| `DurationInSecs` | `int64` | `yaml:durationInSecs` |  |
| `IntervalInSecs` | `int64` | `yaml:intervalInSecs` |  |
| `PreRunWaitInSecs` | `int64` | `yaml:preRunWaitInSecs` |  |

---
#### couchbaseReadiness

Config symbol: `CouchbaseReadiness`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Name` | `string` | `yaml:name` | `caoCli:required`  |
| `State` | `string` | `yaml:state` | `caoCli:required`  |
| `DurationInSecs` | `int64` | `yaml:durationInSecs` |  |
| `IntervalInSecs` | `int64` | `yaml:intervalInSecs` |  |

---
#### couchbaseVersion

Config symbol: `CBVersion`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Name` | `string` | `yaml:name` | `caoCli:required`  |
| `State` | `string` | `yaml:state` | `caoCli:required`  |
| `CBVersion` | `string` | `yaml:cbVersion` | `caoCli:required`  |
| `DurationInMinutes` | `int64` | `yaml:durationInMinutes` |  |
| `IntervalInMinutes` | `int64` | `yaml:intervalInMinutes` |  |

---
#### kubeconfigContext

Config symbol: `KubeConfigValidator`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Name` | `string` | `yaml:name` | `caoCli:required`  |
| `State` | `string` | `yaml:state` | `caoCli:required`  |
| `CurrentContext` | `string` | `yaml:k8sContext` | `caoCli:required`  |
| `DurationInSecs` | `int64` | `yaml:durationInSecs` |  |
| `IntervalInSecs` | `int64` | `yaml:intervalInSecs` |  |

---
#### preFlight

Config symbol: `PreFlight`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Name` | `string` | `yaml:name` | `caoCli:required`  |
| `State` | `string` | `yaml:state` | `caoCli:required`  |

---
