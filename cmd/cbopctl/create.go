package main

import (
	"fmt"
	"os"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/client"
	cberrors "github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/validator"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

type CreateContext struct {
	filename      string
	kubeconfig    string
	dryRun        bool
	verifyVersion bool
}

func (ctx *CreateContext) Run() {
	resource, err := decodeCouchbaseCluster(ctx.filename)
	if err != nil {
		fmt.Printf("Error decoding specification: %v\n", err)
		os.Exit(1)
	}

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: ctx.kubeconfig},
		&clientcmd.ConfigOverrides{ClusterInfo: clientcmdapi.Cluster{Server: ""}})

	config, err := clientConfig.ClientConfig()
	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}

	// when verify version is disabled then set it to min
	// to ensure it can never cause validation to fail
	if !ctx.verifyVersion {
		resource.Spec.Version = constants.CouchbaseVersionMin
	}

	v := validator.New(kubeClient)
	if err = v.Create(resource); err != nil {
		// when verify version is disabled then ignore this error
		_, versionErr := err.(cberrors.ErrUnsupportedVersion)
		if !versionErr || ctx.verifyVersion {
			fmt.Printf("%v\n", err)
			os.Exit(1)
		}
	}

	if ctx.dryRun {
		return
	}

	if resource.Namespace == "" {
		resource.Namespace, _, _ = clientConfig.Namespace()
	}

	_, err = k8sutil.CreateCouchbaseCluster(client.MustNew(config), resource)
	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s \"%s\" created\n", api.CRDResourcePlural, resource.Name)
}
