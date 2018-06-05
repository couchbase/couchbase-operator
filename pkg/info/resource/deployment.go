package resource

import (
	"strings"

	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	"k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// deploymentResource represents a collection of deployments
type deploymentResource struct {
	context *context.Context
	// deployments is the raw output from listing deployments
	deployments []v1.Deployment
}

// NewDeploymentResource initializes a new deployment resource
func NewDeploymentResource(context *context.Context) Resource {
	return &deploymentResource{
		context: context,
	}
}

func (r *deploymentResource) Kind() string {
	return "Deployment"
}

// Fetch collects all deployments as defined by the configuration
func (r *deploymentResource) Fetch() error {
	deployments, err := r.context.KubeClient.Apps().Deployments(r.context.Config.Namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	// The deployment is unlabelled so we need to be fairly intelligent in what we
	// collect so as not to harvest everything; go off the image name for now
	if r.context.Config.All {
		r.deployments = deployments.Items
		return nil
	}

	r.deployments = []v1.Deployment{}
	for _, deployment := range deployments.Items {
		for _, container := range deployment.Spec.Template.Spec.Containers {
			if strings.HasPrefix(container.Image, "couchbase/couchbase-operator") {
				r.deployments = append(r.deployments, deployment)
				break
			}
		}
	}

	return nil
}

func (r *deploymentResource) Write(b backend.Backend) error {
	for _, deployment := range r.deployments {
		data, err := yaml.Marshal(deployment)
		if err != nil {
			return err
		}

		b.WriteFile(util.ArchivePath(r.context.Config.Namespace, r.Kind(), deployment.Name, deployment.Name+".yaml"), string(data))
	}
	return nil
}

func (r *deploymentResource) References() []ResourceReference {
	references := []ResourceReference{}
	for _, deployment := range r.deployments {
		references = append(references, newResourceReference(r.Kind(), deployment.Name))
	}
	return references
}
