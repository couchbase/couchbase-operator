/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package resource

type referenceImpl struct {
	kind string
	name string
}

func newReference(kind, name string) Reference {
	return &referenceImpl{
		kind: kind,
		name: name,
	}
}

func (r referenceImpl) Kind() string {
	return r.kind
}

func (r referenceImpl) Name() string {
	return r.name
}
