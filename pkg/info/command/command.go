package command

import (
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/pkg/info/config"
	"github.com/couchbase/couchbase-operator/pkg/version"

	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// normalize takes a blob of text and prepares it for use as a long description or
// and example for a command.
func normalize(s string) string {
	s = strings.TrimSpace(s)

	formatted := []string{}

	for _, line := range strings.Split(s, "\n") {
		formatted = append(formatted, "  "+strings.TrimSpace(line))
	}

	return strings.Join(formatted, "\n")
}

// ApplySubCommands attaches the log collection sub commands to an arbitrary root command.
func ApplySubCommands(root *cobra.Command, flags *genericclioptions.ConfigFlags) {
	c := config.Configuration{
		ConfigFlags: flags,
	}

	collect := &cobra.Command{
		Use:   "collect-logs",
		Short: "Log and resource collection for Couchbase Autonomous Operator support",
		Long: normalize(`
                        Log and resource collection for Couchbase Autonomous Operator support.

                        When you encounter a problem with the Autonomous Operator, our support
                        teams require more than just the last line of the logs to diagnose and,
                        ultimately, resolve the issue quickly.

                        Log collection, in its most basic form, collects all resources associated
                        with the Autonomous Operator and Couchbase clusters in the specified
                        namespace, this includes associated logs and events.  Most resource
                        types are filtered, so the tool collects only what is necessary. Where
                        filtering is not possible, all instances of that resource are collected,
                        so it may be desirable to segregate the Autonomous Operator into its
                        own namespace.  Secrets, for example, are not filtered, but the tool
                        redacts values, so if your support request relates to TLS, you may
                        need to manually collect these resources and include them in your
                        support request.
                `),
		Example: normalize(`
                        # Collect operator and all couchbase cluster resources
                        cao collect-logs

                        # Collect operator and a named cluster's resources
                        cao collect-logs --couchbase-cluster my-cluster

                        # Collect operator resources and Couchbase Server logs
                        cao collect-logs --collectinfo --collectinfo-collect=all

                        # Collect operator and system (kube-system) resources
                        cao collect-logs --system

                        # Collect all known resources, applying no filtering
                        cao collect-logs --all

                        # Collect only required resources, filtering potentially sensitive information
                        cao collect-logs --log-level 0

                        # Collect cbbackupmgr logs from all backups
                        cao collect-logs --backup-logs

                        # Collect cbbackupmgr logs from a specific backup
                        cao collect-logs --backup-logs --backup-logs-name my-backup
                `),
		Run: func(cmd *cobra.Command, args []string) {
			collect(c)
		},
	}

	c.AddFlags(collect.Flags())

	root.AddCommand(collect)
}

func GenerateCommand() *cobra.Command {
	c := config.Configuration{
		ConfigFlags: genericclioptions.NewConfigFlags(true),
	}

	version := &cobra.Command{
		Use:   "version",
		Short: "Prints the command version",
		Long:  "Prints the command version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("cbopinfo", version.WithBuildNumberAndRevision())
		},
	}

	root := &cobra.Command{
		Use:   "cbopinfo",
		Short: "Log and resource collection for Couchbase Autonomous Operator support",
		Long: normalize(`
                        Log and resource collection for Couchbase Autonomous Operator support.

                        When you encounter a problem with the Autonomous Operator, our support
                        teams require more than just the last line of the logs to diagnose and,
                        ultimately, resolve the issue quickly.

                        cbopinfo, in its most basic form, collects all resources associated
                        with the Autonomous Operator and Couchbase clusters in the specified
                        namespace, this includes associated logs and events.  Most resource
                        types are filtered, so the tool collects only what is necessary. Where
                        filtering is not possible, all instances of that resource are collected,
                        so it may be desirable to segregate the Autonomous Operator into its
                        own namespace.  Secrets, for example, are not filtered, but the tool
                        redacts values, so if your support request relates to TLS, you may
                        need to manually collect these resources and include them in your
                        support request.
                `),
		Example: normalize(`
                        # Collect operator and all couchbase cluster resources
                        cbopinfo

                        # Collect operator and a named cluster's resources
                        cbopinfo --couchbase-cluster my-cluster

                        # Collect operator resources and Couchbase Server logs
                        cbopinfo --collectinfo --collectinfo-collect=all

                        # Collect operator and system (kube-system) resources
                        cbopinfo --system

                        # Collect all known resources, applying no filtering
                        cbopinfo --all
                `),
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("\nWARNING: This tool is deprecated and will be removed in a later release. Please use cao binary that features the same functionality: https://docs.couchbase.com/operator/current/tools/cao.html#cao-collect-logs-flags\n\n")
			collect(c)
		},
	}

	root.AddCommand(version)

	c.AddFlags(root.Flags())
	c.ConfigFlags.AddFlags(root.Flags())

	return root
}
