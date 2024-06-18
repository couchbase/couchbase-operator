# cao-test-runner

## Table of Contents

* [Overview](#overview)
* [Examples](#examples-aws-local-dev)
    * [Run a scenario: Deploy  Cluster](#run-a-scenario-deploy-cluster)
    * [Run a scenario: Upgrade Cluster](#run-a-scenario-upgrade-cluster)
* [Directory Structure](#directory-structure)
* [Architecture](#architecture)
    * [Commands](#commands)
    * [Tasks, Actions, Validators and Trees](#tasks-actions-validators-and-trees)
    * [Config Tags](#config-tags)
    * [Scenarios](#scenarios)
    * [Validators](#validators)
    * [Multi-Scenarios](#multi-scenarios)
    * [Available Actions](action-config.generated.md)
    * [Available Validators](validator-config.generated.md)

## Overview

`cao-test-runner` is a command line tool created primarily to automate the scale, upgrade and volume testing of Couchbase Autonomous Operator.
It's built upon [spf13/cobra](https://github.com/spf13/cobra) and logger.
The task tree architecture was inspired by [eksctl](https://github.com/weaveworks/eksctl).

## Examples

### Run a scenario: Deploy Cluster

This scenario can be used to deploy a Couchbase Cluster using the Operator.

```shell
cd test/cao-test-runner

go run . scenario --scenario scenarios/deploy/data_node_couchase.yaml
```

### Run a scenario: Upgrade Cluster

This scenario is used to deploy a Couchbase Cluster and then perform an upgrade using the Operator.

```shell
cd test/cao-test-runner

go run . scenario --scenario scenarios/upgrade/delta_recovery.yaml
```

## Directory Structure

```
├── cao-test-runner
│   ├── actions
│   ├── cmd
│   ├── documentation
│   ├── result
│   ├── scenarios
│   ├── task
│   ├── test_data
│   ├── util
│   ├── validations
│   └── main.go
```

* `actions` - The action definitions themselves, broken down into types.
* `cmd` - The top-level cobra command.
* `documentation` - documentation for `cao-test-runner`.
* `result` - Handles the result output of scenario trees.
* `scenarios` - Contains all committed scenarios.
* `task` - This package contains the actions to be executed as part of commands or task trees.
* `test_data` - Contains all the YAMLs required for the tests.
* `util` - Contains all the utilities such as kubectl, helm, shell wrappers etc.
* `validations` - Contains all the different validations that can be added to any action in a plug and play fashion


## Architecture

### Commands

A **command** will be dispatched by cobra when a user executes `cao-test-runner` (see `main.go` for the entry point).
There is only the `scenario` command (see `cmd/scenario.go` for the definition),
this is used to execute `cao-test-runner` scenario files.

### Tasks, Actions, Validators and Trees

An **action**, found within `actions`, is used to encapsulate the execution of work required to complete a task - such
as deploying a cluster or upgrading a cluster. This facilitates the composition of many actions into a **tree** 
(also known as a task tree), whereby each action may be executed many times; for example deploying a cluster, running workloads, 
and upgrading cluster (1x `Deploy Cluster` + 2x `Run Workload` + 1x `Upgrade Cluster`). This is particularly useful
when it comes to creating various scenarios for automated scale, upgrade or chaos testing.

In terms of the difference between **task** and **action**, a task can be thought of as the node in the tree whereas the
**action** is the work that the task does; although they are sometimes used interchangeably.

### Config Tags

Configuration structs should be tagged using the `caoCli` tag.

* `required` - An error should be thrown if the field does not have a value or has the 0 value for that type.
* `context`
    * If the value exists in the yaml config and not the context, the yaml value is placed into the context.
    * If the value exists in the context but not the yaml config, the context value is placed into the struct field.
    * If the value exists in both, the context value is used.

The `context` tag is especially useful for cutting down boilerplate code as it covers checking both config sources and
reconciling them.

Configuration structs can also be tagged using the `env` tag to pull config values from environment variables. These take precedent over yaml values but not context.

Check the `cao-test-runner/actions/context` package for more detail.

Here is an example config struct tagged using the above tags:

```go
type CouchbaseConfig struct {
SpecPath   string         `yaml:"specPath" caoCli:"required"`
Validators map[string]any `yaml:"validators,omitempty"`
}
```

### Scenarios

A **scenario** comprises a **task tree** to describe an automated upgrade testing path, such
as:

* Couchbase Cluster Upgrade 7.2.5 to 7.6.1:
    * `scenarios/upgrade/delta_recovery.yaml`

A summary of the **task tree** action hierarchy for the scenario above might look something like this:

```shell
└── Create Namespace
    └── Deploy Operator
        └── Deploy Cluster
            └── Run Workloads
                └── Update Cluster
                    └── Destroy Cluster
                        └── Delete Namespace
```

See [Available Actions](generator/action-config.generated.md).

Each action within the tree will inherit the parent context of the actions above it - so in the example above,
the `Run Workloads` action will know about the cluster from the previous `Deploy Cluster` actions. Equally, the 
`Destroy Cluster` action will destroy the cluster created higher up the task tree. For this reason, dependent actions 
should be nested so that they can inherit the parent context.


### Validators

Validators are plug and play components that can be used to validate various things. All the `actions` have `config` 
which have the `validator`. We can add multiple validators to each action.

See [Available Validators](generator/validator-config.generated.md).

The YAML representation of a short scenario looks like this:

```yaml
action: "Deploy Couchbase Cluster"
config:
  specPath: "./test_data/couchbase_spec/single_data_couchbase.yaml"
  validators:
    preFlight:
      state: "PRE"
    couchbaseReadiness:
      state: "POST"
    couchbaseVersion:
      state: "POST"
      cbVersion: "7.2.5"
trees:
  - action: Delta Upgrade
    config:
      specPath: "./test_data/couchbase_spec/single_data_couchbase_upgrade.yaml"
      validators:
        couchbaseVersion:
          state: "POST"
          cbVersion: "7.6.1"
        couchbaseReadiness:
          state: "POST"
```

In the above example, the `config` object is passed to the action `Deploy Couchbase Cluster`. Once this action has 
completed, the task runner will then execute the next action within `trees` (which can contain further trees and actions recursively).
This allows for dependent actions to be executed only when their prerequisite actions are completed.


### Suites

A multi-scenario is called a suite. Suite is a scenario made up of many other scenarios, below is a fairly contrived example:

```yaml
timeoutInMins: 120
scenarios:
  -
    - "./scenarios/upgrade/delta_recovery.yaml"
    - "./scenarios/upgrade/swap_rebalance.yaml"
```

As you can see there is 1 groups of paths. In the group there are 2 scenarios. All scenarios in the same group are 
composed into a single tree and are executed in parallel. Once the group has completed all scenarios within it, it will 
move onto the next group and do the same.

An overview of the YAML schema for a multi scenario file is shown below:

```yaml
timeoutInMins: 300
scenarios:
  - []
```

Where:

* `timeoutInMins` - The time, in minutes, to allow for the execution of the action to complete - otherwise mark as
  failed using an error (default: `120`)
* `scenarios` - Is the list of groups of scenario files that are to be executed. Scenario files in the same group are
  executed as a part of the same tree in parallel. Groups are executed in sequence.

To execute a multi-scenario file just pass it in as you would with a regular scenario.

