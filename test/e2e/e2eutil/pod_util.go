/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2eutil

import (
	"context"
	"fmt"
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func MustAddCustomAnnotationAndLabels(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, annotations, labels map[string]string) {
	err := addCustomAnnotationAndLabels(k8s, couchbase, annotations, labels)
	if err != nil {
		Die(t, err)
	}
}

func addCustomAnnotationAndLabels(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, annotations, labels map[string]string) error {
	listOptions := metav1.ListOptions{
		LabelSelector: constants.CouchbaseServerClusterKey + "=" + couchbase.Name,
	}

	pods, err := k8s.KubeClient.CoreV1().Pods(couchbase.Namespace).List(context.Background(), listOptions)
	if err != nil {
		return err
	}

	for _, pod := range pods.Items {
		newPod := pod.DeepCopy()
		newAnnotations := newPod.ObjectMeta.Annotations

		if newAnnotations == nil {
			newAnnotations = make(map[string]string)
		}

		for key, value := range annotations {
			newAnnotations[key] = value
		}

		newPod.ObjectMeta.Annotations = newAnnotations

		newLabels := newPod.ObjectMeta.Labels

		if newLabels == nil {
			newLabels = make(map[string]string)
		}

		for key, value := range labels {
			newLabels[key] = value
		}

		newPod.ObjectMeta.Labels = newLabels

		_, err := k8s.KubeClient.CoreV1().Pods(newPod.ObjectMeta.Namespace).Update(context.Background(), newPod, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}

	return nil
}

func MustCheckCustomAnnotationAndLabels(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, annotations, labels map[string]string) {
	err := checkCustomAnnotationAndLabels(k8s, couchbase, annotations, labels)
	if err != nil {
		Die(t, err)
	}
}

func checkPodMaps(actual, expected map[string]string, podName, mapType string) error {
	for key, value := range expected {
		actualValue, exists := actual[key]
		if !exists {
			return fmt.Errorf("no custom %s for %q=%q on pod %q", mapType, key, value, podName)
		}

		if actualValue != value {
			return fmt.Errorf("mismatch in custom %s for %q=%q on pod %q", mapType, key, value, podName)
		}
	}

	return nil
}

func checkCustomAnnotationAndLabels(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, annotations, labels map[string]string) error {
	listOptions := metav1.ListOptions{
		LabelSelector: constants.CouchbaseServerClusterKey + "=" + couchbase.Name,
	}

	if len(annotations) == 0 && len(labels) == 0 {
		return nil
	}

	pods, err := k8s.KubeClient.CoreV1().Pods(couchbase.Namespace).List(context.Background(), listOptions)
	if err != nil {
		return err
	}

	for _, pod := range pods.Items {
		err = checkPodMaps(pod.Annotations, annotations, pod.Name, "annotation")
		if err != nil {
			return err
		}

		err = checkPodMaps(pod.Labels, labels, pod.Name, "label")
		if err != nil {
			return err
		}
	}

	return nil
}
