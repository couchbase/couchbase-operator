/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package v1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
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
