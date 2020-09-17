// This takes generated CDR YAML and parses out any broken stuff.
package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/ghodss/yaml"
)

func prune(in interface{}) (interface{}, error) {
	switch t := in.(type) {
	case map[string]interface{}:
		out := map[string]interface{}{}

		for k, v := range t {
			switch k {
			case "x-kubernetes-list-map-keys", "x-kubernetes-list-type":
				// Ignore this, it's broken for native types.
				// We should only do this if the map keys are neither
				// required nor have defaults.
				break
			default:
				pruned, err := prune(v)
				if err != nil {
					return nil, err
				}

				if pruned != nil {
					out[k] = pruned
				}
			}
		}

		if len(out) == 0 {
			return nil, nil
		}

		return out, nil
	case []interface{}:
		out := []interface{}{}

		for _, v := range t {
			pruned, err := prune(v)
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

		var raw interface{}

		if err := yaml.Unmarshal([]byte(manifest), &raw); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		// Remove the status, crdgen shouldn't be omitting this anyway.
		topLevel, ok := raw.(map[string]interface{})
		if !ok {
			fmt.Println("object incorrectly formatted")
			os.Exit(1)
		}

		delete(topLevel, "status")

		// Recusively do a DFS through the tree removing any bad things that
		// cause failure (e.g. bugs in Kubernetes types that haven't been fixed).
		pruned, err := prune(raw)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		encoded, err := yaml.Marshal(pruned)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		manifests[i] = string(encoded)
	}

	output := strings.Join(manifests, "---\n")

	if err := ioutil.WriteFile(out, []byte(output), 0644); err != nil {
		fmt.Println(err)
		os.Exit(0)
	}
}
