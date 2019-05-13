package v1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/runtime/scheme"
)

const (
	CRDResourceKind   = "CouchbaseCluster"
	CRDResourcePlural = "couchbaseclusters"
	GroupName         = "couchbase.com"
	CRDName           = CRDResourcePlural + "." + GroupName
)

var (
	SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: "v1"}

	SchemeBuilder = &scheme.Builder{GroupVersion: SchemeGroupVersion}

	AddToScheme = SchemeBuilder.AddToScheme
)

func init() {
	SchemeBuilder.Register(&CouchbaseCluster{}, &CouchbaseClusterList{})
}

func Resource(resource string) schema.GroupResource {
	return schema.GroupResource{
		Group:    GroupName,
		Resource: CRDResourceKind,
	}
}
