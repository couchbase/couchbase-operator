/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package main

import (
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strings"

	cbopcfg "github.com/couchbase/couchbase-operator/pkg/config"
	cbopinfo "github.com/couchbase/couchbase-operator/pkg/info/command"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func write(file io.StringWriter, format string, args ...interface{}) {
	line := fmt.Sprintf(format+"\n", args...)

	if _, err := file.WriteString(line); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

type documenter struct {
	name  string
	cobra *cobra.Command
}

func normalize(s string) string {
	lines := []string{}

	for _, line := range strings.Split(s, "\n") {
		lines = append(lines, strings.TrimSpace(line))
	}

	return strings.Join(lines, "\n")
}

func generateDocumentation(file io.StringWriter, command *cobra.Command) {
	write(file, "== %s", command.UseLine())
	write(file, "")

	write(file, normalize(command.Long))
	write(file, "")

	if command.HasExample() {
		write(file, "=== Examples")
		write(file, "")

		write(file, "[source,console]")
		write(file, "----")
		write(file, normalize(command.Example))
		write(file, "----")
		write(file, "")
	}

	if command.Runnable() {
		flagVisitor := func(flag *pflag.Flag) {
			options := []string{}

			if flag.Name != "" {
				options = append(options, fmt.Sprintf("--%s", flag.Name))
			}

			if flag.Shorthand != "" {
				options = append(options, fmt.Sprintf("-%s", flag.Shorthand))
			}

			write(file, "%s::", strings.Join(options, ", "))
			write(file, "*Type*: %s", flag.Value.Type())
			write(file, "+")

			if flag.DefValue != "" {
				// Need to get the value of the HOME variable for MacOS
				homeValue, _ := os.UserHomeDir()
				if homeValue == "" {
					homeValue = "/home"
				}
				homeValue = path.Clean(homeValue)

				r := regexp.MustCompile("^" + homeValue)
				def := r.ReplaceAllLiteralString(flag.DefValue, "$HOME")

				write(file, "*Default*: %s", def)
				write(file, "+")
			}

			write(file, normalize(flag.Usage))
			write(file, "")
		}

		if command.HasFlags() {
			write(file, "=== Flags")
			write(file, "")

			command.Flags().VisitAll(flagVisitor)
		}

		if command.HasInheritedFlags() {
			write(file, "=== Inherited Flags")
			write(file, "")

			command.InheritedFlags().VisitAll(flagVisitor)
		}
	}

	if command.HasSubCommands() {
		for _, subCommand := range command.Commands() {
			generateDocumentation(file, subCommand)
		}
	}
}

func main() {
	documents := []documenter{
		{
			name:  "cbopcfg",
			cobra: cbopcfg.GenerateCommand(),
		},
		{
			name:  "cbopinfo",
			cobra: cbopinfo.GenerateCommand(),
		},
	}

	for _, document := range documents {
		file, err := os.OpenFile("docs/user/modules/ROOT/pages/tools/"+document.name+".adoc", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		write(file, "// THIS FILE IS AUTO-GENERATED - DO NOT EDIT")
		write(file, "= %s", document.name)
		write(file, "include::partial$autogen-tools-aliases.adoc[tags=%s]", document.name)
		write(file, "")

		write(file, "include::partial$autogen-tools-install.adoc[tags=%s]", document.name)
		write(file, "")

		generateDocumentation(file, document.cobra)

		file.Close()
	}
}
