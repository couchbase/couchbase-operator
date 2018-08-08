package v1

import (
	sdkK8sutil "github.com/coreos/operator-sdk/pkg/util/k8sutil"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	CRDResourceKind   = "CouchbaseCluster"
	CRDResourcePlural = "couchbaseclusters"
	groupName         = "couchbase.com"
)

var (
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)
	AddToScheme   = SchemeBuilder.AddToScheme

	SchemeGroupVersion = schema.GroupVersion{Group: groupName, Version: "v1"}
	CRDName            = CRDResourcePlural + "." + groupName
)

func init() {
	sdkK8sutil.AddToSDKScheme(AddToScheme)
}

// addKnownTypes adds the set of types defined in this package to the supplied scheme.
func addKnownTypes(s *runtime.Scheme) error {
	s.AddKnownTypes(SchemeGroupVersion,
		&CouchbaseCluster{},
		&CouchbaseClusterList{},
	)
	metav1.AddToGroupVersion(s, SchemeGroupVersion)
	return nil
}

func Resource(resource string) schema.GroupResource {
	return schema.GroupResource{
		Group:    groupName,
		Resource: CRDResourceKind,
	}
}
