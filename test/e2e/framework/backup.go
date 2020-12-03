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

	if _, err := exec.Command("/cbopcfg", args...).CombinedOutput(); err != nil {
		return err
	}

	return nil
}
