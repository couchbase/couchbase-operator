package main

import (
	"fmt"
	"os"

	"github.com/couchbase/couchbase-operator/pkg/config"
)

func main() {
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
