package meta

const (
	metaVersion = "v1"
)

// OperatorMetadata tells you all you need to know about the operator.
type OperatorMetadata struct {
	// Image records the operator image and version, if we were able
	// to derive it.  This tells us how in-date the user is...
	Image string `json:"image,omitempty"`

	// LogPath is the relative path to the operator log file.
	LogPath string `json:"logPath,omitempty"`
}

// ClusterMetadata tells you all you need to know about Couchbase clusters.
type ClusterMetadata struct {
	// Name is the cluster name.
	Name string `json:"name,omitempty"`

	// Image records the couchbase service image and version.  You can
	// cross reference this with the operator image and see how supported people
	// are...
	Image string `json:"image,omitempty"`

	// ResourcePath is the relative path to the cluster configuration YAML.
	// Usually the first port of call to detect whack configurations.
	// You can do cool things like classify what features are in use, and what
	// causes the most customer issues.  Also important are cluster conditions.
	ResourcePath string `json:"resourcePath,omitempty"`

	// EventsPath is a relative path to the cluster events.  These are given as
	// an unordered list of events.  Once you unmarshal the YAML you can sort
	// them with `metadata.creationTimestamp`.  This tells you a high level
	// overview of "what's occurring".
	EventsPath string `json:"eventsPath,omitempty"`
}

// LogsMetadata records metadata about the logs, where they came from
// where the pertinent logs are, etc. etc.
type LogsMetadata struct {
	// Version is the version of this struct.  We expect a client to
	// do a generic unmarshal of the JSON, extract the version and
	// make decisions based on that.
	Version string `json:"version"`

	// PlatformVersion records the version of Kubernetes the user
	// is running.  Will contain at least a semver (\d+\.\d+\.\d+).
	PlatformVersion string `json:"platformVersion,omitempty"`

	// Namespace records the namespace the tool was run in (or project if
	// you use a strange Kubernetes type).
	Namespace string `json:"namespace,omitempty"`

	// ToolVersion records the version of this tool used to collect
	// the logs.  The Operator version semver.
	ToolVersion string `json:"toolVersion,omitempty"`

	// CommandLine records the command line used to invoke the tool.
	CommandLine []string `json:"commandLine,omitempty"`

	// Operator optionally contains information about the operator.
	Operator *OperatorMetadata `json:"operator,omitempty"`

	// Clusters optionall contains information about any Couchbase clusters.
	Clusters []ClusterMetadata `json:"clusters,omitempty"`
}
