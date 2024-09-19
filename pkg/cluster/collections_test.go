package cluster

import (
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/annotations"
	"github.com/google/go-cmp/cmp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCollectionHistoryAnnotation(t *testing.T) {
	collection := couchbasev2.CouchbaseCollection{}

	collection.Annotations = make(map[string]string)
	collection.Annotations["cao.couchbase.com/history"] = "true"

	err := annotations.Populate(&collection.Spec, collection.Annotations)
	if err != nil {
		t.Fatalf("unexpected err %v", err)
	}

	if !*(collection.Spec.History) {
		t.Fatal("history not set to true")
	}

	collection.Spec = couchbasev2.CouchbaseCollectionSpec{}
	collection.Annotations["cao.couchbase.com/history"] = "false"

	err = annotations.Populate(&collection.Spec, collection.Annotations)
	if err != nil {
		t.Fatalf("unexpected err %v", err)
	}

	if *(collection.Spec.History) {
		t.Fatal("history not set to false")
	}
}

func TestMaxTTLWithNegativeOverrideUnmarshalJSON(t *testing.T) {
	testcases := []struct {
		input      string
		expected   *metav1.Duration
		shouldFail bool
	}{
		{
			input:      `{"maxTTL":`,
			shouldFail: true,
		},
		{
			input:      `{"maxTTL": "invalid-time"}`,
			shouldFail: true,
		},
		{
			input: `{"maxTTL": "-1"}`,
			expected: &metav1.Duration{
				Duration: -1 * time.Second,
			},
		},
		{
			input: `{"maxTTL": "5h2m"}`,
			expected: &metav1.Duration{
				Duration: 5*time.Hour + 2*time.Minute,
			},
		},
		{
			input:      `{"maxTTL": ""}`,
			shouldFail: true,
		},
		{
			input: `{}`,
			expected: &metav1.Duration{
				Duration: 0 & time.Second,
			},
		},
		{
			input:      "",
			shouldFail: true,
		},
	}

	for _, testcase := range testcases {
		var output couchbasev2.MaxTTLWithNegativeOverride
		err := output.UnmarshalJSON([]byte(testcase.input))

		if testcase.shouldFail {
			if err == nil {
				t.Errorf("expected json unmarshal of %v to error, but got none", testcase.input)
			}
		} else if err != nil || !cmp.Equal(testcase.expected, output.MaxTTL) {
			t.Errorf("unable to json unmarshal %v correctly, expected %v but got %v, error: %v", testcase.input, testcase.expected, output.MaxTTL, err)
		}
	}
}

func TestMaxTTLWithNegativeOverrideMarshalJSON(t *testing.T) {
	testcases := []struct {
		input    metav1.Duration
		expected string
	}{
		{
			input:    metav1.Duration{Duration: 10 * time.Minute},
			expected: `{"maxTTL":"10m0s"}`,
		},
		{
			input:    metav1.Duration{Duration: -1 * time.Second},
			expected: `{"maxTTL":"-1"}`,
		},
		{
			input:    metav1.Duration{Duration: 0},
			expected: `{"maxTTL":"0s"}`,
		},
	}

	for _, testcase := range testcases {
		var input = couchbasev2.MaxTTLWithNegativeOverride{MaxTTL: &testcase.input}
		output, err := input.MarshalJSON()

		if err != nil {
			t.Errorf("unexpected err when marshaling maxTTL: %v", err)
		} else if string(output) != testcase.expected {
			t.Errorf("expected json marshal to be %v but got %v", testcase.expected, string(output))
		}
	}
}
