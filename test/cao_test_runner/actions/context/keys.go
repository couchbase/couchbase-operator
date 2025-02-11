package context

type contextKey int

const (
	NamespaceIDKey = iota
	OperatorIDKey
	ClusterSpecPathIDKey
	BucketsSpecPathIDKey
	CouchbaseClusterNameKey
	K8sContextKey
	ClusterNameKey
	EKSRegionKey
	AKSRegionKey
	GKERegionKey
	CBBackupRestoreKey
	PortForwardKey
)

var (
	contextUnmarshalKeys = map[string]contextKey{
		"Namespace":            NamespaceIDKey,
		"ClusterName":          ClusterNameKey,
		"ClusterSpecPath":      ClusterSpecPathIDKey,
		"BucketsSpecPath":      BucketsSpecPathIDKey,
		"CouchbaseClusterName": CouchbaseClusterNameKey,
		"K8sContext":           K8sContextKey,
		"EKSRegion":            EKSRegionKey,
		"AKSRegion":            AKSRegionKey,
		"GKERegion":            GKERegionKey,
		"CBBackupRestore":      CBBackupRestoreKey,
		"PortForward":          PortForwardKey,
	}
)
