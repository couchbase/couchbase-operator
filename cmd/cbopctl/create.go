package main

import (
	"fmt"
	"os"

	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/validator"

	"k8s.io/client-go/tools/clientcmd"
)

type CreateContext struct {
	filename   string
	kubeconfig string
	dryRun     bool
}

func (ctx *CreateContext) Run() {
	resource, err := decodeCouchbaseCluster(ctx.filename)
	if err != nil {
		fmt.Printf("Error decoding specification: %v\n", err)
		os.Exit(1)
	}

	err = validator.Create(resource)
	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}

	if ctx.dryRun {
		return
	}

	if resource.Namespace == "" {
		resource.Namespace = "default"
	}

	config, err := clientcmd.BuildConfigFromFlags("", ctx.kubeconfig)
	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}

	_, err = k8sutil.CreateCouchbaseCluster(client.MustNew(config), resource)
	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}
}
