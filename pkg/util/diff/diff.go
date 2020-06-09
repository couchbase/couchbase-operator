// package diff provides helper functions to diff arbitrary objects for the purposes
// of debug and logging.
package diff

import (
	"github.com/ghodss/yaml"
	"github.com/google/go-cmp/cmp"
)

// Diff takes a pair of objects, marshals them into YAML then generates a string
// diff of them.
func Diff(old, new interface{}) (string, error) {
	oldBytes, err := yaml.Marshal(old)
	if err != nil {
		return "", err
	}

	newBytes, err := yaml.Marshal(new)
	if err != nil {
		return "", err
	}

	return cmp.Diff(string(oldBytes), string(newBytes)), nil
}
