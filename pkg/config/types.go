/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package config

// scopeType describes the scope of an operation.
type scopeType string

const (
	// scopeCluster creates resources at the cluster scope.
	scopeCluster scopeType = "cluster"

	// scopeNamespace creates resources at the namespace scope.
	scopeNamespace scopeType = "namespace"
)

// isClusterScope indicates whether to create resources at the cluster scope.
func (t scopeType) isClusterScope() bool {
	return t == scopeCluster
}

// isNamespaceScope indicates whether to create resources at the namespace scope.
func (t scopeType) isNamespaceScope() bool {
	return t == scopeNamespace
}
