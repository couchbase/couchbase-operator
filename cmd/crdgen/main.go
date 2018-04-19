package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	"github.com/ghodss/yaml"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
)

var outfile string

func init() {
	flag.StringVar(&outfile, "outfile", "", "The file to write the crd to")
	flag.Parse()
}

func main() {
	if outfile == "" {
		fmt.Println("The outfile parameter is required")
		os.Exit(1)
	}

	crd := k8sutil.GetCRD()
	resource := struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		MetaData   struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Spec apiextensionsv1beta1.CustomResourceDefinitionSpec `json:"spec"`
	}{
		APIVersion: apiextensionsv1beta1.SchemeGroupVersion.String(),
		Kind:       "CustomResourceDefinition",
		Spec:       crd.Spec,
	}
	resource.MetaData.Name = crd.Name

	data, err := yaml.Marshal(resource)
	if err != nil {
		fmt.Printf("Error marshaling crd: %s\n", err)
		os.Exit(1)
	}

	err = ioutil.WriteFile(outfile, data, 0644)
	if err != nil {
		fmt.Printf("Error writing crd to %s: %s\n", outfile, err)
		os.Exit(1)
	}
}
