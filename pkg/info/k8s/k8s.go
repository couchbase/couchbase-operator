package k8s

import (
	"github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
)

// InitContext performs Kubernetes specific initialization operations on
// a context
func InitContext(context *context.Context) error {
	// Add our CRD to the global scheme
	if err := v1.AddToScheme(scheme.Scheme); err != nil {
		return err
	}

	// Load the Kubernetes configuration, expanding the file path as the
	// Kubernetes client is pretty bad at handling typical strings, then
	// silently fails
	kubeconfigPath, err := util.AbsolutePath(context.Config.KubeConfig)
	if err != nil {
		return err
	}

	// Load the configuration and return a new client
	context.KubeConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return err
	}

	// Create Kubernetes clients
	context.KubeClient, err = kubernetes.NewForConfig(context.KubeConfig)
	if err != nil {
		return err
	}
	context.KubeExtClient, err = clientset.NewForConfig(context.KubeConfig)
	if err != nil {
		return err
	}

	// Create a Couchbase client
	context.CouchbaseClusterClient, err = versioned.NewForConfig(context.KubeConfig)
	if err != nil {
		return err
	}

	return nil
}
