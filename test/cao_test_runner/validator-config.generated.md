
### Available Validators

Each validator can be added as part of a config of an action. The validators available are:

 * [collectLogs](#collectlogs)
 * [couchbaseReadiness](#couchbasereadiness)
 * [couchbaseVersion](#couchbaseversion)
 * [preFlight](#preflight)

---
#### collectLogs

Config symbol: `CollectLogs`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `State` | `string` | `yaml:state` |  |
| `CBServerLogs` | `bool` | `yaml:CBServerLogs` | `caoCli:required`  |
| `OperatorLogs` | `bool` | `yaml:operatorLogs` | `caoCli:required`  |

---
#### couchbaseReadiness

Config symbol: `CouchbaseReadiness`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `State` | `string` | `yaml:state` |  |

---
#### couchbaseVersion

Config symbol: `CBVersion`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `State` | `string` | `yaml:state` |  |
| `CBVersion` | `string` | `yaml:cbVersion` |  |

---
#### preFlight

Config symbol: `PreFlight`

| Name | Type | YAML Tag | CAO-CLI Tag |
| ---- | ---- | -------- | ----------- |
| `State` | `string` | `yaml:state` |  |

---
