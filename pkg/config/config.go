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
	"fmt"

	"github.com/couchbase/couchbase-operator/pkg/specgen"
	"github.com/ghodss/yaml"

	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/spf13/cobra"
)

const (
	GenerateCmd = "generate"
	CreateCmd   = "create"
	DeleteCmd   = "delete"
	UpdateCmd   = "update"
	GenSpecCmd  = "genspec"
)

// ApplySubCommands attaches the configuration (create/delete/generate) sub commands to
// an arbitrary root command.
func ApplySubCommands(root *cobra.Command, flags *genericclioptions.ConfigFlags) {
	// 'cao generate' creates YAML for various Operator deployments.
	generate := &cobra.Command{
		Use:   GenerateCmd,
		Short: "Generates YAML manifests",
		Long:  "Generates YAML manifests for various Operator components",
	}

	generate.AddCommand(getGenerateOperatorCommand(flags))
	generate.AddCommand(getGenerateAdmissionCommand(flags))
	generate.AddCommand(getGenerateBackupCommand(flags))
	generate.AddCommand(getGeneratePodCommand(flags))

	// 'cao create' actually creates resources.
	create := &cobra.Command{
		Use:   CreateCmd,
		Short: "Creates Couchbase Autonomous Operator components",
		Long:  "Creates Couchbase Autonomous Operator components",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return checkAPIVersions(flags)
		},
	}

	create.AddCommand(getCreateOperatorCommand(flags))
	create.AddCommand(getCreateAdmissionCommand(flags))
	create.AddCommand(getCreateBackupCommand(flags))
	create.AddCommand(getCreatePodCommand(flags))

	// 'cao delete' actually deletes resources.
	deleteCmd := &cobra.Command{
		Use:   DeleteCmd,
		Short: "Deletes Couchbase Autonomous Operator components",
		Long:  "Deletes Couchbase Autonomous Operator components",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return checkAPIVersions(flags)
		},
	}

	deleteCmd.AddCommand(getDeleteOperatorCommand(flags))
	deleteCmd.AddCommand(getDeleteAdmissionCommand(flags))
	deleteCmd.AddCommand(getDeleteBackupCommand(flags))

	updateCmd := &cobra.Command{
		Use:   UpdateCmd,
		Short: "Updates Couchbase Autonomous Operator components",
		Long:  "Updates Couchbase Autonomous Operator components",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return checkAPIVersions(flags)
		},
	}

	updateCmd.AddCommand(getUpdateAdmissionCommand(flags))

	oGenSpec := specgen.SpecGeneratorOptions{}

	genSpecCmd := &cobra.Command{
		Use:   GenSpecCmd,
		Short: "Generates a spec file for a running Couchbase cluster",
		Long:  "Generates a spec file for a running Couchbase cluster",
		Run: func(cmd *cobra.Command, args []string) {
			generator := specgen.NewSpecGenerator(oGenSpec)

			spec, err := generator.Generate()
			if err != nil {
				fmt.Println("ERROR: ", err)
				return
			}

			d, err := yaml.Marshal(spec)

			if err != nil {
				fmt.Println("ERROR: ", err)
				return
			}

			fmt.Println(string(d))
		},
	}

	genSpecCmd.Flags().StringVarP(&oGenSpec.Cluster, "cluster", "c", "", "The cluster hostname")
	genSpecCmd.Flags().StringVarP(&oGenSpec.Username, "username", "u", "", "Cluster admin username")
	genSpecCmd.Flags().StringVarP(&oGenSpec.Password, "password", "p", "", "Cluster admin password")

	root.AddCommand(generate)
	root.AddCommand(create)
	root.AddCommand(deleteCmd)
	root.AddCommand(updateCmd)
	root.AddCommand(genSpecCmd)
}
