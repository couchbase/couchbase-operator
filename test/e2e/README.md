Testing Framework
-----------------

The Couchbase Autonomous Operator Testing Framework is used for running automated functional regression tests, system tests, and cbopctl validation tests.

The testing framework has the goal of being highly customizeable for both Developer and QE testing requirements.

Basic Usage
-----------

To run a test or suite of tests, user must provide the required fields to `./resources/test_config.yaml`. 

Once `test_config.yaml` has the appropriate values, users can then run `make test-operator` from the top-level directory of the couchbase-operator repo.

Test Configuration
-----------

The test configuration is defined in `./resources/test_config.yaml`. The default `test_config.yaml` looks like the following:

```
operator-image: couchbase/couchbase-operator:v1
namespace: default
kube-config:
- config: /root/.kube/config
duration: 7
skip-tear-down: false
suite: TestCustom
kube-type: kubernetes
serviceAccountName: default
 ```

The following are descriptions of the field in `test_config.yaml`:

`operator-image`: the name of the autonomous operator docker image to test. If you built the image locally and are using Minikube or Minishift, you do not need to modify this field.

`namespace`: the namespace in the kubernetes cluster in which the test will be run. The testing framework will set up all required permissions (clusterrole, clusterrolebinding, and serviceaccount) in this namespace 

`kube-config`: the kubeconfig to use for testing. The kubeconfig will be associated with a cluster name. Each test requires a specific cluster name to run. The required cluster name is specified both in the test code and the test suite descriptor yamls. For basic testing on Minikube or Minishift, the default cluster name should be used, but the config should point to the Minikube or Minishift kubeconfig. If this `kube-config` is ommited or commented out, the testing framework will provision the require cluster using the set of VMs listed in `./resources/cluster_conf.yaml`.

`duration`: the time in days to run system tests.

`skip-tear-down`: this flag, if set to true, will leave the couchbase operator and couchbase cluster pods running after the last test has completed. This is mostly used for running and debugging single tests.

`suite`: the test suite descriptor file to run. The default test suite descriptor can be modified for custom testing, like running/debugging single tests.

`kube-type`: the kubernetes cluster type (kubernetes or openshift) to provision if no kubeconfig is provided to the framework.

`serviceAccountName`: the name of the serviceaccount to create for the Autonomous Operator.

Test Suite Descriptor
---------------------

All tests are run as described in a test suite descriptor file. The default test suite is `TestCustom.yaml`. This suite is intended to be modified for custom testing such as running individual tests or custom sanity tests. The file has the following contents:

```
testcases:
- TestCreateCluster
```

The following are descriptions of the field in `TestCustom.yaml` but are standard for all test suite descriptors:

`testcases`: list of test cases to run. Each test case requires a name of valid test listed in `./util.go`.
