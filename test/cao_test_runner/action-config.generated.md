
### Available Actions

Each action can be executed as part of a scenario. All actions inherit the
[Global flags](README.md#global-flags) as common configuration, additional
configuration is also available on a per-action basis:

 * [Bare Metal Chaos](#bare-metal-chaos)
 * [Change Current Namespace](#change-current-namespace)
 * [Change Kubeconfig Context](#change-kubeconfig-context)
 * [Chaos](#chaos)
 * [Create Namespace](#create-namespace)
 * [Data Workload](#data-workload)
 * [Delete CRDs](#delete-crds)
 * [Delta Upgrade](#delta-upgrade)
 * [Deploy Couchbase](#deploy-couchbase)
 * [Destroy Admission Controller](#destroy-admission-controller)
 * [Destroy Kubernetes Cluster](#destroy-kubernetes-cluster)
 * [Destroy Operator](#destroy-operator)
 * [Generic Workload](#generic-workload)
 * [Index Workload](#index-workload)
 * [Label Taint Nodes](#label-taint-nodes)
 * [Query Workload](#query-workload)
 * [Setup Admission Controller](#setup-admission-controller)
 * [Setup CAO Binary and Deploy CRDs](#setup-cao-binary-and-deploy-crds)
 * [Setup Kubernetes Cluster](#setup-kubernetes-cluster)
 * [Setup Operator](#setup-operator)
 * [Sleep](#sleep)
 * [Upgrade Kubernetes Cluster](#upgrade-kubernetes-cluster)

---
#### Bare Metal Chaos

Config symbol: `BareMetalChaosConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `CloudProvider` | `string` | `yaml:cloudProvider` | `caoCli:required`  |
| `CloudRegion` | `string` | `yaml:cloudRegion` | `caoCli:required`  |
| `InstanceDNS` | `slice` | `yaml:instanceDNS` | `caoCli:required`  |
| `ChaosList` | `slice` | `yaml:chaosList` | `caoCli:required`  |

---
#### Change Current Namespace

Config symbol: `NamespaceChangeConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `Namespace` | `string` | `yaml:namespace` | `caoCli:required,context`  |

---
#### Change Kubeconfig Context

Config symbol: `KubeConfigContextChangeConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `K8SContext` | `string` | `yaml:k8sContext` | `caoCli:required,context`  |

---
#### Chaos

Config symbol: `ChaosConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `ClusterName` | `string` | `yaml:clusterName` | `caoCli:required,context`  |
| `ChaosList` | `slice` | `yaml:chaosList` | `caoCli:required`  |
| `PortForward` | `bool` | `yaml:portForward` | `caoCli:context`  |
| `ms` | `ptr` | |  |

---
#### Create Namespace

Config symbol: `SetupNamespaceConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `Namespace` | `string` | `yaml:namespace` | `caoCli:context`  |

---
#### Data Workload

Config symbol: `DataWorkloadConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Name` | `string` | `yaml:name` | `caoCli:required`  |
| `Namespace` | `string` | `yaml:namespace` | `caoCli:context,required`  |
| `CBClusterSecret` | `string` | `yaml:cbClusterSecret` | `caoCli:required`  |
| `PreRunWait` | `int64` | `yaml:preRunWait` |  |
| `PostRunWait` | `int64` | `yaml:postRunWait` |  |
| `RunDuration` | `int64` | `yaml:runDuration` | `caoCli:required`  |
| `DataWorkloadName` | `string` | `yaml:dataWorkloadName` | `caoCli:required`  |
| `NumDataPods` | `int` | `yaml:numDataPods` | `caoCli:required`  |
| `CheckJobCompletion` | `bool` | `yaml:checkJobCompletion` |  |
| `NodeSelector` | `map` | `yaml:nodeSelector` |  |
| `BucketCount` | `int` | `yaml:bucketCount` | `caoCli:required`  |
| `Buckets` | `slice` | `yaml:buckets` |  |
| `OpsRate` | `int` | `yaml:opsRate` |  |
| `RangeStart` | `int64` | `yaml:rangeStart` |  |
| `RangeEnd` | `int64` | `yaml:rangeEnd` |  |
| `DocSize` | `int64` | `yaml:docSize` |  |
| `Creates` | `int` | `yaml:creates` |  |
| `Reads` | `int` | `yaml:reads` |  |
| `Updates` | `int` | `yaml:updates` |  |
| `Deletes` | `int` | `yaml:deletes` |  |
| `Expires` | `int` | `yaml:expires` |  |
| `TTL` | `int` | `yaml:ttl` |  |
| `cbDataPods` | `slice` | `yaml:-` |  |

---
#### Delete CRDs

Config symbol: `CRDDestroyConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `ClusterName` | `string` | `yaml:clusterName` | `caoCli:required,context`  |

---
#### Delta Upgrade

Config symbol: `DeltaRecoveryUpgradeConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `CBClusterSpecPath` | `string` | `yaml:cbClusterSpecPath` | `caoCli:required`  |

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

---
#### Destroy Admission Controller

Config symbol: `AdmissionControllerConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `Scope` | `string` | `yaml:scope` |  |

---
#### Destroy Kubernetes Cluster

Config symbol: `KubernetesDestroyConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `ClusterName` | `string` | `yaml:clusterName` | `caoCli:required,context`  |
| `EKSRegion` | `string` | `yaml:eksRegion` | `caoCli:context`  |
| `AKSRegion` | `string` | `yaml:aksRegion` | `caoCli:context`  |
| `GKERegion` | `string` | `yaml:gkeRegion` | `caoCli:context`  |
| `ms` | `ptr` | |  |

---
#### Destroy Operator

Config symbol: `OperatorConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `Scope` | `string` | `yaml:scope` |  |

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
#### Index Workload

Config symbol: `IndexWorkloadConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Name` | `string` | `yaml:name` | `caoCli:required`  |
| `Namespace` | `string` | `yaml:namespace` | `caoCli:context,required`  |
| `CBClusterSecret` | `string` | `yaml:cbClusterSecret` | `caoCli:required`  |
| `RunDuration` | `int64` | `yaml:runDuration` | `caoCli:required`  |
| `PreRunWait` | `int64` | `yaml:preRunWait` |  |
| `PostRunWait` | `int64` | `yaml:postRunWait` |  |
| `CheckJobCompletion` | `bool` | `yaml:checkJobCompletion` |  |
| `IndexWorkloadName` | `string` | `yaml:indexWorkloadName` | `caoCli:required`  |
| `NumQueryPods` | `int` | `yaml:numQueryPods` | `caoCli:required`  |
| `NumIndexPods` | `int` | `yaml:numIndexPods` | `caoCli:required`  |
| `NodeSelector` | `map` | `yaml:nodeSelector` |  |
| `BucketCount` | `int` | `yaml:bucketCount` | `caoCli:required`  |
| `Buckets` | `slice` | `yaml:buckets` |  |
| `IndexConfig` | `struct` | `yaml:indexConfig` |  |
| `queries` | `map` | `yaml:-` |  |
| `cbQueryPods` | `slice` | `yaml:-` |  |

---
#### Label Taint Nodes

Config symbol: `LabelTaintNodeConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Name` | `string` | `yaml:name` | `caoCli:required`  |
| `Description` | `slice` | `yaml:description` |  |
| `NodeFilter` | `ptr` | `yaml:nodeFilter` | `caoCli:required`  |
| `ApplyLabel` | `bool` | `yaml:applyLabel` |  |
| `RemoveLabel` | `bool` | `yaml:removeLabel` |  |
| `Labels` | `slice` | `yaml:labels` |  |
| `ApplyTaint` | `bool` | `yaml:applyTaint` |  |
| `RemoveTaint` | `bool` | `yaml:removeTaint` |  |
| `Taints` | `slice` | `yaml:taints` |  |

---
#### Query Workload

Config symbol: `QueryWorkloadConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Name` | `string` | `yaml:name` | `caoCli:required`  |
| `Namespace` | `string` | `yaml:namespace` | `caoCli:context,required`  |
| `CBClusterSecret` | `string` | `yaml:cbClusterSecret` | `caoCli:required`  |
| `RunDuration` | `int64` | `yaml:runDuration` | `caoCli:required`  |
| `PreRunWait` | `int64` | `yaml:preRunWait` |  |
| `PostRunWait` | `int64` | `yaml:postRunWait` |  |
| `CheckJobCompletion` | `bool` | `yaml:checkJobCompletion` |  |
| `QueryWorkloadName` | `string` | `yaml:queryWorkloadName` | `caoCli:required`  |
| `NumQueryPods` | `int` | `yaml:numQueryPods` | `caoCli:required`  |
| `NodeSelector` | `map` | `yaml:nodeSelector` |  |
| `BucketCount` | `int` | `yaml:bucketCount` | `caoCli:required`  |
| `Buckets` | `slice` | `yaml:buckets` |  |
| `QueryConfig` | `struct` | `yaml:queryConfig` |  |
| `cbQueryPods` | `slice` | `yaml:-` |  |

---
#### Setup Admission Controller

Config symbol: `AdmissionControllerConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `AdmissionControllerImage` | `string` | `yaml:admissionControllerImage` |  |
| `CPULimit` | `int` | `yaml:cpuLimit` |  |
| `CPURequest` | `int` | `yaml:cpuRequest` |  |
| `ImagePullPolicy` | `string` | `yaml:imagePullPolicy` |  |
| `ImagePullSecret` | `struct` | `yaml:imagePullSecret` |  |
| `AdmissionControllerLogLevel` | `int` | `yaml:admissionControllerLogLevel` |  |
| `MemoryLimit` | `int` | `yaml:memoryLimit` |  |
| `MemoryRequest` | `int` | `yaml:memoryRequest` |  |
| `Replicas` | `int` | `yaml:replicas` |  |
| `Scope` | `string` | `yaml:scope` |  |
| `ValidateSecrets` | `bool` | `yaml:validateSecrets` | `caoCli:required`  |
| `ValidateStorageClasses` | `bool` | `yaml:validateStorageClasses` | `caoCli:required`  |

---
#### Setup CAO Binary and Deploy CRDs

Config symbol: `CaoCrdSetupConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `OperatorVersion` | `string` | `yaml:operatorVersion` | `caoCli:required`  |
| `ClusterName` | `string` | `yaml:clusterName` | `caoCli:required,context`  |

---
#### Setup Kubernetes Cluster

Config symbol: `KubernetesSetupConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `ClusterName` | `string` | `yaml:clusterName` | `caoCli:required,context`  |
| `Platform` | `string` | `yaml:platform` | `caoCli:required`  |
| `Environment` | `string` | `yaml:environment` | `caoCli:required`  |
| `NumControlPlane` | `int` | `yaml:numControlPlane` |  |
| `NumWorkers` | `int` | `yaml:numWorkers` |  |
| `LoadDockerImageToKind` | `bool` | `yaml:loadDockerImageToKind` |  |
| `OperatorImage` | `string` | `yaml:operatorImage` | `caoCli:required`  |
| `AdmissionControllerImage` | `string` | `yaml:admissionControllerImage` | `caoCli:required`  |
| `Provider` | `string` | `yaml:provider` |  |
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
| `OSSKU` | `string` | `yaml:osSKU` |  |
| `OSType` | `string` | `yaml:osType` |  |
| `VMSize` | `string` | `yaml:vmSize` |  |
| `Count` | `int` | `yaml:count` |  |
| `NumNodePools` | `int` | `yaml:numNodePools` |  |
| `MachineType` | `string` | `yaml:machineType` |  |
| `ImageType` | `string` | `yaml:imageType` |  |
| `DiskType` | `string` | `yaml:diskType` |  |
| `ReleaseChannel` | `string` | `yaml:releaseChannel` |  |
| `kubeconfigPath` | `ptr` | |  |
| `ms` | `ptr` | |  |
| `resultsDirectory` | `ptr` | |  |

---
#### Setup Operator

Config symbol: `OperatorConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `OperatorImage` | `string` | `yaml:operatorImage` |  |
| `CPULimit` | `int` | `yaml:cpuLimit` |  |
| `CPURequest` | `int` | `yaml:cpuRequest` |  |
| `ImagePullPolicy` | `string` | `yaml:imagePullPolicy` |  |
| `ImagePullSecret` | `struct` | `yaml:imagePullSecret` |  |
| `OperatorLogLevel` | `int` | `yaml:operatorLogLevel` |  |
| `MemoryLimit` | `int` | `yaml:memoryLimit` |  |
| `MemoryRequest` | `int` | `yaml:memoryRequest` |  |
| `PodCreationTimeout` | `string` | `yaml:podCreationTimeout` |  |
| `PodDeleteDelay` | `string` | `yaml:podDeleteDelay` |  |
| `PodReadinessDelay` | `string` | `yaml:podReadinessDelay` |  |
| `PodReadinessPeriod` | `string` | `yaml:podReadinessPeriod` |  |
| `Scope` | `string` | `yaml:scope` |  |

---
#### Sleep

Config symbol: `SleepActionConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Duration` | `int64` | `yaml:duration` |  |

---
#### Upgrade Kubernetes Cluster

Config symbol: `KubernetesUpgradeConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Description` | `slice` | `yaml:description` |  |
| `ClusterName` | `string` | `yaml:clusterName` | `caoCli:required,context`  |
| `EKSRegion` | `string` | `yaml:eksRegion` | `caoCli:context`  |
| `AKSRegion` | `string` | `yaml:aksRegion` | `caoCli:context`  |
| `GKERegion` | `string` | `yaml:gkeRegion` | `caoCli:context`  |
| `KubernetesVersion` | `string` | `yaml:kubernetesVersion` |  |
| `UpgradeClusterVersion` | `bool` | `yaml:upgradeClusterVersion` |  |
| `WaitForClusterUpgrade` | `bool` | `yaml:waitForClusterUpgrade` |  |
| `UpgradeAKSNodePools` | `bool` | `yaml:upgradeAKSNodePools` |  |
| `AKSNodePoolsToUpgrade` | `slice` | `yaml:aksNodePoolsToUpgrade` |  |
| `UpgradeEKSNodeGroups` | `bool` | `yaml:upgradeEKSNodeGroups` |  |
| `EKSNodeGroupsToUpgrade` | `slice` | `yaml:eksNodeGroupsToUpgrade` |  |
| `UpgradeGKEMaster` | `bool` | `yaml:upgradeGKEMaster` |  |
| `WaitForGKEMasterUpgrade` | `bool` | `yaml:waitForGKEMasterUpgrade` |  |
| `UpgradeGKENodePool` | `bool` | `yaml:upgradeGKENodePool` |  |
| `GKENodePoolsToUpgrade` | `slice` | `yaml:gkeNodePoolsToUpgrade` |  |
| `ms` | `ptr` | |  |

---
