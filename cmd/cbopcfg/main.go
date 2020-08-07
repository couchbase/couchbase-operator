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

	var configurator func(*config.Config) error

	switch c.Component {
	case config.ComponentOperator:
		configurator = config.DumpOperatorYAML
	case config.ComponentAdmission:
		configurator = config.DumpAdmissionYAML
	case config.ComponentBackup:
		configurator = config.DumpBackupYAML
	}

	if err := configurator(c); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
