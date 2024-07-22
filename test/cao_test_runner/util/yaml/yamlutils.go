package yaml

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ghodss/yaml"
)

var (
	ErrPathNotFound = errors.New("path not found")
)

// UnmarshalYAMLFile unmarshal the yaml file into map[string]interface{}.
func UnmarshalYAMLFile(pathToYAMLFile string) (map[string]interface{}, error) {
	yamlFile, err := os.ReadFile(pathToYAMLFile)
	if err != nil {
		return nil, fmt.Errorf("unmarshal yaml file: %w", err)
	}

	unmarshalledYAML := make(map[string]interface{})

	err = yaml.Unmarshal(yamlFile, &unmarshalledYAML)
	if err != nil {
		return nil, fmt.Errorf("unmarshal yaml file: %w", err)
	}

	return unmarshalledYAML, nil
}

// MarshalYAMLIntoFile marshals the map[string]interface{} (yaml) into a yaml file and saves it.
func MarshalYAMLIntoFile(pathToYAMLFile string, unmarshalledYAML map[string]interface{}) error {
	// TODO have mutex to synchronize the writing to same file
	marshalYAML, err := yaml.Marshal(unmarshalledYAML)
	if err != nil {
		return fmt.Errorf("marshal yaml into file: %w", err)
	}

	err = os.WriteFile(pathToYAMLFile, marshalYAML, os.ModePerm)
	if err != nil {
		return fmt.Errorf("marshal yaml into file: %w", err)
	}

	return nil
}

// UpdateMapStringInterface updates the value at the specified path in the nested map[string]interface{}.
/*
 * Can be used to update YAMLs which have been unmarshalled into map[string]interface{}.
 * Supports replacing elements of sequence / list in the YAMLs.
 * E.g. of path: /spec/servers/0/size.
 */
func UpdateMapStringInterface(data map[string]interface{}, path string, newValue interface{}) error {
	// Split the path into keys
	keys := strings.Split(strings.Trim(path, "/"), "/")

	// Traverse the map to the specified path
	current := data

	for i, key := range keys {
		if i == len(keys)-1 {
			current[key] = newValue
		} else {
			// Navigate to the next level in the map
			if next, ok := current[key].(map[string]interface{}); ok {
				current = next
			} else if next, ok := current[key].([]interface{}); ok {
				// If we encounter a sequence / list
				idx, _ := strconv.Atoi(keys[i+1])
				if tempMap, ok := next[idx].(map[string]interface{}); ok {
					if i+2 < len(keys) {
						err := UpdateMapStringInterface(tempMap, strings.Join(keys[i+2:], "/"), newValue)
						if err != nil {
							return err
						}
					} else {
						return fmt.Errorf("update map string interface: path %s: %w", "/"+strings.Join(keys, "/"), ErrPathNotFound)
					}
				} else if _, ok := next[idx].(interface{}); ok {
					next[idx] = newValue
				}

				return nil
			} else {
				return fmt.Errorf("update map string interface: path %s: %w", "/"+strings.Join(keys, "/"), ErrPathNotFound)
			}
		}
	}

	return nil
}
