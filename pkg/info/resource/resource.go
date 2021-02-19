package resource

import (
	ctx "context"
	"fmt"
	"strings"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/config"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/k8s"
	log_meta "github.com/couchbase/couchbase-operator/pkg/info/meta"
	"github.com/couchbase/couchbase-operator/pkg/info/util"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"

	"github.com/ghodss/yaml"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes/scheme"
)

// Reference contains data so other modules can extract data associated
// with discovered associated with resource instances.
type Reference interface {
	// Kind is the Kubernetes kind of a resource
	Kind() string
	// Name is the name of the resource
	Name() string
	// IsOperator is a flag to say this resource is the operator deployment.
	IsOperator() bool
	// SetIsOperator set this as the operator deployment.
	SetIsOperator()
}

// getResourceSelector returns a label selector which will scope the resources we
// can collect in the requested namespace based on configuration directives.
func getResourceSelector(c *config.Configuration, all bool) (labels.Selector, error) {
	// Collect everything we can
	if all {
		return labels.Everything(), nil
	}

	// Collect requirements for the label selector
	requirements := []labels.Requirement{}

	// By default we only collect items labeled as couchbase
	req, err := labels.NewRequirement(constants.LabelApp, selection.Equals, []string{constants.App})
	if err != nil {
		return nil, err
	}

	requirements = append(requirements, *req)

	// If we specify specific clusters add this requirement
	if len(c.Clusters) != 0 {
		req, err := labels.NewRequirement(constants.LabelCluster, selection.In, c.Clusters)
		if err != nil {
			return nil, err
		}

		requirements = append(requirements, *req)
	}

	// Create and return the selector
	selector := labels.NewSelector()

	return selector.Add(requirements...), nil
}

// GetResourceSelector returns a label selector which will scope the resources we
// can collect in the requested namespace based on configuration directives.
func GetResourceSelector(c *config.Configuration) (labels.Selector, error) {
	return getResourceSelector(c, c.All)
}

// GetResourceSelectorForCluster returns a label selector which will scope the resources we
// can collect in the requested namespace based on configuration directives.  Explicitly
// limits scope to the cluster and ignores the --all flag.
func GetResourceSelectorForCluster(c *config.Configuration) (labels.Selector, error) {
	return getResourceSelector(c, false)
}

// Scope defines how aggressive we are with collection.
type Scope string

const (
	// ScopeAll collects all resources found.
	ScopeAll Scope = "all"

	// ScopeCluster collects all resources associated with a cluster.
	ScopeCluster Scope = "cluster"

	// ScopeClusterName collects all, but filters on the cluster names in the configuration.
	ScopeClusterName Scope = "name"

	// ScopeNamespace collects all, but filters on the namespace name in the configuration.
	ScopeNamespace Scope = "namespace"

	// ScopeCouchbaseGroup collects all, but filters on the resource name.
	ScopeCouchbaseGroup Scope = "group"

	// ScopeOperatorDeployment collects only the Operator deployment based on image name.
	ScopeOperatorDeployment Scope = "operator"
)

// Collector defines types to collect and how to collect them.
type Collector struct {
	// Resource is the object type.  We will use reflection to infer API
	// accesses.
	Resource runtime.Object

	// Scope is the scope of a collection.
	Scope Scope
}

// Collect goes through each defined type and lists all resources, filtered by the
// scoping rules.
func Collect(context *context.Context, backend backend.Backend, resources []Collector) []Reference {
	references := []Reference{}

	for _, r := range resources {
		// Translate from a concrete opbect type into a group/version/kind with
		// the scheme mapper.  Then use the GVK to map into an API call.
		kinds, _, err := scheme.Scheme.ObjectKinds(r.Resource)
		if err != nil {
			fmt.Println("failed to map resource to GVK:", err)
			continue
		}

		gvk := kinds[0]

		mapping, err := context.RESTMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			fmt.Println("failed to map gvk to API:", err)
			continue
		}

		// Collect everything by default, if we are filtering based on cluster
		// then do this server side in order to save bandwidth.
		opts := metav1.ListOptions{}

		if r.Scope == ScopeCluster {
			selector, err := GetResourceSelector(&context.Config)
			if err != nil {
				fmt.Println("failed to get label selector for resource:", err)
				continue
			}

			opts.LabelSelector = selector.String()
		}

		var objects *unstructured.UnstructuredList

		if mapping.Scope.Name() == meta.RESTScopeNameRoot {
			objects, err = context.DynamicClient.Resource(mapping.Resource).List(ctx.Background(), opts)
			if err != nil {
				fmt.Println("failed to list dynamic resources:", err)
				continue
			}
		} else {
			objects, err = context.DynamicClient.Resource(mapping.Resource).Namespace(context.Namespace()).List(ctx.Background(), opts)
			if err != nil {
				fmt.Println("failed to list dynamic resources:", err)
				continue
			}
		}

	NextObject:
		for _, o := range objects.Items {
			// Watch out for the operator, we need to flag this for special
			// handling down the line.
			var isOperatorDeployment bool

			// At this level we get access to the full resource, and the Operator
			// image name.
			operatorImage := context.Config.OperatorImage

			// Perform any post list filtering.  If we are filtering based on cluster
			// name then reject any resources that don't match a named cluster.  If we
			// are filtering based on namespace name, then reject any resources that
			// don't match the namespace name.
			switch r.Scope {
			case ScopeClusterName:
				// No constraints, let everything through.
				if len(context.Config.Clusters) == 0 {
					break
				}

				// Check to see it the named cluster is specified, rejecting
				// any that aren't in the list.
				found := false

				for _, name := range context.Config.Clusters {
					if name == o.GetName() {
						found = true
						break
					}
				}

				if !found {
					continue NextObject
				}
			case ScopeNamespace:
				if context.Namespace() != o.GetName() {
					continue NextObject
				}
			case ScopeCouchbaseGroup:
				if !strings.Contains(o.GetName(), couchbasev2.GroupName) {
					continue NextObject
				}
			case ScopeOperatorDeployment:
				// We need to interrogate all deployments, and work out if this
				// deployment refers to the Operator.  Doing this in typed land
				// is a lot easier...
				deployment := &appsv1.Deployment{}
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, deployment); err != nil {
					continue NextObject
				}

				// Determine whether the deployment is the operator and needs
				// further scrutiny.
				if k8s.IsOperatorDeployment(context, deployment) {
					isOperatorDeployment = true
					operatorImage = deployment.Spec.Template.Spec.Containers[0].Image
				}

				// Filter out non-operator deployments to limit the scope of our
				// collection efforts, we don't need to see, nor do customers want
				// us to see all the things (this is meaningless, we collect all the
				// secrets as is...)
				if !context.Config.All && !isOperatorDeployment {
					continue NextObject
				}
			}

			// Finally marshal to YAML and add to the backend archive.
			data, err := yaml.Marshal(o.Object)
			if err != nil {
				fmt.Println("failed to marshal data:", err)
				continue
			}

			var path string

			if mapping.Scope.Name() == meta.RESTScopeNameRoot {
				path = util.ArchivePathUnscoped(gvk.Kind, o.GetName(), o.GetName()+".yaml")
			} else {
				path = util.ArchivePath(context.Namespace(), gvk.Kind, o.GetName(), o.GetName()+".yaml")
			}

			_ = backend.WriteFile(path, string(data))

			reference := NewReference(gvk.Kind, o.GetName())

			// Metadata collation, used by support for automation.
			switch gvk.Kind {
			case "CouchbaseCluster":
				// The image will be part of the cluster, it's guaranteed by CRD
				// validation.  Events however, we cannot guess at whether there are
				// any at this point in the process, so just fill in the path that we
				// expect, and support can deal with the missing file.
				image, _, _ := unstructured.NestedString(o.Object, "spec", "image")

				log_meta.SetCluster(o.GetName(), image, path)
			case "Deployment":
				if isOperatorDeployment {
					reference.SetIsOperator()

					log_meta.SetOperator(operatorImage)
				}
			}

			references = append(references, reference)
		}
	}

	return references
}
