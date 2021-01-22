package config

import (
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/spf13/cobra"
)

const (
	// BackupResource is the name used for all resources involving automated backup.
	BackupResourceName = "couchbase-backup"
)

// generateBackupOptions defines options for generating backup resources.
type generateBackupOptions struct {
}

// getGenerateBackupCommand creates YAML capable of creating backup job prerequisites.
func getGenerateBackupCommand(flags *genericclioptions.ConfigFlags) *cobra.Command {
	o := &generateBackupOptions{}

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Generates YAML for backup jobs.",
		Long:  "Generates YAML for backup jobs.",
		RunE: func(cmd *cobra.Command, args []string) error {
			resources, err := o.generate(flags)
			if err != nil {
				return err
			}

			if err := dumpResources(resources); err != nil {
				return err
			}

			return nil
		},
	}

	return cmd
}

// getCreateBackupCommand creates backup job prerequisites.
func getCreateBackupCommand(flags *genericclioptions.ConfigFlags) *cobra.Command {
	o := &generateBackupOptions{}

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Creates backup roles.",
		Long:  "Creates backup roles.",
		RunE: func(cmd *cobra.Command, args []string) error {
			resources, err := o.generate(flags)
			if err != nil {
				return err
			}

			if err := createResources(flags, resources); err != nil {
				return err
			}

			return nil
		},
	}

	return cmd
}

// getDeleteBackupCommand deletes backup job prerequisites.
func getDeleteBackupCommand(flags *genericclioptions.ConfigFlags) *cobra.Command {
	o := &generateBackupOptions{}

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Deletes backup roles.",
		Long:  "Deletes backup roles.",
		RunE: func(cmd *cobra.Command, args []string) error {
			resources, err := o.generate(flags)
			if err != nil {
				return err
			}

			if err := deleteResources(flags, resources); err != nil {
				return err
			}

			return nil
		},
	}

	return cmd
}

// generate dumps all operator resources to standard out.
func (o *generateBackupOptions) generate(flags *genericclioptions.ConfigFlags) ([]runtime.Object, error) {
	namespace, _, err := flags.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return nil, err
	}

	resources := []runtime.Object{
		GetBackupServiceAccount(),
		GetBackupRole(),
		GetBackupRoleBinding(namespace),
	}

	return resources, nil
}

// GetBackupRole returns the role required for the backup script to function correctly.
func GetBackupRole() *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name: BackupResourceName,
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

// GetBackupServiceAccount returns a service account for the backup script to run as.
func GetBackupServiceAccount() *v1.ServiceAccount {
	return &v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: BackupResourceName,
		},
	}
}

// GetBackupRoleBinding returns a role binding linking the backup service account to its role.
func GetBackupRoleBinding(namespace string) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: BackupResourceName,
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
