package main

import (
	"fmt"
	"os"

	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/validator"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
)

type ApplyContext struct {
	filename   string
	kubeconfig string
	dryRun     bool
}

func (ctx *ApplyContext) Run() {
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

	crClient := client.MustNew(config)
	current, err := crClient.CouchbaseV1beta1().CouchbaseClusters(resource.Namespace).Get(resource.Name, metav1.GetOptions{})
	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}

	errs, warnings := validator.Update(current, resource)
	if warnings != nil {
		for _, warning := range warnings {
			fmt.Printf("%s\n", warning.String())
		}
	}

	if errs != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}

	if ctx.dryRun {
		return
	}

	_, err = k8sutil.UpdateCouchbaseCluster(crClient, resource)
	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}
}
