package config

import (
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// BackupResource is the name used for all resources involving automated backup
	BackupResourceName = "couchbase-backup"
)

// DumpAdmissionYAML dumps all operator resources to standard out.
func DumpBackupYAML(conf *Config) error {
	if !conf.Backup {
		return nil
	}

	if err := DumpYAML(conf, "backup-service-account", GetBackupServiceAccount(BackupResourceName)); err != nil {
		return err
	}

	if err := DumpYAML(conf, "backup-role", GetBackupRole(BackupResourceName)); err != nil {
		return err
	}

	if err := DumpYAML(conf, "backup-role-binding", GetBackupRoleBinding(BackupResourceName, conf.Namespace)); err != nil {
		return err
	}

	return nil
}

func GetBackupRole(name string) *rbacv1.Role {
	return &rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "Role",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{
					"batch",
				},
				Resources: []string{
					"jobs",
					"cronjobs",
				},
				Verbs: []string{
					"get",
					"list",
				},
			},
			{
				APIGroups: []string{
					"",
				},
				Resources: []string{
					"pods",
				},
				Verbs: []string{
					"get",
					"list",
				},
			},
			{
				APIGroups: []string{
					"",
				},
				Resources: []string{
					"events",
				},
				Verbs: []string{
					"create",
				},
			},
			{
				APIGroups: []string{
					"couchbase.com",
				},
				Resources: []string{
					"couchbasebackups",
					"couchbasebackuprestores",
				},
				Verbs: []string{
					"get",
					"list",
					"watch",
					"patch",
					"update",
				},
			},
		},
	}
}

func GetBackupServiceAccount(name string) *v1.ServiceAccount {
	return &v1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func GetBackupRoleBinding(name, namespace string) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "RoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      name,
				Namespace: namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     name,
		},
	}
}
