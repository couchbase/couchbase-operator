package e2eutil

import (
	"context"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/e2e/types"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
)

// getRESTMapping takes a runtime object (a generic interface for any Kubernetes type),
// and returns a rest mapping (basically how to generate an API call to interact with a
// resource of this type).
func getRESTMapping(k8s *types.Cluster, resource runtime.Object) (*meta.RESTMapping, error) {
	// Map from object to dynamic API mapping... "e.g. couchbase.com/v2/couchbaseclusters"
	kinds, _, err := scheme.Scheme.ObjectKinds(resource)
	if err != nil {
		return nil, err
	}

	if len(kinds) == 0 {
		return nil, fmt.Errorf("no GVK discovered for object %v, have you registered it with the scheme?", resource.GetObjectKind())
	}

	gvk := kinds[0]

	return k8s.RESTMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
}

// metaGet reads a resource from the Kubernetes API and returns it as unstructured, ostensibly,
// JSON.
func metaGet(k8s *types.Cluster, mapping *meta.RESTMapping, resource runtime.Object) (*unstructured.Unstructured, error) {
	// Up cast the resource into a meta object so we can interrogate name and namespace.
	metaResource, ok := resource.(metav1.Object)
	if !ok {
		return nil, fmt.Errorf("unable to convert from runtime to meta resource")
	}

	if mapping.Scope.Name() == meta.RESTScopeNameRoot {
		return k8s.DynamicClient.Resource(mapping.Resource).Get(context.Background(), metaResource.GetName(), metav1.GetOptions{})
	}

	return k8s.DynamicClient.Resource(mapping.Resource).Namespace(metaResource.GetNamespace()).Get(context.Background(), metaResource.GetName(), metav1.GetOptions{})
}
