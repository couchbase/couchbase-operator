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
	if len(c.Clusters.Values) != 0 {
		req, err := labels.NewRequirement(constants.LabelCluster, selection.In, c.Clusters.Values)
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

// LogLevel records how necessary a resource collection is.
type LogLevel int

const (
	// LogLevelRequired means we absolutely need to see this to debug
	// issues.
	LogLevelRequired LogLevel = iota

	// LogLevelSensitive means we may need this, but it also may contain
	// sensitive information, or we're just hoovering up all the data
	// which could be considered naughty.
	LogLevelSensitive
)

// Collector defines types to collect and how to collect them.
type Collector struct {
	// Resource is the object type.  We will use reflection to infer API
	// accesses.
	Resource runtime.Object

	// Scope is the scope of a collection.
	Scope Scope

	// LogLevel is how much we want/need to see the data vs how sensitive
	// it is to the customer.
	LogLevel LogLevel
}

// getRESTMapping translates from a concrete opbect type into a group/version/kind with
// the scheme mapper.  Then use the GVK to map into an API call.
func getRESTMapping(context *context.Context, r runtime.Object) (*meta.RESTMapping, error) {
	kinds, _, err := scheme.Scheme.ObjectKinds(r)
	if err != nil {
		return nil, fmt.Errorf("failed to map resource to GVK: %w", err)
	}

	gvk := kinds[0]

	mapping, err := context.RESTMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to map gvk to API: %w", err)
	}

	return mapping, nil
}

// getListOptions returns API list options with any server side filtering
// applied.
func getListOptions(context *context.Context, scope Scope) (*metav1.ListOptions, error) {
	opts := &metav1.ListOptions{}

	if scope == ScopeCluster {
		selector, err := GetResourceSelector(&context.Config)
		if err != nil {
			return nil, fmt.Errorf("failed to get label selector for resource: %w", err)
		}

		opts.LabelSelector = selector.String()
	}

	return opts, nil
}

// listResources returns all resources of the specified kind that match the filter options.
func listResources(context *context.Context, mapping *meta.RESTMapping, opts *metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	if mapping.Scope.Name() == meta.RESTScopeNameRoot {
		return context.DynamicClient.Resource(mapping.Resource).List(ctx.Background(), *opts)
	}

	return context.DynamicClient.Resource(mapping.Resource).Namespace(context.Namespace()).List(ctx.Background(), *opts)
}

// filterObject returns true if the object needs to be filtered out and not collected.
func filterObject(context *context.Context, scope Scope, o *unstructured.Unstructured) bool {
	switch scope {
	case ScopeClusterName:
		// No constraints, let everything through.
		if len(context.Config.Clusters.Values) == 0 {
			return false
		}

		// Check to see it the named cluster is specified, rejecting
		// any that aren't in the list.
		found := false

		for _, name := range context.Config.Clusters.Values {
			if name == o.GetName() {
				found = true
				break
			}
		}

		if !found {
			return true
		}
	case ScopeNamespace:
		if context.Namespace() != o.GetName() {
			return true
		}
	case ScopeCouchbaseGroup:
		if !strings.Contains(o.GetName(), couchbasev2.GroupName) {
			return true
		}
	case ScopeOperatorDeployment:
		// We need to interrogate all deployments, and work out if this
		// deployment refers to the Operator.  Doing this in typed land
		// is a lot easier...
		deployment := &appsv1.Deployment{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, deployment); err != nil {
			return true
		}

		// Filter out non-operator deployments to limit the scope of our
		// collection efforts, we don't need to see, nor do customers want
		// us to see all the things (this is meaningless, we collect all the
		// secrets as is...)
		if !context.Config.All && !k8s.IsOperatorDeployment(context, deployment) {
			return true
		}
	}

	return false
}

// mutateObject makes any necessary alterations to the object before being emitted e.g.
// for security purposes etc.
func mutateObject(o *unstructured.Unstructured) error {
	if o.GetAPIVersion() == "v1" && o.GetKind() == "Secret" {
		data, ok, err := unstructured.NestedMap(o.Object, "data")
		if err != nil {
			return err
		}

		if ok {
			// Redact secret data (e.g. passwords and private keys).  The
			// apimachinery unstructured interface doesn't support byte
			// arrays, so we have to do this in an unclean way.
			for key := range data {
				data[key] = []byte{}
			}

			o.Object["data"] = data
		}
	}

	return nil
}

// getArchivePath returns the path to archive the resource to.
func getArchivePath(context *context.Context, mapping *meta.RESTMapping, o *unstructured.Unstructured) string {
	if mapping.Scope.Name() == meta.RESTScopeNameRoot {
		return util.ArchivePathUnscoped(o.GetKind(), o.GetName(), o.GetName()+".yaml")
	}

	return util.ArchivePath(context.Namespace(), o.GetKind(), o.GetName(), o.GetName()+".yaml")
}

// processResourceMetadata spot special resources and adds them to the archive metadata
// to make locating key logs easier.
func processResourceMetadata(context *context.Context, o *unstructured.Unstructured, path string, reference Reference) {
	// Metadata collation, used by support for automation.
	switch o.GetKind() {
	case "CouchbaseCluster":
		// The image will be part of the cluster, it's guaranteed by CRD
		// validation.  Events however, we cannot guess at whether there are
		// any at this point in the process, so just fill in the path that we
		// expect, and support can deal with the missing file.
		image, _, _ := unstructured.NestedString(o.Object, "spec", "image")

		log_meta.SetCluster(o.GetName(), image, path)
	case "Deployment":
		deployment := &appsv1.Deployment{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, deployment); err != nil {
			return
		}

		if k8s.IsOperatorDeployment(context, deployment) {
			reference.SetIsOperator()

			log_meta.SetOperator(context.Config.OperatorImage)
		}
	}
}

// Collect goes through each defined type and lists all resources, filtered by the
// scoping rules.
func Collect(context *context.Context, backend backend.Backend, resources []Collector) []Reference {
	references := []Reference{}

	for _, r := range resources {
		// Filter out sensitive collections based on log level.
		if int(r.LogLevel) > context.Config.LogLevel {
			continue
		}

		mapping, err := getRESTMapping(context, r.Resource)
		if err != nil {
			fmt.Println(err)
			continue
		}

		opts, err := getListOptions(context, r.Scope)
		if err != nil {
			fmt.Println(err)
			continue
		}

		objects, err := listResources(context, mapping, opts)
		if err != nil {
			fmt.Println("failed to list dynamic resources:", err)
			continue
		}

		for i := range objects.Items {
			o := &objects.Items[i]

			if filterObject(context, r.Scope, o) {
				continue
			}

			if err := mutateObject(o); err != nil {
				fmt.Println("failed to mutate data:", err)
				continue
			}

			// Finally marshal to YAML and add to the backend archive.
			data, err := yaml.Marshal(o.Object)
			if err != nil {
				fmt.Println("failed to marshal data:", err)
				continue
			}

			path := getArchivePath(context, mapping, o)
			_ = backend.WriteFile(path, string(data))

			reference := NewReference(o.GetKind(), o.GetName())
			processResourceMetadata(context, o, path, reference)
			references = append(references, reference)
		}
	}

	return references
}
