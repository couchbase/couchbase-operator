package context

type contextKey int

const (
	CAOBinaryPathKey = iota
	OperatorVersionKey
	PlatformKey
	OperatingSystemKey
	ArchitectureKey
	NamespaceIDKey
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
		"CAOBinaryPath":            CAOBinaryPathKey,
		"OperatorVersion":          OperatorVersionKey,
		"Platform":                 PlatformKey,
		"OperatingSystem":          OperatingSystemKey,
		"Architecture":             ArchitectureKey,
		"Namespace":                NamespaceIDKey,
		"OperatorImage":            OperatorIDKey,
		"AdmissionControllerImage": AdmissionIDKey,
		"ClusterSpecPath":          ClusterSpecPathIDKey,
		"BucketsSpecPath":          BucketsSpecPathIDKey,
		"CouchbaseClusterName":     CouchbaseClusterNameKey,
		"K8sNodesMap":              K8sNodesMapKey,
		"K8sPodsMap":               K8sPodsMapKey,
		"K8sContext":               K8sContextKey,
	}
)
