package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/task"
)

var (
	perm = 0o600
)

const filename = "action-config.generated.md"

type Config struct {
	OutputDir string `env:"OUTPUT_DIR" usage:"Directory to output generated .go files" default:"."`
}

func (c *Config) Validate() error {
	var err error
	c.OutputDir, err = filepath.Abs(c.OutputDir)

	if err != nil {
		return fmt.Errorf("failed to determine absolute path for --output-dir %s: %w", c.OutputDir, err)
	}

	return nil
}

func MainA() error {
	r := task.Register{}

	actions := r.Actions()
	keys := make([]string, 0, len(actions))

	for k := range actions {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	output := `
### Available Actions

Each action can be executed as part of a scenario. All actions inherit the
[Global flags](README.md#global-flags) as common configuration, additional
configuration is also available on a per-action basis:

`

	for _, k := range keys {
		a := strings.ReplaceAll(strings.ToLower(k), " ", "-")
		output += fmt.Sprintf(" * [%s](#%s)\n", k, a)
	}

	output += "\n---\n"

	for _, k := range keys {
		a := actions[k]

		val := reflect.ValueOf(a.Config())
		output += fmt.Sprintf("#### %s\n\n", k)

		if !val.IsValid() {
			output += string("No config found.\n\n")
			output += "\n---\n"

			continue
		}

		typ := reflect.TypeOf(a.Config()).Elem()
		output += fmt.Sprintf("Config symbol: `%s`\n\n", typ.Name())

		n := val.Elem().NumField()
		if n == 0 {
			output += string("No fields found on struct.\n")

			continue
		}

		output += "| Name | Type | YAML Tag | CAO-CLI Tag |\n"
		output += "| ---- | ---- | -------- | ----------- |\n"

		for i := 0; i < val.Elem().NumField(); i++ {
			f := val.Elem().Type().Field(i)

			// Name
			output += "| `" + f.Name + "` "

			// Type
			n := f.Type.Name()
			k := f.Type.Kind().String()

			if n == k {
				output += "| `" + n + "` "
			} else {
				output += "| `" + k + "` "
			}

			// YAML, CP-CLI
			for _, tagName := range []string{"yaml", "caoCli"} {
				if tagContents, ok := f.Tag.Lookup(tagName); ok {
					output += "| `" + tagName + ":" + tagContents + "` "
					continue
				}

				output += "| "
			}
			// Close last table column
			output += " |\n"
		}

		output += "\n---\n"
	}

	// dir, _, err := shell.RunWithOutputCapture("git", "rev-parse --show-toplevel")
	// if err != nil {
	// 	return fmt.Errorf("failed to determine top-level directory: %w", err)
	// }

	dir, _ := os.Getwd()

	return os.WriteFile(filepath.Join(dir, filename), []byte(output), os.FileMode(perm))
}
