// This takes generated CDR YAML and parses out any broken stuff.
package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/golang/glog"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// pathMatch matches a JSON path within a versioned kind.
type pathMatch struct {
	// group is the group of a CRD resource e.g. "couchbase.com".  Leave blank to
	// match all groups.
	group string
	// kind is the CRD kind of resource e.g. "CouchbaseCluster". Leave blank to
	// match all kinds.
	kind string
	// path ts the json path to match e.g. ".spec.foo".  This may be prefixed with
	// an asterix to perform a suffix match.
	path string
}

// pathMatcher is an abstraction to see if a particular JSON path matches something we
// care about.
type pathMatcher []pathMatch

// contains determines whether there a path match for the resource type.
func (p pathMatcher) contains(group, kind, path string) bool {
	for _, pm := range p {
		if pm.group != "" && pm.group != group {
			continue
		}

		if pm.kind != "" && pm.kind != kind {
			continue
		}

		if pm.path != path {
			if pm.path[0] != '*' {
				continue
			}

			if !strings.HasSuffix(path, pm.path[1:]) {
				continue
			}
		}

		return true
	}

	return false
}

// retain is a set of paths we should always keep that would be otherwise pruned.
var retain = pathMatcher{
	// This needs to be an empty object in order to work, not be pruned.
	{
		path: ".spec.subresources.status",
	},
}

// discard is a set of paths that we should always remove, typically these are
// due to Kubernetes breaking backwards compatibility.
var discard = pathMatcher{
	// Don't emit the status, kubernetes won't accept it.
	{
		path: ".status",
	},
	// This key is broken for native types.
	{
		path: "*.x-kubernetes-list-map-keys",
	},
	// This key is broken for native types.
	{
		path: "*.x-kubernetes-list-type",
	},
	// In operator 2.0 (1.13) this was not marked as omitempty, as a result
	// when upgrading to operator 2.1 (1.17), the "null" fails validation because
	// it's not an object.  To support concurrent operation, we just ignore this
	// attribute as it's unimportant.
	{
		group: "couchbase.com",
		kind:  "CouchbaseCluster",
		path:  ".spec.validation.openAPIV3Schema.properties.spec.properties.volumeClaimTemplates.items.properties.spec.properties.dataSource",
	},
	// Validation is "broken" for pod templates, in that they require at least
	// one container, so remove this restriction.
	{
		group: "couchbase.com",
		kind:  "CouchbaseCluster",
		path:  ".spec.validation.openAPIV3Schema.properties.spec.properties.servers.items.properties.pod.properties.spec.properties.containers",
	},
}

func prune(in interface{}, group, kind, path string) (interface{}, error) {
	// Discard anything we are forced to.
	if discard.contains(group, kind, path) {
		glog.V(1).Infof("Discarding %s %s %s", group, kind, path)
		return nil, nil
	}

	// Keep anything we are forced to.
	if retain.contains(group, kind, path) {
		glog.V(1).Infof("Retaining %s %s %s", group, kind, path)
		return in, nil
	}

	switch t := in.(type) {
	case map[string]interface{}:
		out := map[string]interface{}{}

		for k, v := range t {
			pruned, err := prune(v, group, kind, path+"."+k)
			if err != nil {
				return nil, err
			}

			if pruned != nil {
				out[k] = pruned
			}
		}

		if len(out) == 0 {
			return nil, nil
		}

		return out, nil
	case []interface{}:
		out := []interface{}{}

		for _, v := range t {
			pruned, err := prune(v, group, kind, path)
			if err != nil {
				return nil, err
			}

			if pruned != nil {
				out = append(out, pruned)
			}
		}

		if len(out) == 0 {
			return nil, nil
		}

		return out, nil
	default:
		return in, nil
	}
}

func main() {
	var in string

	var out string

	flag.StringVar(&in, "in", "example/crd.yaml", "Input file")
	flag.StringVar(&out, "out", "example/crd.yaml", "Output file")
	flag.Parse()

	input, err := ioutil.ReadFile(in)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	manifests := strings.Split(string(input), "\n---\n")

	for i, manifest := range manifests {
		if strings.TrimSpace(manifest) == "" {
			continue
		}

		raw := unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(manifest), &raw); err != nil {
			glog.Exit(err)
		}

		group, ok, _ := unstructured.NestedString(raw.Object, "spec", "group")
		if !ok {
			glog.Exit("CRD doesn't have group attribute")
		}

		kind, ok, _ := unstructured.NestedString(raw.Object, "spec", "names", "kind")
		if !ok {
			glog.Exit("CRD doesn't have kind name attribute")
		}

		// Recusively do a DFS through the tree removing any bad things that
		// cause failure (e.g. bugs in Kubernetes types that haven't been fixed).
		pruned, err := prune(raw.Object, group, kind, "")
		if err != nil {
			glog.Exit(err)
		}

		encoded, err := yaml.Marshal(pruned)
		if err != nil {
			glog.Exit(err)
		}

		manifests[i] = string(encoded)
	}

	output := strings.Join(manifests, "---\n")

	if err := ioutil.WriteFile(out, []byte(output), 0644); err != nil {
		glog.Exit(err)
	}
}
