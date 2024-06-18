package context

type contextKey int

const (
	NamespaceIDKey = iota
	OperatorIDKey
	AdmissionIDKey
	CouchbaseSpecPathIDKey
	CouchbaseClusterNameKey
)

var (
	contextUnmarshalKeys = map[string]contextKey{
		"Namespace":            NamespaceIDKey,
		"OperatorImage":        OperatorIDKey,
		"AdmissionImage":       AdmissionIDKey,
		"CouchbaseSpecPath":    CouchbaseSpecPathIDKey,
		"CouchbaseClusterName": CouchbaseClusterNameKey,
	}
)
