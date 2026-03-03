/*
Copyright 2019-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package framework

import (
	"os/exec"

	"github.com/couchbase/couchbase-operator/test/e2e/types"
)

func CreateBackupStuff(k8s *types.Cluster) error {
	args := []string{
		"create",
		"backup",
		"--namespace=" + k8s.Namespace,
		"--kubeconfig=" + k8s.KubeConfPath,
	}

	if k8s.Context != "" {
		args = append(args, "--context="+k8s.Context)
	}

	if _, err := exec.Command("../../build/bin/cbopcfg", args...).CombinedOutput(); err != nil {
		return err
	}

	return nil
}
