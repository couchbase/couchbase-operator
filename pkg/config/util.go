package config

import (
	"fmt"
	"os"

	"github.com/ghodss/yaml"
)

// DumpYAML takes a tagged struct and dumps it to standard out as YAML.
func DumpYAML(toFile bool, name string, object interface{}) error {
	data, err := yaml.Marshal(object)
	if err != nil {
		return err
	}

	if toFile {
		file, err := os.Create(name + ".yaml")
		if err != nil {
			return err
		}

		defer file.Close()

		if _, err := file.Write(data); err != nil {
			return err
		}

		return nil
	}

	fmt.Println("---")
	fmt.Println(string(data))

	return nil
}
