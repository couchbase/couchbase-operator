/*
Copyright 2019-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package main

import (
	"fmt"
	"os"

	"github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/pkg/version"
)

func main() {

	c := &config.Config{}
	if err := c.ParseArgs(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if c.Version {
		fmt.Println("cbopcfg", version.VersionWithBuildNumber())
		os.Exit(0)
	}

	configurators := []func(*config.Config) error{
		config.DumpAdmissionYAML,
		config.DumpOperatorYAML,
		config.DumpBackupYAML,
	}

	for _, configurator := range configurators {
		if err := configurator(c); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}
}
