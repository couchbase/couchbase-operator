/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

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

func GenerateCommand() *cobra.Command {
	c := config.Configuration{
		ConfigFlags: genericclioptions.NewConfigFlags(true),
	}

	version := &cobra.Command{
		Use:   "version",
		Short: "Prints the command version",
		Long:  "Prints the command version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("cbopinfo", version.WithBuildNumber())
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
			collect(c)
		},
	}

	root.AddCommand(version)

	c.AddFlags(root.Flags())

	return root
}
