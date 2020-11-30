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

// persistentVolumeClaimResource represents a collection of persistentVolumeClaims.
type persistentVolumeClaimResource struct {
	context *context.Context
	// persistentVolumeClaims is the raw output from listing persistentVolumeClaims
	persistentVolumeClaims *v1.PersistentVolumeClaimList
}

// NewPersistentVolumeClaimResource initializes a new persistentVolumeClaim resource.
func NewPersistentVolumeClaimResource(context *context.Context) Resource {
	return &persistentVolumeClaimResource{
		context: context,
	}
}

func (r *persistentVolumeClaimResource) Kind() string {
	return "PersistentVolumeClaim"
}

// Fetch collects all persistentVolumeClaims as defined by the configuration.
func (r *persistentVolumeClaimResource) Fetch() error {
	selector, err := GetResourceSelector(&r.context.Config)
	if err != nil {
		return err
	}

	r.persistentVolumeClaims, err = r.context.KubeClient.CoreV1().PersistentVolumeClaims(r.context.Namespace()).List(ctx.Background(), metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return err
	}

	return nil
}

func (r *persistentVolumeClaimResource) Write(b backend.Backend) error {
	for _, persistentVolumeClaim := range r.persistentVolumeClaims.Items {
		data, err := yaml.Marshal(persistentVolumeClaim)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), persistentVolumeClaim.Name, persistentVolumeClaim.Name+".yaml"), string(data))
	}

	return nil
}

func (r *persistentVolumeClaimResource) References() []Reference {
	return []Reference{}
}
