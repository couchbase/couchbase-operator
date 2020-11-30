package resource

import (
	ctx "context"

	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// persistentVolumeResource represents a collection of persistentVolumes.
type persistentVolumeResource struct {
	context *context.Context
	// persistentVolumes is the raw output from listing persistentVolumes
	persistentVolumes *v1.PersistentVolumeList
}

// NewPersistentVolumeResource initializes a new persistentVolume resource.
func NewPersistentVolumeResource(context *context.Context) Resource {
	return &persistentVolumeResource{
		context: context,
	}
}

func (r *persistentVolumeResource) Kind() string {
	return "PersistentVolume"
}

// Fetch collects all persistentVolumes as defined by the configuration.
func (r *persistentVolumeResource) Fetch() error {
	var err error

	r.persistentVolumes, err = r.context.KubeClient.CoreV1().PersistentVolumes().List(ctx.Background(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	return nil
}

func (r *persistentVolumeResource) Write(b backend.Backend) error {
	for _, persistentVolume := range r.persistentVolumes.Items {
		data, err := yaml.Marshal(persistentVolume)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePathUnscoped(r.Kind(), persistentVolume.Name, persistentVolume.Name+".yaml"), string(data))
	}

	return nil
}

func (r *persistentVolumeResource) References() []Reference {
	return []Reference{}
}
