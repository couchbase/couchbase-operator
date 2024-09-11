
### Available Validators

Each validator can be added as part of a config of an action. The validators available are:

 * [collectLogs](#collectlogs)
 * [couchbaseClusterSize](#couchbaseclustersize)
 * [couchbaseReadiness](#couchbasereadiness)
 * [couchbaseVersion](#couchbaseversion)
 * [kubeconfigContext](#kubeconfigcontext)
 * [labelTaintNodes](#labeltaintnodes)
 * [preFlight](#preflight)

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
#### labelTaintNodes

Config symbol: `LabelTaintNodes`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Name` | `string` | `yaml:name` | `caoCli:required`  |
| `State` | `string` | `yaml:state` | `caoCli:required`  |
| `NodeFilter` | `struct` | `yaml:nodeFilter` | `caoCli:required`  |
| `LabelKey` | `string` | `yaml:labelKey` |  |
| `LabelValue` | `string` | `yaml:labelValue` |  |
| `TaintKey` | `string` | `yaml:taintKey` |  |
| `TaintValue` | `string` | `yaml:taintValue` |  |
| `TaintEffect` | `string` | `yaml:taintEffect` |  |
| `ApplyTaint` | `bool` | `yaml:applyTaint` | `caoCli:required`  |
| `ApplyLabel` | `bool` | `yaml:applyLabel` | `caoCli:required`  |
| `RemoveTaint` | `bool` | `yaml:removeTaint` |  |
| `RemoveLabel` | `bool` | `yaml:removeLabel` |  |

---
#### preFlight

Config symbol: `PreFlight`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `Name` | `string` | `yaml:name` | `caoCli:required`  |
| `State` | `string` | `yaml:state` | `caoCli:required`  |

---
