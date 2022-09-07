package meta

import (
	"encoding/json"

	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/version"
)

var logsMeta LogsMetadata

// Init fills in as much useful platform information as it can get.
func Init(ctx *context.Context, args []string) error {
	v, err := ctx.KubeClient.Discovery().ServerVersion()
	if err != nil {
		return err
	}

	logsMeta.Version = metaVersion
	logsMeta.ToolVersion = version.WithBuildNumber()
	logsMeta.PlatformVersion = v.String()
	logsMeta.Namespace = ctx.Namespace()
	logsMeta.CommandLine = args

	return nil
}

// SetOperator is called when we encounter the operator for the firsrt time, cache
// metadata about the resource.  The resource will be flagged, and any optional
// fields associated with it will be filled in by individual collectors.
func SetOperator(image string) {
	logsMeta.Operator = &OperatorMetadata{
		Image: image,
	}
}

// SetOperatorLogPath is optionally called if there are any operator logs collected.
func SetOperatorLogPath(path string) {
	// Yes this can technically fault, but I'd rather know that something went
	// wrong in testing!!
	logsMeta.Operator.LogPath = path
}

// SetCluster is called when we encounter a cluster resource for the first time,
// caching metadata about that resource.
func SetCluster(name, image, path string) {
	logsMeta.Clusters = append(logsMeta.Clusters, ClusterMetadata{
		Name:         name,
		Image:        image,
		ResourcePath: path,
	})
}

// SetClusterEvents is optionally called if there are any events collected from
// a cluster resource.
func SetClusterEvents(name, path string) {
	for i := range logsMeta.Clusters {
		if logsMeta.Clusters[i].Name != name {
			continue
		}

		logsMeta.Clusters[i].EventsPath = path

		break
	}
}

// ToJSON returns the logs metadata for addition to the archive.
func ToJSON() (string, error) {
	raw, err := json.Marshal(logsMeta)
	if err != nil {
		return "", err
	}

	return string(raw), nil
}
