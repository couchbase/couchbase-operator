/*
Copyright 2019-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

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
	BackupResourceName  = "couchbase-backup"
	BackupIAMAnnotation = "eks.amazonaws.com/role-arn"
)

// generateBackupOptions defines options for generating backup resources.
type generateBackupOptions struct {
	// backupIAMRoleARN is the ARN of the IAM role to be associated with the backup
	// service account.
	backupIAMRoleARN string
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

			return dumpResources(resources)
		},
	}

	o.registerBackupGenerateFlags(cmd)

	return cmd
}

// getCreateBackupCommand creates backup job prerequisites.
func getCreateBackupCommand(command string, flags *genericclioptions.ConfigFlags) *cobra.Command {
	o := &generateBackupOptions{}

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Creates backup roles.",
		Long:  "Creates backup roles.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if command != caoBinaryName {
				genDeprecatedWarning("https://docs.couchbase.com/operator/current/tools/cao.html#cao-create-backup-flags")
			}

			resources, err := o.generate(flags)
			if err != nil {
				return err
			}

			return createResources(flags, resources)
		},
	}

	o.registerBackupGenerateFlags(cmd)

	return cmd
}

// getDeleteBackupCommand deletes backup job prerequisites.
func getDeleteBackupCommand(command string, flags *genericclioptions.ConfigFlags) *cobra.Command {
	o := &generateBackupOptions{}

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Deletes backup roles.",
		Long:  "Deletes backup roles.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if command != caoBinaryName {
				genDeprecatedWarning("https://docs.couchbase.com/operator/current/tools/cao.html#cao-delete-backup")
			}

			resources, err := o.generate(flags)
			if err != nil {
				return err
			}

			return deleteResources(flags, resources)
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
		o.GetBackupServiceAccount(),
		o.GetBackupRole(),
		o.GetBackupRoleBinding(namespace),
	}

	return resources, nil
}

// GetBackupRole returns the role required for the backup script to function correctly.
func (o *generateBackupOptions) GetBackupRole() *rbacv1.Role {
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
func (o *generateBackupOptions) GetBackupServiceAccount() *v1.ServiceAccount {
	serviceAccount := &v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: BackupResourceName,
		},
	}

	if o.backupIAMRoleARN != "" {
		serviceAccount.Annotations[BackupIAMAnnotation] = o.backupIAMRoleARN
	}

	return serviceAccount
}

// GetBackupRoleBinding returns a role binding linking the backup service account to its role.
func (o *generateBackupOptions) GetBackupRoleBinding(namespace string) *rbacv1.RoleBinding {
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

// registerBackupGenerateFlags adds generic generation flags to the provided command.
func (o *generateBackupOptions) registerBackupGenerateFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.backupIAMRoleARN, "iam-role-arn", "", "Adds the IAM Role ARN to the backup service account's annotation. e.g arn:aws:iam::<ACCOUNT_ID>:role/<IAM_ROLE_NAME>")
}
