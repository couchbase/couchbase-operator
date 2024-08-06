package context

type contextKey int

const (
	NamespaceIDKey = iota
	OperatorIDKey
	AdmissionIDKey
	ClusterSpecPathIDKey
	BucketsSpecPathIDKey
	CouchbaseClusterNameKey
	K8sNodesMapKey
	K8sPodsMapKey
	K8sContextKey
)

var (
	contextUnmarshalKeys = map[string]contextKey{
		"Namespace":            NamespaceIDKey,
		"OperatorImage":        OperatorIDKey,
		"AdmissionImage":       AdmissionIDKey,
		"ClusterSpecPath":      ClusterSpecPathIDKey,
		"BucketsSpecPath":      BucketsSpecPathIDKey,
		"CouchbaseClusterName": CouchbaseClusterNameKey,
		"K8sNodesMap":          K8sNodesMapKey,
		"K8sPodsMap":           K8sPodsMapKey,
		"K8sContext":           K8sContextKey,
	}
)
