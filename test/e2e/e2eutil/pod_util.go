package e2eutil

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func MustAddCustomAnnotationAndLabels(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, annotations, labels map[string]string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	callback := func() error {
		err := addCustomAnnotationAndLabels(k8s, couchbase, annotations, labels)
		if err != nil {
			Die(t, err)
		}

		return nil
	}

	if err := retryutil.Retry(ctx, 10*time.Second, callback); err != nil {
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

func MustCheckPodSpecAnnotationsForNodeSelector(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, expectedNodeSelLabel map[string]string) {
	err := checkPodSpecAnnotationsForNodeSelector(k8s, couchbase, expectedNodeSelLabel)
	if err != nil {
		Die(t, err)
	}
}

func checkPodSpecAnnotationsForNodeSelector(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, expectedNodeSelLabel map[string]string) error {
	listOptions := metav1.ListOptions{
		LabelSelector: constants.CouchbaseServerClusterKey + "=" + couchbase.Name,
	}

	pods, err := k8s.KubeClient.CoreV1().Pods(couchbase.Namespace).List(context.Background(), listOptions)
	if err != nil {
		return err
	}

	for _, pod := range pods.Items {
		actualSpec := &v1.PodSpec{}
		if annotation, ok := pod.Annotations[constants.PodSpecAnnotation]; ok {
			if err := json.Unmarshal([]byte(annotation), actualSpec); err != nil {
				return err
			}
		}

		if !reflect.DeepEqual(actualSpec.NodeSelector, expectedNodeSelLabel) {
			return fmt.Errorf("expected nodeSelector %v, but actual nodeSelector %v on pod %s", expectedNodeSelLabel, actualSpec.NodeSelector, pod.Name)
		}
	}

	return nil
}
