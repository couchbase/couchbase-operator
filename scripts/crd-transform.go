/*
Copyright 2020-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

// This takes generated CDR YAML and parses out any broken stuff.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"

	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/version"

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
	// mutate is used to do modify attributes.
	mutate func(value interface{}) interface{}
}

// pathMatcher is an abstraction to see if a particular JSON path matches something we
// care about.
type pathMatcher []pathMatch

// get looks up the first path match.
func (p pathMatcher) get(group, kind, path string) *pathMatch {
	for i := range p {
		pm := &p[i]

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

		return pm
	}

	return nil
}

// contains determines whether there a path match for the resource type.
func (p pathMatcher) contains(group, kind, path string) bool {
	glog.V(2).Infof("Examining path %s %s %s", group, kind, path)

	return p.get(group, kind, path) != nil
}

// retain is a set of paths we should always keep that would be otherwise pruned.
var retain = pathMatcher{
	// This needs to be an empty object in order to work, not be pruned.
	{
		path: ".spec.versions.subresources.status",
	},
}

// discard is a set of paths that we should always remove, typically these are
// due to Kubernetes breaking backwards compatibility.
var discard = pathMatcher{
	// Don't emit the status, kubernetes won't accept it.
	{
		path: ".status",
	},
	// These native types are invalid, and also unnecessary, so just remove them.
	{
		group: "couchbase.com",
		kind:  "CouchbaseCluster",
		path:  ".spec.versions.schema.openAPIV3Schema.properties.spec.properties.networking.properties.adminConsoleServiceTemplate.properties.spec.properties.ports",
	},
	{
		group: "couchbase.com",
		kind:  "CouchbaseCluster",
		path:  ".spec.versions.schema.openAPIV3Schema.properties.spec.properties.networking.properties.exposedFeatureServiceTemplate.properties.spec.properties.ports",
	},
	// In operator 2.0 (1.13) this was not marked as omitempty, as a result
	// when upgrading to operator 2.1+ (1.17+), the "null" fails validation because
	// it's not an object.  To support concurrent operation, we just ignore this
	// attribute as it's unimportant.
	{
		group: "couchbase.com",
		kind:  "CouchbaseCluster",
		path:  ".spec.versions.schema.openAPIV3Schema.properties.spec.properties.volumeClaimTemplates.items.properties.spec.properties.dataSource",
	},
	// Validation is "broken" for pod templates, in that they require at least
	// one container, so remove this restriction, and prevent process injection!
	// The removal of required is somewhat imprecise and may need fixing in the
	// future, it's a hard problem as we cannot remove array elements by value
	// with JSON paths.
	{
		group: "couchbase.com",
		kind:  "CouchbaseCluster",
		path:  ".spec.versions.schema.openAPIV3Schema.properties.spec.properties.servers.items.properties.pod.properties.spec.properties.containers",
	},
	{
		group: "couchbase.com",
		kind:  "CouchbaseCluster",
		path:  ".spec.versions.schema.openAPIV3Schema.properties.spec.properties.servers.items.properties.pod.properties.spec.properties.initContainers",
	},
	{
		group: "couchbase.com",
		kind:  "CouchbaseCluster",
		path:  ".spec.versions.schema.openAPIV3Schema.properties.spec.properties.servers.items.properties.pod.properties.spec.required",
	},
	// Stuff we always override.
	{
		group: "couchbase.com",
		kind:  "CouchbaseCluster",
		path:  ".spec.versions.schema.openAPIV3Schema.properties.spec.properties.networking.properties.adminConsoleServiceTemplate.properties.spec.properties.publishNotReadyAddresses",
	},
	{
		group: "couchbase.com",
		kind:  "CouchbaseCluster",
		path:  ".spec.versions.schema.openAPIV3Schema.properties.spec.properties.networking.properties.adminConsoleServiceTemplate.properties.spec.properties.selector",
	},
	{
		group: "couchbase.com",
		kind:  "CouchbaseCluster",
		path:  ".spec.versions.schema.openAPIV3Schema.properties.spec.properties.networking.properties.exposedFeatureServiceTemplate.properties.spec.properties.publishNotReadyAddresses",
	},
	{
		group: "couchbase.com",
		kind:  "CouchbaseCluster",
		path:  ".spec.versions.schema.openAPIV3Schema.properties.spec.properties.networking.properties.exposedFeatureServiceTemplate.properties.spec.properties.selector",
	},
	{
		group: "couchbase.com",
		kind:  "CouchbaseCluster",
		path:  ".spec.versions.schema.openAPIV3Schema.properties.spec.properties.servers.items.properties.pod.properties.spec.properties.restartPolicy",
	},
	{
		group: "couchbase.com",
		kind:  "CouchbaseCluster",
		path:  ".spec.versions.schema.openAPIV3Schema.properties.spec.properties.servers.items.properties.pod.properties.spec.properties.hostname",
	},
	{
		group: "couchbase.com",
		kind:  "CouchbaseCluster",
		path:  ".spec.versions.schema.openAPIV3Schema.properties.spec.properties.servers.items.properties.pod.properties.spec.properties.subdomain",
	},
	{
		group: "couchbase.com",
		kind:  "CouchbaseCluster",
		path:  ".spec.versions.schema.openAPIV3Schema.properties.spec.properties.servers.items.properties.pod.properties.spec.properties.securityContext",
	},
	{
		group: "couchbase.com",
		kind:  "CouchbaseCluster",
		path:  ".spec.versions.schema.openAPIV3Schema.properties.spec.properties.servers.items.properties.pod.properties.spec.properties.readinessGates",
	},
	{
		group: "couchbase.com",
		kind:  "CouchbaseCluster",
		path:  ".spec.versions.schema.openAPIV3Schema.properties.spec.properties.servers.items.properties.pod.properties.spec.properties.hostAliases",
	},
	{
		group: "couchbase.com",
		kind:  "CouchbaseCluster",
		path:  ".spec.versions.schema.openAPIV3Schema.properties.spec.properties.servers.items.properties.pod.properties.spec.properties.volumes",
	},
	{
		group: "couchbase.com",
		kind:  "CouchbaseCluster",
		path:  ".spec.versions.schema.openAPIV3Schema.properties.spec.properties.servers.items.properties.pod.properties.spec.properties.ephemeralContainers",
	},
}

// mutators allow the modification of attributes.  The only reason this is necessary is
// because controller-tools is broken.
var mutators = pathMatcher{
	// Kubebuilder has no way of creating an empty object as a default, so we
	// need to explicitly mark this and mutate it to a valid CRD default.
	{
		path:   "*.default",
		mutate: mutateEmptyObjectDefault,
	},
}

// mutateEmptyObjectDefault catches our empty object marker and replaces it with an
// empty object.
func mutateEmptyObjectDefault(v interface{}) interface{} {
	if value, ok := v.(string); ok && value == "x-couchbase-object" {
		return struct{}{}
	}

	return v
}

func prune(in interface{}, group, kind, path string) (interface{}, error) {
	// Discard anything we are forced to.
	if discard.contains(group, kind, path) {
		glog.V(1).Infof("Discarding %s %s %s", group, kind, path)
		return nil, nil
	}

	// Mutate any attributes that we've injected markers to do so.
	if pm := mutators.get(group, kind, path); pm != nil {
		glog.V(1).Infof("Mutating %s %s %s", group, kind, path)
		return pm.mutate(in), nil
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

// versionMarkerPrefix is the comment marker prefix for minimum server version.
const versionMarkerPrefix = "+couchbase:version:minimum="

// typesFile is the path to the types.go source file.
const typesFile = "pkg/apis/couchbase/v2/types.go"

// extractJSONName extracts the JSON field name from a struct tag.
func extractJSONName(tag string) string {
	jsonTag := reflect.StructTag(tag).Get("json")
	if jsonTag == "" || jsonTag == "-" {
		return ""
	}

	name, _, _ := strings.Cut(jsonTag, ",")

	return name
}

// parseVersionMarkers parses the types.go source file and returns a map of
// JSON field name to minimum server version string. The markers are extracted
// from comments of the form: // +couchbase:version:minimum=X.Y.Z
func parseVersionMarkers(path string) (map[string]string, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}

	result := make(map[string]string)

	// Walk all type declarations.
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}

			for _, field := range structType.Fields.List {
				if field.Tag == nil {
					continue
				}

				// Strip the backtick quotes from the tag.
				tag := strings.Trim(field.Tag.Value, "`")

				jsonName := extractJSONName(tag)
				if jsonName == "" {
					continue
				}

				// Check the field's doc comment for the version marker.
				if field.Doc == nil {
					continue
				}

				for _, comment := range field.Doc.List {
					text := strings.TrimSpace(strings.TrimPrefix(comment.Text, "//"))
					if strings.HasPrefix(text, versionMarkerPrefix) {
						version := strings.TrimPrefix(text, versionMarkerPrefix)
						result[jsonName] = version
					}
				}
			}
		}
	}

	return result, nil
}

// injectVersionMarkers walks the CRD schema and injects x-couchbase-version-minimum
// extensions into property definitions that match the provided version map.
func injectVersionMarkers(schema interface{}, versionMap map[string]string) {
	obj, ok := schema.(map[string]interface{})
	if !ok {
		return
	}

	// If this node has "properties", walk each property.
	if props, ok := obj["properties"].(map[string]interface{}); ok {
		for name, prop := range props {
			propMap, ok := prop.(map[string]interface{})
			if !ok {
				continue
			}

			if version, found := versionMap[name]; found {
				propMap["x-couchbase-version-minimum"] = version

				// Automatically append version info to the description
				versionSuffix := "This field is available in Couchbase Server " + version + " and later."

				if desc, ok := propMap["description"].(string); ok {
					if !strings.Contains(desc, versionSuffix) {
						propMap["description"] = strings.TrimRight(desc, " \n") + "\n" + versionSuffix
					}
				} else {
					propMap["description"] = versionSuffix
				}
			}

			// Recurse into the property.
			injectVersionMarkers(propMap, versionMap)
		}
	}

	// Handle arrays with object items.
	if items, ok := obj["items"].(map[string]interface{}); ok {
		injectVersionMarkers(items, versionMap)
	}

	// Handle additionalProperties.
	if addlProps, ok := obj["additionalProperties"].(map[string]interface{}); ok {
		injectVersionMarkers(addlProps, versionMap)
	}
}

func main() {
	var in string

	var out string

	flag.StringVar(&in, "in", "example/crd.yaml", "Input file")
	flag.StringVar(&out, "out", "example/crd.yaml", "Output file")
	flag.Parse()

	input, err := os.ReadFile(in)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// Parse version markers from the types.go source file.
	versionMap, err := parseVersionMarkers(typesFile)
	if err != nil {
		glog.Exitf("Failed to parse version markers: %v", err)
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

		prunedObject, ok := pruned.(map[string]interface{})
		if !ok {
			glog.Exit("Pruned CRD in wrong format")
		}

		// Inject x-couchbase-version-minimum extensions into the CRD schema
		// from the parsed +couchbase:version:minimum= markers in types.go.
		if specMap, ok := prunedObject["spec"].(map[string]interface{}); ok {
			if versionsList, ok := specMap["versions"].([]interface{}); ok {
				for _, v := range versionsList {
					if versionEntry, ok := v.(map[string]interface{}); ok {
						if schema, ok := versionEntry["schema"].(map[string]interface{}); ok {
							if openAPI, ok := schema["openAPIV3Schema"].(map[string]interface{}); ok {
								injectVersionMarkers(openAPI, versionMap)
							}
						}
					}
				}
			}
		}

		output := unstructured.Unstructured{
			Object: prunedObject,
		}

		// Set the version information.
		annotations := output.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}

		annotations[constants.ConfigurationVersionAnnotation] = version.Version

		output.SetAnnotations(annotations)

		encoded, err := yaml.Marshal(output.Object)
		if err != nil {
			glog.Exit(err)
		}

		manifests[i] = string(encoded)
	}

	output := strings.Join(manifests, "---\n")

	if err := os.WriteFile(out, []byte(output), 0o644); err != nil {
		glog.Exit(err)
	}
}
