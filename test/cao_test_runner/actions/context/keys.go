package context

type contextKey int

const (
	CAOBinaryPathKey = iota
	CRDPathKey
	OperatorVersionKey
	OperatingSystemKey
	ArchitectureKey
	NamespaceIDKey
	OperatorIDKey
	AdmissionIDKey
	ClusterSpecPathIDKey
	BucketsSpecPathIDKey
	CouchbaseClusterNameKey
	K8sContextKey
	ClusterNameKey
	EKSRegionKey
	AKSRegionKey
	GKERegionKey
	KubeConfigPathKey
	CBBackupRestoreKey
	PortForwardKey
)

var (
	contextUnmarshalKeys = map[string]contextKey{
		"CAOBinaryPath":            CAOBinaryPathKey,
		"CRDPath":                  CRDPathKey,
		"OperatorVersion":          OperatorVersionKey,
		"OperatingSystem":          OperatingSystemKey,
		"Architecture":             ArchitectureKey,
		"Namespace":                NamespaceIDKey,
		"OperatorImage":            OperatorIDKey,
		"AdmissionControllerImage": AdmissionIDKey,
		"ClusterName":              ClusterNameKey,
		"ClusterSpecPath":          ClusterSpecPathIDKey,
		"BucketsSpecPath":          BucketsSpecPathIDKey,
		"CouchbaseClusterName":     CouchbaseClusterNameKey,
		"K8sContext":               K8sContextKey,
		"EKSRegion":                EKSRegionKey,
		"AKSRegion":                AKSRegionKey,
		"GKERegion":                GKERegionKey,
		"KubeConfigPath":           KubeConfigPathKey,
		"CBBackupRestore":          CBBackupRestoreKey,
		"PortForward":              PortForwardKey,
	}
)
