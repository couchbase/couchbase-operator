package config

import (
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/spf13/cobra"
)

const (
	// BackupResource is the name used for all resources involving automated backup.
	BackupResourceName = "couchbase-backup"
)

// generateBackupOptions defines options for generating backup resources.
type generateBackupOptions struct {
	// file defines whether or not to output to a file.
	file bool
}

// getGenerateBackupCommand creates YAML capable of creating backup job prerequisites.
func getGenerateBackupCommand(flags *genericclioptions.ConfigFlags) *cobra.Command {
	o := &generateBackupOptions{}

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Generates YAML for backup jobs",
		Long:  "Generates YAML for backup jobs, these require Kubernetes roles to be bound to the job",
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.generate(flags)
		},
	}

	cmd.Flags().BoolVar(&o.file, "file", false, "Generate files rather than printing to the console")

	return cmd
}

// generate dumps all operator resources to standard out.
func (o *generateBackupOptions) generate(flags *genericclioptions.ConfigFlags) error {
	namespace, _, err := flags.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}

	if err := DumpYAML(o.file, "backup-service-account", GetBackupServiceAccount(namespace)); err != nil {
		return err
	}

	if err := DumpYAML(o.file, "backup-role", GetBackupRole(namespace)); err != nil {
		return err
	}

	if err := DumpYAML(o.file, "backup-role-binding", GetBackupRoleBinding(namespace)); err != nil {
		return err
	}

	return nil
}

func GetBackupRole(namespace string) *rbacv1.Role {
	return &rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "Role",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      BackupResourceName,
			Namespace: namespace,
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

func GetBackupServiceAccount(namespace string) *v1.ServiceAccount {
	return &v1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      BackupResourceName,
			Namespace: namespace,
		},
	}
}

func GetBackupRoleBinding(namespace string) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "RoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      BackupResourceName,
			Namespace: namespace,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      BackupResourceName,
				Namespace: namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     BackupResourceName,
		},
	}
}
