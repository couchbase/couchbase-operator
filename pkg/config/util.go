/*
Copyright 2019-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

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
