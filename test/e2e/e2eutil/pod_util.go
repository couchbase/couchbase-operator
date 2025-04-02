package e2eutil

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/cluster"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func MustAddCustomAnnotationAndLabels(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, annotations, labels map[string]string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	listOptions := metav1.ListOptions{
		LabelSelector: constants.CouchbaseServerClusterKey + "=" + couchbase.Name,
	}

	pods, err := k8s.KubeClient.CoreV1().Pods(couchbase.Namespace).List(context.Background(), listOptions)
	if err != nil {
		Die(t, err)
	}

	callback := func() error {
		return addCustomAnnotationAndLabels(k8s, annotations, labels, *pods)
	}

	if err := retryutil.Retry(ctx, 10*time.Second, callback); err != nil {
		Die(t, err)
	}
}

func MustAddCustomAnnotationAndLabelsSinglePod(t *testing.T, k8s *types.Cluster, annotations, labels map[string]string, pod v1.Pod) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	var pods = []v1.Pod{pod}

	var podList = v1.PodList{Items: pods}

	callback := func() error {
		return addCustomAnnotationAndLabels(k8s, annotations, labels, podList)
	}

	if err := retryutil.Retry(ctx, 10*time.Second, callback); err != nil {
		Die(t, err)
	}
}

func addCustomAnnotationAndLabels(k8s *types.Cluster, annotations, labels map[string]string, pods v1.PodList) error {
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

func MustCheckPodsForVersion(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, expectedImage, expectedVersion string) {
	err := retryutil.RetryFor(15*time.Second, func() error {
		return CheckPodsForVersion(k8s, couchbase, expectedImage, expectedVersion)
	})

	if err != nil {
		Die(t, err)
	}
}

func CheckPodsForVersion(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, expectedImage, expectedVersion string) error {
	listOptions := metav1.ListOptions{
		LabelSelector: constants.CouchbaseServerClusterKey + "=" + couchbase.Name,
	}

	pods, err := k8s.KubeClient.CoreV1().Pods(couchbase.Namespace).List(context.Background(), listOptions)
	if err != nil {
		return err
	}

	for _, pod := range pods.Items {
		if pod.Spec.Containers[0].Image != expectedImage {
			return fmt.Errorf("expected pod (%s) image to be: %s but found: %s", pod.Name, expectedImage, pod.Spec.Containers[0].Image)
		}

		if podVersion, ok := pod.Annotations[constants.CouchbaseVersionAnnotationKey]; !ok {
			return fmt.Errorf("expected pod (%s) to have %s annotation", pod.Name, constants.CouchbaseVersionAnnotationKey)
		} else if podVersion != expectedVersion {
			return fmt.Errorf("expected pod (%s) version to be %s but found %s", pod.Name, expectedVersion, podVersion)
		}
	}

	return nil
}

func MustCheckPodForVersion(t *testing.T, k8s *types.Cluster, podName string, expectedImage, expectedVersion string) {
	err := retryutil.RetryFor(15*time.Second, func() error {
		return CheckPodForVersion(k8s, podName, expectedImage, expectedVersion)
	})

	if err != nil {
		Die(t, err)
	}
}

func CheckPodForVersion(k8s *types.Cluster, podName string, expectedImage, expectedVersion string) error {
	pod, err := k8s.KubeClient.CoreV1().Pods(k8s.Namespace).Get(context.Background(), podName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if pod.Spec.Containers[0].Image != expectedImage {
		return fmt.Errorf("expected pod (%s) image to be: %s but found: %s", pod.Name, expectedImage, pod.Spec.Containers[0].Image)
	}

	if podVersion, ok := pod.Annotations[constants.CouchbaseVersionAnnotationKey]; !ok {
		return fmt.Errorf("expected pod (%s) to have %s annotation", pod.Name, constants.CouchbaseVersionAnnotationKey)
	} else if podVersion != expectedVersion {
		return fmt.Errorf("expected pod (%s) version to be %s but found %s", pod.Name, expectedVersion, podVersion)
	}

	return nil
}

// Ephemeral Volumes are named deterministically.
// <pod-name>-<volume-name>
// pods are suffixed with the backup name, so we can recreate the generated name and fetch it.
// https://kubernetes.io/docs/concepts/storage/ephemeral-volumes/#persistentvolumeclaim-naming
func FindBackupEphemeralVolume(kubernetes *types.Cluster, backupName string) (*v1.PersistentVolumeClaim, error) {
	pods, err := kubernetes.KubeClient.CoreV1().Pods(kubernetes.Namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	podName := ""

	// scan through looking for the backup prefix.
	for _, pod := range pods.Items {
		if strings.HasPrefix(pod.Name, backupName) {
			podName = pod.Name
		}
	}

	if podName == "" {
		return nil, fmt.Errorf("unable to find pod for backup cronjob %s", backupName)
	}

	pvcs, err := kubernetes.KubeClient.CoreV1().PersistentVolumeClaims(kubernetes.Namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	pvcName := fmt.Sprintf("%s-%s", podName, cluster.BackupVolumeName)
	for _, pvc := range pvcs.Items {
		if pvc.Name == pvcName { // found an ephemeral volume backup pvc.
			return &pvc, nil
		}
	}

	return nil, fmt.Errorf("unable to find pvc %s", pvcName)
}

func MustCheckServerClassPodsForVersion(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, serverClass, image, version string) {
	err := retryutil.RetryFor(time.Second, func() error {
		return checkServerClassPodsForVersion(k8s, couchbase, serverClass, image, version)
	})

	if err != nil {
		Die(t, err)
	}
}

func checkServerClassPodsForVersion(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, serverClass, expectedImage, expectedVersion string) error {
	listOptions := metav1.ListOptions{
		LabelSelector: constants.CouchbaseServerClusterKey + "=" + couchbase.Name + "," + constants.CouchbaseNodeConfKey + "=" + serverClass,
	}

	pods, err := k8s.KubeClient.CoreV1().Pods(couchbase.Namespace).List(context.Background(), listOptions)
	if err != nil {
		return err
	}

	for _, pod := range pods.Items {
		if pod.Spec.Containers[0].Image != expectedImage {
			return fmt.Errorf("expected pod (%s) image to be: %s but found: %s", pod.Name, expectedImage, pod.Spec.Containers[0].Image)
		}

		if podVersion, ok := pod.Annotations[constants.CouchbaseVersionAnnotationKey]; !ok {
			return fmt.Errorf("expected pod (%s) to have %s annotation", pod.Name, constants.CouchbaseVersionAnnotationKey)
		} else if podVersion != expectedVersion {
			return fmt.Errorf("expected pod (%s) version to be %s but found %s", pod.Name, expectedVersion, podVersion)
		}
	}

	return nil
}
