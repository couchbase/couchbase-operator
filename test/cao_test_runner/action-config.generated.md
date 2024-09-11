
### Available Actions

Each action can be executed as part of a scenario. All actions inherit the
[Global flags](README.md#global-flags) as common configuration, additional
configuration is also available on a per-action basis:

 * [Change Kubeconfig Context](#change-kubeconfig-context)
 * [Delete CRDs](#delete-crds)
 * [Delta Upgrade](#delta-upgrade)
 * [Deploy Couchbase](#deploy-couchbase)
 * [Destroy Admission Controller](#destroy-admission-controller)
 * [Destroy Kubernetes Cluster](#destroy-kubernetes-cluster)
 * [Destroy Operator](#destroy-operator)
 * [Generic Workload](#generic-workload)
 * [Setup Admission Controller](#setup-admission-controller)
 * [Setup CAO Binary and Deploy CRDs](#setup-cao-binary-and-deploy-crds)
 * [Setup Kubernetes Cluster](#setup-kubernetes-cluster)
 * [Setup Operator](#setup-operator)
 * [Sleep](#sleep)

---
#### Change Kubeconfig Context

Config symbol: `KubeConfigContextSetupConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `K8sContext` | `string` | `yaml:k8sContext` | `caoCli:required,context`  |
| `Validators` | `slice` | `yaml:validators,omitempty` |  |

---
#### Delete CRDs

Config symbol: `CRDDestroyConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `CRDPath` | `string` | `yaml:crdPath` | `caoCli:required,context`  |
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
#### Destroy Admission Controller

Config symbol: `AdmissionControllerConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `CAOBinaryPath` | `string` | `yaml:caoBinaryPath` | `caoCli:required,context`  |
| `Scope` | `string` | `yaml:scope` |  |
| `Validators` | `slice` | `yaml:validators,omitempty` |  |

---
#### Destroy Kubernetes Cluster

Config symbol: `KubernetesDestroyConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `ClusterName` | `string` | `yaml:clusterName` | `caoCli:required,context`  |
| `Platform` | `string` | `yaml:platform` | `caoCli:required,context`  |
| `Environment` | `string` | `yaml:environment` | `caoCli:required,context`  |
| `Provider` | `string` | `yaml:provider` | `caoCli:context`  |
| `EKSRegion` | `string` | `yaml:eksRegion` | `caoCli:context`  |
| `AKSRegion` | `string` | `yaml:aksRegion` | `caoCli:context`  |
| `GKERegion` | `string` | `yaml:gkeRegion` | `caoCli:context`  |
| `Validators` | `slice` | `yaml:validators,omitempty` |  |

---
#### Destroy Operator

Config symbol: `OperatorConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `CAOBinaryPath` | `string` | `yaml:caoBinaryPath` | `caoCli:required,context`  |
| `Scope` | `string` | `yaml:scope` |  |
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
| `Description` | `slice` | `yaml:description` |  |
| `AdmissionControllerImage` | `string` | `yaml:admissionControllerImage` | `caoCli:context`  |
| `CAOBinaryPath` | `string` | `yaml:caoBinaryPath` | `caoCli:required,context`  |
| `CPULimit` | `int` | `yaml:cpuLimit` |  |
| `CPURequest` | `int` | `yaml:cpuRequest` |  |
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
| `OperatorVersion` | `string` | `yaml:operatorVersion` | `caoCli:required,context`  |
| `Platform` | `string` | `yaml:platform` | `caoCli:required,context`  |
| `OperatingSystem` | `string` | `yaml:operatingSystem` | `caoCli:required,context`  |
| `Architecture` | `string` | `yaml:architecture` | `caoCli:required,context`  |
| `Validators` | `slice` | `yaml:validators,omitempty` |  |

---
#### Setup Kubernetes Cluster

Config symbol: `KubernetesSetupConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `ClusterName` | `string` | `yaml:clusterName` | `caoCli:required,context`  |
| `Platform` | `string` | `yaml:platform` | `caoCli:required,context`  |
| `Environment` | `string` | `yaml:environment` | `caoCli:required,context`  |
| `NumControlPlane` | `int` | `yaml:numControlPlane` |  |
| `NumWorkers` | `int` | `yaml:numWorkers` |  |
| `OperatorImage` | `string` | `yaml:operatorImage` | `caoCli:required,context`  |
| `AdmissionControllerImage` | `string` | `yaml:admissionControllerImage` | `caoCli:required,context`  |
| `Provider` | `string` | `yaml:provider` | `caoCli:context`  |
| `EKSRegion` | `string` | `yaml:eksRegion` | `caoCli:context`  |
| `AKSRegion` | `string` | `yaml:aksRegion` | `caoCli:context`  |
| `GKERegion` | `string` | `yaml:gkeRegion` | `caoCli:context`  |
| `KubernetesVersion` | `string` | `yaml:kubernetesVersion` |  |
| `InstanceType` | `string` | `yaml:instanceType` |  |
| `NumNodeGroups` | `int` | `yaml:numNodeGroups` |  |
| `MinSize` | `int` | `yaml:minSize` |  |
| `MaxSize` | `int` | `yaml:maxSize` |  |
| `DesiredSize` | `int` | `yaml:desiredSize` |  |
| `DiskSize` | `int` | `yaml:diskSize` |  |
| `AMI` | `string` | `yaml:ami` |  |
| `KubeConfigPath` | `string` | `yaml:kubeconfigPath` | `caoCli:context`  |
| `OSSKU` | `string` | `yaml:osSKU` |  |
| `OSType` | `string` | `yaml:osType` |  |
| `VMSize` | `string` | `yaml:vmSize` |  |
| `Count` | `int` | `yaml:count` |  |
| `NumNodePools` | `int` | `yaml:numNodePools` |  |
| `MachineType` | `string` | `yaml:machineType` |  |
| `ImageType` | `string` | `yaml:imageType` |  |
| `DiskType` | `string` | `yaml:diskType` |  |
| `ReleaseChannel` | `string` | `yaml:releaseChannel` |  |
| `Validators` | `slice` | `yaml:validators,omitempty` |  |

---
#### Setup Operator

Config symbol: `OperatorConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `OperatorImage` | `string` | `yaml:operatorImage` | `caoCli:context`  |
| `CAOBinaryPath` | `string` | `yaml:caoBinaryPath` | `caoCli:required,context`  |
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
