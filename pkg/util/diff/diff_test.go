package diff

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPrettyDiff(t *testing.T) {
	expectedDiffString := "+{v1.Pod}.TypeMeta.Kind:hello;{v1.Pod}.TypeMeta.APIVersion:V1->V2;+{v1.Pod}.ObjectMeta.Labels:map[app:njr];+{v1.Pod}.ObjectMeta.Finalizers[?->1]:f2"

	pod1 := v1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "V1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"f1"},
		},
	}
	pod2 := v1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "hello",
			APIVersion: "V2",
		},
		ObjectMeta: metav1.ObjectMeta{
			Labels:     map[string]string{"app": "njr"},
			Finalizers: []string{"f1", "f2"},
		},
	}

	if diffString := PrettyDiff(pod1, pod2); diffString != expectedDiffString {
		t.Errorf("expected diff to be: %s but got: %s", expectedDiffString, diffString)
	}
}
