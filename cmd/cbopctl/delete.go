package main

import (
	"fmt"
	"os"

	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	"k8s.io/client-go/tools/clientcmd"
)

type DeleteContext struct {
	filename   string
	kubeconfig string
}

func (ctx *DeleteContext) Run() {
	resource, err := decodeCouchbaseCluster(ctx.filename)
	if err != nil {
		fmt.Printf("Error decoding specification: %v\n", err)
		os.Exit(1)
	}

	if resource.Namespace == "" {
		resource.Namespace = "default"
	}

	config, err := clientcmd.BuildConfigFromFlags("", ctx.kubeconfig)
	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}

	err = k8sutil.DeleteCouchbaseCluster(client.MustNew(config), resource)
	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}
}
