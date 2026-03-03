/*
Copyright 2020-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package util

import (
	"context"
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
func CouchbaseOperatorConfig(args ...string) error {
	output, err := exec.Command("../../build/bin/cbopcfg", args...).CombinedOutput()
	logrus.Infof("Command output:\n%s", string(output))

	return err
}

// CreateResourceWithUpdate dynamically creates an unstructured resource.
func CreateResourceWithUpdate(c *Clients, resource *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	logrus.Infof("Creating resource %v %v %v", resource.GetAPIVersion(), resource.GetKind(), resource.GetName())

	gvk := resource.GroupVersionKind()

	mapping, err := c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, err
	}

	if mapping.Scope.Name() == meta.RESTScopeNameRoot {
		updated, err := c.dynamic.Resource(mapping.Resource).Create(context.Background(), resource, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}

		return updated, nil
	}

	updated, err := c.dynamic.Resource(mapping.Resource).Namespace(resource.GetNamespace()).Create(context.Background(), resource, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	return updated, nil
}

// CreateResource dynamically creates an unstructured resource, omitting the updated resource.
func CreateResource(c *Clients, resource *unstructured.Unstructured) error {
	_, err := CreateResourceWithUpdate(c, resource)
	return err
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
		if old, err = c.dynamic.Resource(mapping.Resource).Get(context.Background(), resource.GetName(), metav1.GetOptions{}); err != nil {
			return err
		}
	} else {
		if old, err = c.dynamic.Resource(mapping.Resource).Namespace(resource.GetNamespace()).Get(context.Background(), resource.GetName(), metav1.GetOptions{}); err != nil {
			return err
		}
	}

	for field, value := range resource.Object {
		if field == "metadata" || field == "status" {
			continue
		}

		if err := unstructured.SetNestedField(old.Object, value, field); err != nil {
			return err
		}
	}

	if mapping.Scope.Name() == meta.RESTScopeNameRoot {
		if _, err := c.dynamic.Resource(mapping.Resource).Update(context.Background(), old, metav1.UpdateOptions{}); err != nil {
			return err
		}
	} else {
		if _, err := c.dynamic.Resource(mapping.Resource).Namespace(resource.GetNamespace()).Update(context.Background(), old, metav1.UpdateOptions{}); err != nil {
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
		if _, err = c.dynamic.Resource(mapping.Resource).Get(context.Background(), resource.GetName(), metav1.GetOptions{}); err != nil {
			if errors.IsNotFound(err) {
				return CreateResource(c, resource)
			}

			return err
		}
	} else {
		if _, err = c.dynamic.Resource(mapping.Resource).Namespace(resource.GetNamespace()).Get(context.Background(), resource.GetName(), metav1.GetOptions{}); err != nil {
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
		if err := c.dynamic.Resource(mapping.Resource).Delete(context.Background(), resource.GetName(), *metav1.NewDeleteOptions(0)); err != nil {
			return err
		}

		return nil
	}

	if err := c.dynamic.Resource(mapping.Resource).Namespace(resource.GetNamespace()).Delete(context.Background(), resource.GetName(), *metav1.NewDeleteOptions(0)); err != nil {
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
func ResourceCondition(c *Clients, group, version, kind, namespace, name, conditionType, conditionStatus string) WaitFunc {
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
			resource, err = c.dynamic.Resource(mapping.Resource).Get(context.Background(), name, metav1.GetOptions{})
			if err != nil {
				return err
			}
		} else {
			resource, err = c.dynamic.Resource(mapping.Resource).Namespace(namespace).Get(context.Background(), name, metav1.GetOptions{})
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

// NoResourceCondition allows things to check a condition doesn't exist (e.g. an error) for a
// period of time.
func NoResourceCondition(c *Clients, group, version, kind, namespace, name, conditionType string) WaitFunc {
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
			resource, err = c.dynamic.Resource(mapping.Resource).Get(context.Background(), name, metav1.GetOptions{})
			if err != nil {
				return err
			}
		} else {
			resource, err = c.dynamic.Resource(mapping.Resource).Namespace(namespace).Get(context.Background(), name, metav1.GetOptions{})
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

			if typ == conditionType {
				return fmt.Errorf("condition set unexpectedly")
			}
		}

		return nil
	}
}

// ResourceEvent allows waiting on an event happening.  This matches any event raised ever
// (in the last hour at least), so beware of using static names and reusing the cluster.
func ResourceEvent(c *Clients, group, version, kind, namespace, name, reason string) WaitFunc {
	return func() error {
		selector := map[string]string{
			"involvedObject.apiVersion": group + "/" + version,
			"involvedObject.kind":       kind,
			"involvedObject.name":       name,
		}

		events, err := c.kubernetes.CoreV1().Events(namespace).List(context.Background(), metav1.ListOptions{FieldSelector: labels.FormatLabels(selector)})
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

// ResourceDeleted checks for a resource being deleted before continuing.
func ResourceDeleted(c *Clients, group, version, kind, namespace, name string) WaitFunc {
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

		if mapping.Scope.Name() == meta.RESTScopeNameRoot {
			if _, err = c.dynamic.Resource(mapping.Resource).Get(context.Background(), name, metav1.GetOptions{}); err != nil {
				if errors.IsNotFound(err) {
					return nil
				}

				return err
			}
		} else {
			if _, err = c.dynamic.Resource(mapping.Resource).Namespace(namespace).Get(context.Background(), name, metav1.GetOptions{}); err != nil {
				if errors.IsNotFound(err) {
					return nil
				}

				return err
			}
		}

		return fmt.Errorf("resource %s/%s/%s %s/%s still exists", group, version, kind, namespace, name)
	}
}
