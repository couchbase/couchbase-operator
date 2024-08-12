
### Available Actions

Each action can be executed as part of a scenario. All actions inherit the
[Global flags](README.md#global-flags) as common configuration, additional
configuration is also available on a per-action basis:

 * [Change Kubeconfig Context](#change-kubeconfig-context)
 * [Delta Upgrade](#delta-upgrade)
 * [Deploy Couchbase](#deploy-couchbase)
 * [Generic Workload](#generic-workload)
 * [Setup Admission Controller](#setup-admission-controller)
 * [Setup CAO Binary and Deploy CRDs](#setup-cao-binary-and-deploy-crds)
 * [Setup Operator](#setup-operator)
 * [Sleep](#sleep)

---
#### Change Kubeconfig Context

Config symbol: `KubeConfigContextSetupConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `K8sContext` | `string` | `yaml:k8sContext` | `caoCli:required`  |
| `Validators` | `slice` | `yaml:validators,omitempty` |  |

---
#### Delta Upgrade

Config symbol: `DeltaRecoveryUpgradeConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `CBClusterSpecPath` | `string` | `yaml:cbClusterSpecPath` | `caoCli:required`  |
| `Validators` | `slice` | `yaml:validators,omitempty` |  |

---
#### Deploy Couchbase

Config symbol: `CouchbaseConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `CBClusterSpecPath` | `string` | `yaml:cbClusterSpecPath` | `caoCli:required`  |
| `CBBucketsSpecPath` | `string` | `yaml:cbBucketsSpecPath` |  |
| `CBSecretsSpecPath` | `string` | `yaml:cbSecretsSpecPath` |  |
| `ApplyClusterSpecChanges` | `map` | `yaml:applyClusterSpecChanges` |  |
| `ApplyBucketSpecChanges` | `map` | `yaml:applyBucketSpecChanges` |  |
| `Validators` | `slice` | `yaml:validators,omitempty` |  |

---
#### Generic Workload

Config symbol: `GenericWorkloadConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Name` | `string` | `yaml:name` | `caoCli:required`  |
| `SpecPath` | `string` | `yaml:specPath` | `caoCli:required`  |
| `PreRunWait` | `int` | `yaml:preRunWait` |  |
| `PostRunWait` | `int` | `yaml:postRunWait` |  |
| `RunDuration` | `int` | `yaml:runDuration` | `caoCli:required`  |
| `CheckJobCompletion` | `bool` | `yaml:checkJobCompletion` |  |

---
#### Setup Admission Controller

Config symbol: `AdmissionControllerConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `AdmissionControllerImage` | `string` | `yaml:admissionControllerImage` |  |
| `CAOBinaryPath` | `string` | `yaml:caoBinaryPath` | `caoCli:required`  |
| `CPULimit` | `int` | `yaml:CPULimit` |  |
| `CPURequest` | `int` | `yaml:CPURequest` |  |
| `ImagePullPolicy` | `string` | `yaml:imagePullPolicy` |  |
| `ImagePullSecret` | `string` | `yaml:imagePullSecret,omitempty` |  |
| `AdmissionControllerLogLevel` | `int` | `yaml:admissionControllerLogLevel` |  |
| `MemoryLimit` | `int` | `yaml:memoryLimit` |  |
| `MemoryRequest` | `int` | `yaml:memoryRequest` |  |
| `Replicas` | `int` | `yaml:replicas` |  |
| `Scope` | `string` | `yaml:scope` |  |
| `ValidateSecrets` | `bool` | `yaml:validateSecrets` | `caoCli:required`  |
| `ValidateStorageClasses` | `bool` | `yaml:validateStorageClasses` | `caoCli:required`  |
| `Validators` | `slice` | `yaml:validators,omitempty` |  |

---
#### Setup CAO Binary and Deploy CRDs

Config symbol: `CaoCrdSetupConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `OperatorVersion` | `string` | `yaml:operatorVersion` | `caoCli:required`  |
| `Platform` | `string` | `yaml:platform` | `caoCli:required`  |
| `OperatingSystem` | `string` | `yaml:operatingSystem` | `caoCli:required`  |
| `Architecture` | `string` | `yaml:architecture` | `caoCli:required`  |
| `Validators` | `slice` | `yaml:validators,omitempty` |  |

---
#### Setup Operator

Config symbol: `OperatorConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `OperatorImage` | `string` | `yaml:operatorImage` |  |
| `CAOBinaryPath` | `string` | `yaml:caoBinaryPath` | `caoCli:required`  |
| `CPULimit` | `int` | `yaml:cpuLimit` |  |
| `CPURequest` | `int` | `yaml:cpuRequest` |  |
| `ImagePullPolicy` | `string` | `yaml:imagePullPolicy` |  |
| `ImagePullSecret` | `string` | `yaml:imagePullSecret,omitempty` |  |
| `OperatorLogLevel` | `int` | `yaml:operatorLogLevel` |  |
| `MemoryLimit` | `int` | `yaml:memoryLimit` |  |
| `MemoryRequest` | `int` | `yaml:memoryRequest` |  |
| `PodCreationTimeout` | `string` | `yaml:podCreationTimeout` |  |
| `PodDeleteDelay` | `string` | `yaml:podDeleteDelay` |  |
| `PodReadinessDelay` | `string` | `yaml:podReadinessDelay` |  |
| `PodReadinessPeriod` | `string` | `yaml:podReadinessPeriod` |  |
| `Scope` | `string` | `yaml:scope` |  |
| `Validators` | `slice` | `yaml:validators,omitempty` |  |

---
#### Sleep

Config symbol: `SleepActionConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Time` | `int` | `yaml:time` |  |

---
