package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func addStringFlag(cmd *cobra.Command, name, shorthand, dflt, desc string) {
	cmd.PersistentFlags().StringP(name, shorthand, dflt, desc)
	_ = viper.BindPFlag(name, cmd.PersistentFlags().Lookup(name))
}

func addBoolFlag(cmd *cobra.Command, name string, shorthand string, dflt bool, desc string) {
	cmd.PersistentFlags().BoolP(name, shorthand, dflt, desc)
	_ = viper.BindPFlag(name, cmd.PersistentFlags().Lookup(name))
}

var rootCmd = &cobra.Command{
	Use:   "cao-test-runner",
	Short: "cao-test-runner is a test tool for Couchbase Capella",
	Long: `cao-test-runner is a test tool that can setup K8S couchbase clusters in K8S environment,
upgrade them and run health checks on the clusters`,
}

func Execute() {
	rootCmd.AddCommand(newScenarioCmd())
	rootCmd.AddCommand(generateDocumentation())
	err := rootCmd.Execute()

	if err != nil {
		os.Exit(1)
	}
}
