package main

import (
	"github.com/couchbase/couchbase-operator/pkg/config"

	_ "k8s.io/client-go/plugin/pkg/client/auth/azure"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
)

func main() {
	config.Execute()
}
