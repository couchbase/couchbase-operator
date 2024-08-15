package main

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"io/ioutil"
	"strings"
)

type metricMetadata struct {
	Type           string   `json:"type"`
	Help           string   `json:"help"`
	Unit           string   `json:"unit,omitempty"`
	Added          string   `json:"added"`
	Stability      string   `json:"stability"`
	Labels         []string `json:"labels"`
	OptionalLabels []string `json:"optionalLabels"`
}

func main() {
	fset := token.NewFileSet()
	d, err := parser.ParseFile(fset, "pkg/metrics/metrics.go", nil, parser.ParseComments)
	if err != nil {
		panic(err)
	}

	var metrics = map[string]metricMetadata{}
	for _, c := range d.Comments {
		var metricMetadata = new(metricMetadata)
		var metricName string
		for _, c1 := range c.List {
			if strings.Contains(c1.Text, "name:") {
				metricName = strings.TrimLeft(c1.Text, "// name: ")
			}
			if strings.Contains(c1.Text, "type:") {
				metricMetadata.Type = strings.TrimLeft(c1.Text, "// type: ")
			}
			if strings.Contains(c1.Text, "help:") {
				metricMetadata.Help = strings.TrimLeft(c1.Text, "// help: ")
			}
			if strings.Contains(c1.Text, "unit:") {
				metricMetadata.Unit = strings.TrimLeft(c1.Text, "// unit: ")
			}
			if strings.Contains(c1.Text, "added:") {
				metricMetadata.Added = strings.TrimLeft(c1.Text, "// added: ")
			}
			if strings.Contains(c1.Text, "stability:") {
				metricMetadata.Stability = strings.TrimLeft(c1.Text, "// stability: ")
			}
			if strings.Contains(c1.Text, "labels:") {
				labels := strings.TrimLeft(c1.Text, "// labels: ")
				labels = strings.ReplaceAll(labels, " ", "")
				s := strings.Split(labels, ",")
				metricMetadata.Labels = s
			}
			if strings.Contains(c1.Text, "optionalLabels:") {
				optionalLabels := strings.TrimLeft(c1.Text, "// optionalLabels: ")
				optionalLabels = strings.ReplaceAll(optionalLabels, " ", "")
				s := strings.Split(optionalLabels, ",")
				metricMetadata.OptionalLabels = s
			}
		}
		metrics[metricName] = *metricMetadata
	}

	json, err := json.MarshalIndent(metrics, "", "    ")
	if err != nil {
		panic(err)
	}

	error := ioutil.WriteFile("docs/user/modules/ROOT/attachments/cao_metrics_metadata.json", json, 0644)

	if error != nil {
		panic(error)
	}
}
