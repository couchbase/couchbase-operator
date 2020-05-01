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
		fmt.Println("cbopcfg", version.WithBuildNumber())
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
