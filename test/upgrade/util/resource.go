package util

import (
	"fmt"
	"io/ioutil"
	"os/exec"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func parseYAMLs(data []byte) ([]*unstructured.Unstructured, error) {
	yamls := strings.Split(string(data), "\n---\n")

	resources := []*unstructured.Unstructured{}

	for _, yamlString := range yamls {
		if strings.TrimSpace(yamlString) == "" {
			continue
		}

		resource := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(yamlString), resource); err != nil {
			return nil, err
		}

		resources = append(resources, resource)
	}

	return resources, nil
}

// LoadYAMLs reads the specified file and returns an ordered list of YAML blobs.
func LoadYAMLs(path string) ([]*unstructured.Unstructured, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return parseYAMLs(data)
}

// CouchbaseOperatorConfig runs cbopcfg and unmarshals the output.
func CouchbaseOperatorConfig(args ...string) ([]*unstructured.Unstructured, error) {
	command := exec.Command("../../build/bin/cbopcfg", args...)

	data, err := command.CombinedOutput()
	if err != nil {
		return nil, err
	}

	return parseYAMLs(data)
}

// CreateResource dynamically creates an unstructured resource.
func CreateResource(c *Clients, resource *unstructured.Unstructured) error {
	logrus.Infof("Creating resource %v %v %v", resource.GetAPIVersion(), resource.GetKind(), resource.GetName())

	gvk := resource.GroupVersionKind()

	mapping, err := c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return err
	}

	if mapping.Scope.Name() == meta.RESTScopeNameRoot {
		if _, err := c.dynamic.Resource(mapping.Resource).Create(resource, metav1.CreateOptions{}); err != nil {
			return err
		}

		return nil
	}

	if _, err := c.dynamic.Resource(mapping.Resource).Namespace("default").Create(resource, metav1.CreateOptions{}); err != nil {
		return err
	}

	return nil
}

// CreateResources creates resources, in order, from a list of raw YAMl data.
func CreateResources(c *Clients, resources []*unstructured.Unstructured) error {
	for _, resource := range resources {
		if err := CreateResource(c, resource); err != nil {
			return err
		}
	}

	return nil
}

// ReplaceResource is a rudimentary function used to replace resources.  If patches
// any fields in the new resource on top of the current one, with the exception of
// ones we shouldn't be touching.
func ReplaceResource(c *Clients, resource *unstructured.Unstructured) error {
	logrus.Infof("Replacing resource %v %v %v", resource.GetAPIVersion(), resource.GetKind(), resource.GetName())

	gvk := resource.GroupVersionKind()

	mapping, err := c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return err
	}

	var old *unstructured.Unstructured

	if mapping.Scope.Name() == meta.RESTScopeNameRoot {
		if old, err = c.dynamic.Resource(mapping.Resource).Get(resource.GetName(), metav1.GetOptions{}); err != nil {
			return err
		}
	} else {
		if old, err = c.dynamic.Resource(mapping.Resource).Namespace("default").Get(resource.GetName(), metav1.GetOptions{}); err != nil {
			return err
		}
	}

	for field, value := range resource.Object {
		if field == "apiVersion" || field == "kind" || field == "metadata" || field == "status" {
			continue
		}

		if err := unstructured.SetNestedField(old.Object, value, field); err != nil {
			return err
		}
	}

	if mapping.Scope.Name() == meta.RESTScopeNameRoot {
		if _, err := c.dynamic.Resource(mapping.Resource).Update(old, metav1.UpdateOptions{}); err != nil {
			return err
		}
	} else {
		if _, err := c.dynamic.Resource(mapping.Resource).Namespace("default").Update(old, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}

	return nil
}

// ReplaceResources replaces the spec of existing resources.
func ReplaceResources(c *Clients, resources []*unstructured.Unstructured) error {
	for _, resource := range resources {
		if err := ReplaceResource(c, resource); err != nil {
			return err
		}
	}

	return nil
}

func ReplaceOrCreateResource(c *Clients, resource *unstructured.Unstructured) error {
	gvk := resource.GroupVersionKind()

	mapping, err := c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return err
	}

	if mapping.Scope.Name() == meta.RESTScopeNameRoot {
		if _, err = c.dynamic.Resource(mapping.Resource).Get(resource.GetName(), metav1.GetOptions{}); err != nil {
			if errors.IsNotFound(err) {
				return CreateResource(c, resource)
			}

			return err
		}
	} else {
		if _, err = c.dynamic.Resource(mapping.Resource).Namespace("default").Get(resource.GetName(), metav1.GetOptions{}); err != nil {
			if errors.IsNotFound(err) {
				return CreateResource(c, resource)
			}

			return err
		}
	}

	return ReplaceResource(c, resource)
}

func ReplaceOrCreateResources(c *Clients, resources []*unstructured.Unstructured) error {
	for _, resource := range resources {
		if err := ReplaceOrCreateResource(c, resource); err != nil {
			return err
		}
	}

	return nil
}

// DeleteResource dynamically deletes an unstructured resource.
func DeleteResource(c *Clients, resource *unstructured.Unstructured) error {
	logrus.Infof("Deleting resource %v %v %v", resource.GetAPIVersion(), resource.GetKind(), resource.GetName())

	gvk := resource.GroupVersionKind()

	mapping, err := c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return err
	}

	if mapping.Scope.Name() == meta.RESTScopeNameRoot {
		if err := c.dynamic.Resource(mapping.Resource).Delete(resource.GetName(), metav1.NewDeleteOptions(0)); err != nil {
			return err
		}

		return nil
	}

	if err := c.dynamic.Resource(mapping.Resource).Namespace("default").Delete(resource.GetName(), metav1.NewDeleteOptions(0)); err != nil {
		return err
	}

	return nil
}

// DeleteResources deletes resources, in order, from a list of raw YAMl data.
func DeleteResources(c *Clients, resources []*unstructured.Unstructured) error {
	for _, resource := range resources {
		if err := DeleteResource(c, resource); err != nil {
			return err
		}
	}

	return nil
}

// ResourceCondition allows things to wait on any condition on any object type.
func ResourceCondition(c *Clients, group, version, kind, name, conditionType, conditionStatus string) WaitFunc {
	return func() error {
		gvk := schema.GroupVersionKind{
			Group:   group,
			Version: version,
			Kind:    kind,
		}

		mapping, err := c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return err
		}

		var resource *unstructured.Unstructured

		if mapping.Scope.Name() == meta.RESTScopeNameRoot {
			resource, err = c.dynamic.Resource(mapping.Resource).Get(name, metav1.GetOptions{})
			if err != nil {
				return err
			}
		} else {
			resource, err = c.dynamic.Resource(mapping.Resource).Namespace("default").Get(name, metav1.GetOptions{})
			if err != nil {
				return err
			}
		}

		conditions, ok, _ := unstructured.NestedSlice(resource.Object, "status", "conditions")
		if !ok {
			return fmt.Errorf("object has no status conditions")
		}

		for _, condition := range conditions {
			object, ok := condition.(map[string]interface{})
			if !ok {
				return fmt.Errorf("condition malformed")
			}

			typ, ok, _ := unstructured.NestedString(object, "type")
			if !ok {
				return fmt.Errorf("condition type malformed")
			}

			if typ != conditionType {
				continue
			}

			stat, ok, _ := unstructured.NestedString(object, "status")
			if !ok {
				return fmt.Errorf("condition status malformed")
			}

			if stat != conditionStatus {
				return fmt.Errorf("condition status %s, expected %s", stat, conditionStatus)
			}

			return nil
		}

		return fmt.Errorf("condition %s not found", conditionType)
	}
}

// ResourceEvent allows waiting on an event happening.  This matches any event raised ever
// (in the last hour at least), so beware of using static names and reusing the cluster.
func ResourceEvent(c *Clients, group, version, kind, name, reason string) WaitFunc {
	return func() error {
		selector := map[string]string{
			"involvedObject.apiVersion": group + "/" + version,
			"involvedObject.kind":       kind,
			"involvedObject.name":       name,
		}

		events, err := c.kubernetes.CoreV1().Events("default").List(metav1.ListOptions{FieldSelector: labels.FormatLabels(selector)})
		if err != nil {
			return err
		}

		for _, event := range events.Items {
			if event.Reason == reason {
				return nil
			}
		}

		return fmt.Errorf("no event of reason %s seen", reason)
	}
}
