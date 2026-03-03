/*
Copyright 2017-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2eutil

// error handling for util methods.
import (
	"fmt"
	"strconv"
)

func NewErrVerifyEditBucket(bucket string) error {
	return fmt.Errorf("failed during edit bucket verification: %s", bucket)
}

func NewErrVerifyClusterInfo() error {
	return fmt.Errorf("failed to verify cluster info")
}

func NewErrAutoFailoverInfo() error {
	return fmt.Errorf("failed to verify autofailover info")
}

func NewErrVerifyServices() error {
	return fmt.Errorf("failed to verify services info")
}

func NewErrIndexSettingInfo() error {
	return fmt.Errorf("failed to verify index setting info")
}

func NewErrGetClusterBucket(bucket string) error {
	return fmt.Errorf("failed to get bucket from cluster %s", bucket)
}

func NewErrGetBucketSpec(bucket string) error {
	return fmt.Errorf("failed to get spec for bucket %s", bucket)
}

func NewErrServerConfigNotFound(configName string) error {
	return fmt.Errorf("failed to find server config in spec %s", configName)
}

func NewErrEmptyNodeList() error {
	return fmt.Errorf("node list is empty")
}

func NewErrConsoleNotExposed() error {
	return fmt.Errorf("admin console is not exposed")
}

func NewErrDefaultConfigPresent() error {
	return fmt.Errorf("default configuration is incorrectly present")
}

func NewErrLogInvalidConfig(managed, exists bool) error {
	return fmt.Errorf("invalid configuration as mismatch in config vs annotations: %s != %s", strconv.FormatBool(managed), strconv.FormatBool(exists))
}

func NewErrLogMissingConfigKey(name string) error {
	return fmt.Errorf("missing %q key", name)
}

func NewErrLogMissingConfigKeyContents(name string) error {
	return fmt.Errorf("missing contents of %q key", name)
}

func NewErrContainerCountInvalid(actual, expected int, podName string) error {
	return fmt.Errorf("invalid container count (%d != %d) for pod %s", actual, expected, podName)
}

func NewErrMissingAuditCleanupLogs(name string) error {
	return fmt.Errorf("missing audit GC logs for pod %q", name)
}

func NewErrMissingLogs(expected, name string) error {
	return fmt.Errorf("logs do not contain %q for pod %q", expected, name)
}

func NewErrUnexpectedSuccess(stepDescription string) error {
	return fmt.Errorf("unexpected success %s", stepDescription)
}

func NewErrLogInvalidClusterConfig() error {
	return fmt.Errorf("incorrectly specified cluster - need to disable management of configuration")
}

func NewErrAuditInvalidClusterConfig() error {
	return fmt.Errorf("auditing incorrectly enabled")
}

func NewErrAuditNotEnabled() error {
	return fmt.Errorf("auditing should be enabled")
}

func NewErrInvalidPathCheck(checkName, actual, expected string) error {
	return fmt.Errorf("%s not correct: %q != %q", checkName, actual, expected)
}

func NewErrInvalidIntegerCheck(checkName string, actual, expected int) error {
	return fmt.Errorf("%s not correct: %d != %d", checkName, actual, expected)
}

func NewErrInvalidLogConfigOwner(details string) error {
	return fmt.Errorf("invalid owner for configuration: %s, ", details)
}
