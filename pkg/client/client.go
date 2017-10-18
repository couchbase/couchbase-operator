package client

import (
	"github.com/couchbaselabs/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/k8sutil"

	"k8s.io/client-go/rest"
)

func MustNewInCluster() versioned.Interface {
	cfg, err := k8sutil.InClusterConfig()
	if err != nil {
		panic(err)
	}
	return MustNew(cfg)
}

func MustNew(cfg *rest.Config) versioned.Interface {
	cli, err := versioned.NewForConfig(cfg)
	if err != nil {
		panic(err)
	}
	return cli
}
