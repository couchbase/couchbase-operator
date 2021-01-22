package framework

import (
	"context"

	"github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func CreateBackupRole(k8s *types.Cluster) error {
	backupRole := config.GetBackupRole()

	_, err := k8s.KubeClient.RbacV1().Roles(k8s.Namespace).Create(context.Background(), backupRole, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	return nil
}

func CreateBackupServiceAccount(k8s *types.Cluster) error {
	backupServiceAccount := config.GetBackupServiceAccount()

	_, err := k8s.KubeClient.CoreV1().ServiceAccounts(k8s.Namespace).Create(context.Background(), backupServiceAccount, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	return nil
}

func CreateBackupRoleBinding(k8s *types.Cluster) error {
	backupRoleBinding := config.GetBackupRoleBinding(k8s.Namespace)

	_, err := k8s.KubeClient.RbacV1().RoleBindings(k8s.Namespace).Create(context.Background(), backupRoleBinding, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	return nil
}
