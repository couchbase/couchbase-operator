/*
Copyright 2020-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package certification

import (
	"fmt"
	"os"
	"strings"

	"github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/pkg/info/command"
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
	flags := genericclioptions.NewConfigFlags(true)

	version := &cobra.Command{
		Use:   "version",
		Short: "Prints the command version",
		Long:  "Prints the command version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("cao", version.WithBuildNumber())
		},
	}

	root := &cobra.Command{
		Use:   "cao",
		Short: "Couchbase Autonomous Operator Utility Tool",
		Long:  "Couchbase Autonomous Operator Utility Tool",
	}

	flags.AddFlags(root.PersistentFlags())

	root.AddCommand(version)
	root.AddCommand(getCertifyCommand(flags))

	config.ApplySubCommands(root, flags)
	command.ApplySubCommands(root, flags)

	return root
}

func Run() {
	cmd := GenerateCommand()

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
