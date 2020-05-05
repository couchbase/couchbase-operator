package framework

import (
	"github.com/couchbase/couchbase-operator/pkg/config"
	"k8s.io/client-go/kubernetes"
)

func CreateBackupRole(client kubernetes.Interface, namespace string) error {
	backupRole := config.GetBackupRole(config.BackupResourceName)

	_, err := client.RbacV1().Roles(namespace).Create(backupRole)
	if err != nil {
		return err
	}

	return nil
}

func CreateBackupServiceAccount(client kubernetes.Interface, namespace string) error {
	backupServiceAccount := config.GetBackupServiceAccount(config.BackupResourceName)

	_, err := client.CoreV1().ServiceAccounts(namespace).Create(backupServiceAccount)
	if err != nil {
		return err
	}

	return nil
}

func CreateBackupRoleBinding(client kubernetes.Interface, namespace string) error {
	backupRoleBinding := config.GetBackupRoleBinding(config.BackupResourceName, namespace)

	_, err := client.RbacV1().RoleBindings(namespace).Create(backupRoleBinding)
	if err != nil {
		return err
	}

	return nil
}
