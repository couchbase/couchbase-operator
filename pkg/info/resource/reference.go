/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package resource

type resourceReferenceImpl struct {
	kind string
	name string
}

func newResourceReference(kind, name string) ResourceReference {
	return &resourceReferenceImpl{
		kind: kind,
		name: name,
	}
}

func (r resourceReferenceImpl) Kind() string {
	return r.kind
}

func (r resourceReferenceImpl) Name() string {
	return r.name
}
