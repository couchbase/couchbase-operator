package framework

import (
	"github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
)

func CreateBackupRole(k8s *types.Cluster) error {
	backupRole := config.GetBackupRole(config.BackupResourceName)

	_, err := k8s.KubeClient.RbacV1().Roles(k8s.Namespace).Create(backupRole)
	if err != nil {
		return err
	}

	return nil
}

func CreateBackupServiceAccount(k8s *types.Cluster) error {
	backupServiceAccount := config.GetBackupServiceAccount(config.BackupResourceName)

	_, err := k8s.KubeClient.CoreV1().ServiceAccounts(k8s.Namespace).Create(backupServiceAccount)
	if err != nil {
		return err
	}

	return nil
}

func CreateBackupRoleBinding(k8s *types.Cluster) error {
	backupRoleBinding := config.GetBackupRoleBinding(config.BackupResourceName, k8s.Namespace)

	_, err := k8s.KubeClient.RbacV1().RoleBindings(k8s.Namespace).Create(backupRoleBinding)
	if err != nil {
		return err
	}

	return nil
}
