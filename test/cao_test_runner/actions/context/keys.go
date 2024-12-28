package context

type contextKey int

const (
	CAOBinaryPathKey = iota
	CRDPathKey
	EnvironmentKey
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
	K8sContextKey
	ClusterNameKey
	ProviderKey
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
		"Environment":              EnvironmentKey,
		"OperatorVersion":          OperatorVersionKey,
		"Platform":                 PlatformKey,
		"Provider":                 ProviderKey,
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
