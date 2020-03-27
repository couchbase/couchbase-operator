package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/pkg/version"
)

func main() {
	printVersion := false
	flag.BoolVar(&printVersion, "v", false, "Displays the version and exits")
	flag.Parse()

	if printVersion {
		fmt.Println("cbopcfg", version.VersionWithBuildNumber())
		os.Exit(0)
	}

	c := &config.Config{}
	if err := c.ParseArgs(); err != nil {
		fmt.Println(err)
		os.Exit(1)
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
