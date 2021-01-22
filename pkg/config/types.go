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
