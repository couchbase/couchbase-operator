package main

import (
	"fmt"
	"io"
	"os"
	"path"
	"reflect"
	"regexp"
	"sort"
	"strings"

	cao "github.com/couchbase/couchbase-operator/pkg/certification"
	cbopcfg "github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/pkg/info/collector"
	cbopinfo "github.com/couchbase/couchbase-operator/pkg/info/command"
	"github.com/couchbase/couchbase-operator/pkg/info/resource"

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

var logLevelMap = map[resource.LogLevel]string{
	0: "Required",
	1: "Sensitive",
}

func generateCollectedResourceDocumentation(file io.StringWriter) {
	write(file, "=== Collected Resources")
	write(file, "Collected resources are categorised based on log level and scope.")
	write(file, "")
	write(file, "Log level::")
	write(file, "Required: Couchbase resources and those scoped to the cluster.")
	write(file, "+")
	write(file, "Sensitive: may include secrets, roles, etc")
	write(file, "")
	write(file, "Scope::")
	write(file, "all: All resources found")
	write(file, "+")
	write(file, "cluster: All resources associated with a cluster")
	write(file, "+")
	write(file, "name: All resources limited by cluster names")
	write(file, "+")
	write(file, "namespace: All resources limited by namespace name")
	write(file, "+")
	write(file, "group: All resources limited by resource name")
	write(file, "+")
	write(file, "operator: Only the Operator deployment")
	write(file, "")
	resources := collector.Resources

	sort.Slice(resources, func(i, j int) bool {
		if resources[i].LogLevel == resources[j].LogLevel {
			return resources[i].Scope < resources[j].Scope
		}
		return resources[i].LogLevel < resources[j].LogLevel
	})

	if len(resources) > 0 {
		write(file, "==== Log Level - %s", logLevelMap[resources[0].LogLevel])
	}

	for i, resource := range resources {
		if i > 0 && resource.LogLevel != resources[i-1].LogLevel {
			write(file, "")
			write(file, "==== Log Level - %s", logLevelMap[resources[i].LogLevel])
		}
		writeResourceDocumentation(file, &resource)
	}
	write(file, "")
}

func writeResourceDocumentation(file io.StringWriter, resource *resource.Collector) {
	write(file, "%s::", reflect.TypeOf(resource.Resource).Elem().Name())
	write(file, "Log Level: %s", logLevelMap[resource.LogLevel])
	write(file, "+")
	write(file, "Scope: %s", resource.Scope)

	if resource.Reason != "" {
		write(file, "+")
		write(file, "Reason: %s", resource.Reason)
	}
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

	if command.Use == "collect-logs" {
		generateCollectedResourceDocumentation(file)
	}

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
			if flag.Hidden {
				return
			}

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
			name:  "cao",
			cobra: cao.GenerateCommand(),
		},
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
