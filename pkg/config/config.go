package config

import (
	"fmt"
	"os"

	"github.com/couchbase/couchbase-operator/pkg/version"

	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/spf13/cobra"
)

const (
	// versionAnnotation is used to flag who created resources e.g. us, and
	// for what version.  This gives use the ability in future to reason about
	// what needs doing to upgrade the operator...
	versionAnnotation = "config.couchbase.com/version"
)

// ParseArgs parses command line arguments into a Config struct and executes the command.
func Execute() {
	flags := genericclioptions.NewConfigFlags(true)

	// 'cbopcfg version' prints out the Operator version this binary belongs to.
	version := &cobra.Command{
		Use:   "version",
		Short: "Prints the command version",
		Long:  "Prints the command version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("cbopcfg", version.WithBuildNumber())
		},
	}

	// 'cbopcfg generate' creates YAML for various Operator deployments.
	generate := &cobra.Command{
		Use:   "generate",
		Short: "Generates YAML manifests",
		Long:  "Generates YAML manifests for various Operator components",
	}

	generate.AddCommand(getGenerateOperatorCommand(flags))
	generate.AddCommand(getGenerateAdmissionCommand(flags))
	generate.AddCommand(getGenerateBackupCommand(flags))

	// 'cbopcfg create' actually creates resources.
	create := &cobra.Command{
		Use:   "create",
		Short: "Creates Couchbase Autonomous Operator components",
		Long:  "Creates Couchbase Autonomous Operator components",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return checkAPIVersions(flags)
		},
	}

	create.AddCommand(getCreateOperatorCommand(flags))
	create.AddCommand(getCreateAdmissionCommand(flags))
	create.AddCommand(getCreateBackupCommand(flags))

	// 'cbopcfg create' actually deletes resources.
	deleteCmd := &cobra.Command{
		Use:   "delete",
		Short: "Deletes Couchbase Autonomous Operator components",
		Long:  "Deletes Couchbase Autonomous Operator components",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return checkAPIVersions(flags)
		},
	}

	deleteCmd.AddCommand(getDeleteOperatorCommand(flags))
	deleteCmd.AddCommand(getDeleteAdmissionCommand(flags))
	deleteCmd.AddCommand(getDeleteBackupCommand(flags))

	// 'cbopcfg' is the top level command.
	root := &cobra.Command{
		Use: "cbopcfg",
	}

	flags.AddFlags(root.PersistentFlags())

	root.AddCommand(version)
	root.AddCommand(generate)
	root.AddCommand(create)
	root.AddCommand(deleteCmd)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
