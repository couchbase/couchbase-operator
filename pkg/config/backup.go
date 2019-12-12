package config

import (
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetBackupRole(name string) *rbacv1.Role {
	return &rbacv1.Role{
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
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func GetBackupRoleBinding(name string) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      name,
				Namespace: "default",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     name,
		},
	}
}
