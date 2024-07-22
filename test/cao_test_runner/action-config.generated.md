
### Available Actions

Each action can be executed as part of a scenario. All actions inherit the
[Global flags](README.md#global-flags) as common configuration, additional
configuration is also available on a per-action basis:

 * [Delta Upgrade](#delta-upgrade)
 * [Deploy Couchbase](#deploy-couchbase)
 * [Generic Workload](#generic-workload)
 * [Setup Operator](#setup-operator)
 * [Sleep](#sleep)

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
#### Setup Operator

Config symbol: `OperatorConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Namespace` | `string` | `yaml:namespace` |  |
| `CRDS` | `string` | `yaml:CRDS` |  |
| `OperatorImage` | `string` | `yaml:operatorVersion` |  |
| `AdmissionControllerImage` | `string` | `yaml:admissionControllerVersion` |  |

---
#### Sleep

Config symbol: `SleepActionConfig`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Time` | `int` | `yaml:time` |  |

---
