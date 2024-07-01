package context

type contextKey int

const (
	NamespaceIDKey = iota
	OperatorIDKey
	AdmissionIDKey
	CouchbaseSpecPathIDKey
	CouchbaseClusterNameKey
	K8sNodesMapKey
	K8sPodsMapKey
)

var (
	contextUnmarshalKeys = map[string]contextKey{
		"Namespace":            NamespaceIDKey,
		"OperatorImage":        OperatorIDKey,
		"AdmissionImage":       AdmissionIDKey,
		"CouchbaseSpecPath":    CouchbaseSpecPathIDKey,
		"CouchbaseClusterName": CouchbaseClusterNameKey,
		"K8sNodesMap":          K8sNodesMapKey,
		"K8sPodsMap":           K8sPodsMapKey,
	}
)
