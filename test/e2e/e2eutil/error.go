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
