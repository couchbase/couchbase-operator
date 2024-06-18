package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/validations"
)

const filenameV = "validator-config.generated.md"

func MainV() error {
	validators := validations.RegisterValidators()
	keys := make([]string, 0, len(validators))

	for k := range validators {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	output := `
### Available Validators

Each validator can be added as part of a config of an action. The validators available are:

`

	for _, k := range keys {
		v := strings.ReplaceAll(strings.ToLower(k), " ", "-")
		output += fmt.Sprintf(" * [%s](#%s)\n", k, v)
	}

	output += "\n---\n"

	for _, k := range keys {
		v := validators[k]

		val := reflect.ValueOf(v)
		output += fmt.Sprintf("#### %s\n\n", k)

		if !val.IsValid() {
			output += string("No config found.\n\n")
			output += "\n---\n"

			continue
		}

		typ := reflect.TypeOf(v).Elem()
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

			// YAML, CAO-CLI
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

	return os.WriteFile(filepath.Join(dir, filenameV), []byte(output), os.FileMode(perm))
}
