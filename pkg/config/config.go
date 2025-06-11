package config

import (
	"fmt"
	"os"

	"github.com/couchbase/couchbase-operator/pkg/specgen"
	"github.com/couchbase/couchbase-operator/pkg/version"
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
	// 'cbopcfg generate' creates YAML for various Operator deployments.
	generate := &cobra.Command{
		Use:   GenerateCmd,
		Short: "Generates YAML manifests",
		Long:  "Generates YAML manifests for various Operator components",
	}

	generate.AddCommand(getGenerateOperatorCommand(root.UseLine(), flags))
	generate.AddCommand(getGenerateAdmissionCommand(root.UseLine(), flags))
	generate.AddCommand(getGenerateBackupCommand(flags))
	generate.AddCommand(getGeneratePodCommand(root.UseLine(), flags))

	// 'cbopcfg create' actually creates resources.
	create := &cobra.Command{
		Use:   CreateCmd,
		Short: "Creates Couchbase Autonomous Operator components",
		Long:  "Creates Couchbase Autonomous Operator components",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return checkAPIVersions(flags)
		},
	}

	create.AddCommand(getCreateOperatorCommand(root.UseLine(), flags))
	create.AddCommand(getCreateAdmissionCommand(root.UseLine(), flags))
	create.AddCommand(getCreateBackupCommand(root.UseLine(), flags))
	create.AddCommand(getCreatePodCommand(root.UseLine(), flags))

	// 'cbopcfg create' actually deletes resources.
	deleteCmd := &cobra.Command{
		Use:   DeleteCmd,
		Short: "Deletes Couchbase Autonomous Operator components",
		Long:  "Deletes Couchbase Autonomous Operator components",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return checkAPIVersions(flags)
		},
	}

	deleteCmd.AddCommand(getDeleteOperatorCommand(root.UseLine(), flags))
	deleteCmd.AddCommand(getDeleteAdmissionCommand(root.UseLine(), flags))
	deleteCmd.AddCommand(getDeleteBackupCommand(root.UseLine(), flags))

	updateCmd := &cobra.Command{
		Use:   UpdateCmd,
		Short: "Updates Couchbase Autonomous Operator components",
		Long:  "Updates Couchbase Autonomous Operator components",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return checkAPIVersions(flags)
		},
	}

	updateCmd.AddCommand(getUpdateAdmissionCommand(root.UseLine(), flags))

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

func GenerateCommand() *cobra.Command {
	flags := genericclioptions.NewConfigFlags(true)

	// 'cbopcfg version' prints out the Operator version this binary belongs to.
	version := &cobra.Command{
		Use:   "version",
		Short: "Prints the command version",
		Long:  "Prints the command version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("cbopcfg", version.WithBuildNumberAndRevision())
		},
	}

	// 'cbopcfg' is the top level command.
	root := &cobra.Command{
		Use:   "cbopcfg",
		Short: "Couchbase Autonomous Operator configuration utility",
		Long: normalize(`
			Couchbase Autonomous Operator configuration utility.

			The cbopcfg tool is used to automate the life-cycle of the Autonomous
			Operator.  It is responsible for creation and deletion of Autonomous
			Operator components.  A typical installation involves installing the
			Couchbase custom resource definitions, then the Dynamic Admission
			Controller, and finally the Operator itself.

			Additional details for each component are documented under each
			sub-command.

			Alternative methods of life-cycle management are available in the form
			of Helm charts and the Couchbase Open Service Broker. 
		`),
	}

	flags.AddFlags(root.PersistentFlags())

	root.AddCommand(version)

	ApplySubCommands(root, flags)

	return root
}

func Execute() {
	root := GenerateCommand()

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
