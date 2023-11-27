package main

import (
	"os"

	"github.com/couchbase/couchbase-operator/pkg/info/command"

	_ "k8s.io/client-go/plugin/pkg/client/auth/azure"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
)

// main is the entry point of this application.
func main() {
	c := command.GenerateCommand()

	if err := c.Execute(); err != nil {
		os.Exit(1)
	}
}
