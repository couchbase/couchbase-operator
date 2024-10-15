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

func TestUnmarshalMaxTTLWithNegativeOverride(t *testing.T) {
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
		err := output.UnmarshalMaxTTLWithNegativeOverride([]byte(testcase.input))

		if testcase.shouldFail {
			if err == nil {
				t.Errorf("expected json unmarshal of %v to error, but got none", testcase.input)
			}
		} else if err != nil || !cmp.Equal(testcase.expected, output.MaxTTL) {
			t.Errorf("unable to json unmarshal %v correctly, expected %v but got %v, error: %v", testcase.input, testcase.expected, output.MaxTTL, err)
		}
	}
}

func TestCouchbaseCollectionSpecMarshalJSON(t *testing.T) {
	testcases := []struct {
		input    couchbasev2.CouchbaseCollectionSpec
		expected string
	}{
		{
			input:    couchbasev2.CouchbaseCollectionSpec{Name: "testName", CouchbaseCollectionSpecCommon: collectionSpecCommon(true, 5*time.Minute)},
			expected: `{"name":"testName","history":true,"maxTTL":"5m0s"}`,
		},
		{
			input:    couchbasev2.CouchbaseCollectionSpec{},
			expected: `{}`,
		},
		{
			input:    couchbasev2.CouchbaseCollectionSpec{CouchbaseCollectionSpecCommon: collectionSpecCommon(false, -1*time.Second)},
			expected: `{"history":false,"maxTTL":"-1"}`,
		},
		{
			input:    couchbasev2.CouchbaseCollectionSpec{Name: "testName"},
			expected: `{"name":"testName"}`,
		},
	}

	for _, testcase := range testcases {
		output, err := testcase.input.MarshalJSON()

		if err != nil {
			t.Errorf("unexpected err when marshaling maxTTL: %v", err)
		} else if string(output) != testcase.expected {
			t.Errorf("expected json marshal to be %v but got %v", testcase.expected, string(output))
		}
	}
}

func TestCouchbaseCollectionSpecUnmarshalJSON(t *testing.T) {
	testcases := []struct {
		input      string
		expected   couchbasev2.CouchbaseCollectionSpec
		shouldFail bool
	}{
		{
			input:      `{name`,
			shouldFail: true,
		},
		{
			input:      `{"history":"notABoolean"}`,
			shouldFail: true,
		},
		{
			input:    `{"name":"testName","history":true,"maxTTL":"10m0s"}`,
			expected: couchbasev2.CouchbaseCollectionSpec{Name: "testName", CouchbaseCollectionSpecCommon: collectionSpecCommon(true, 10*time.Minute)},
		},
		{
			input:    `{}`,
			expected: couchbasev2.CouchbaseCollectionSpec{CouchbaseCollectionSpecCommon: couchbasev2.CouchbaseCollectionSpecCommon{MaxTTLWithNegativeOverride: couchbasev2.MaxTTLWithNegativeOverride{MaxTTL: &metav1.Duration{Duration: 0}}}},
		},
		{
			input:    `{"history":false,"maxTTL":"-1"}`,
			expected: couchbasev2.CouchbaseCollectionSpec{CouchbaseCollectionSpecCommon: collectionSpecCommon(false, -1*time.Second)},
		},
		{
			input:    `{"name":"testName"}`,
			expected: couchbasev2.CouchbaseCollectionSpec{Name: "testName", CouchbaseCollectionSpecCommon: couchbasev2.CouchbaseCollectionSpecCommon{MaxTTLWithNegativeOverride: couchbasev2.MaxTTLWithNegativeOverride{MaxTTL: &metav1.Duration{Duration: 0}}}},
		},
	}

	for _, testcase := range testcases {
		var output couchbasev2.CouchbaseCollectionSpec
		err := output.UnmarshalJSON([]byte(testcase.input))

		if testcase.shouldFail {
			if err == nil {
				t.Errorf("expected json unmarshal of %v to error, but got none", testcase.input)
			}
		} else if err != nil || !cmp.Equal(testcase.expected, output) {
			t.Errorf("unable to json unmarshal %v correctly, expected %v but got %v, error: %v", testcase.input, testcase.expected, output.MaxTTL, err)
		}
	}
}

func TestCouchbaseCollectionGroupSpecMarshalJSON(t *testing.T) {
	testcases := []struct {
		input    couchbasev2.CouchbaseCollectionGroupSpec
		expected string
	}{
		{
			input:    couchbasev2.CouchbaseCollectionGroupSpec{Names: []couchbasev2.ScopeOrCollectionName{"testName", "anotherName"}, CouchbaseCollectionSpecCommon: collectionSpecCommon(true, 10*time.Minute)},
			expected: `{"names":["testName","anotherName"],"history":true,"maxTTL":"10m0s"}`,
		},
		{
			expected: `{"names":null}`,
			input:    couchbasev2.CouchbaseCollectionGroupSpec{},
		},
		{
			input:    couchbasev2.CouchbaseCollectionGroupSpec{CouchbaseCollectionSpecCommon: collectionSpecCommon(false, -1*time.Second)},
			expected: `{"names":null,"history":false,"maxTTL":"-1"}`,
		},
		{
			input:    couchbasev2.CouchbaseCollectionGroupSpec{Names: []couchbasev2.ScopeOrCollectionName{"testName", "anotherName"}},
			expected: `{"names":["testName","anotherName"]}`,
		},
	}

	for _, testcase := range testcases {
		output, err := testcase.input.MarshalJSON()

		if err != nil {
			t.Errorf("unexpected err when marshaling maxTTL: %v", err)
		} else if string(output) != testcase.expected {
			t.Errorf("expected json marshal to be %v but got %v", testcase.expected, string(output))
		}
	}
}

func TestCouchbaseCollectionGroupSpecUnmarshalJSON(t *testing.T) {
	testcases := []struct {
		input      string
		expected   couchbasev2.CouchbaseCollectionGroupSpec
		shouldFail bool
	}{
		{
			input:      `{name`,
			shouldFail: true,
		},
		{
			input:      `{"history":"notABoolean"}`,
			shouldFail: true,
		},
		{
			input:    `{"names":["testName","anotherName"],"history":true,"maxTTL":"10m0s"}`,
			expected: couchbasev2.CouchbaseCollectionGroupSpec{Names: []couchbasev2.ScopeOrCollectionName{"testName", "anotherName"}, CouchbaseCollectionSpecCommon: collectionSpecCommon(true, 10*time.Minute)},
		},
		{
			input:    `{}`,
			expected: couchbasev2.CouchbaseCollectionGroupSpec{CouchbaseCollectionSpecCommon: couchbasev2.CouchbaseCollectionSpecCommon{MaxTTLWithNegativeOverride: couchbasev2.MaxTTLWithNegativeOverride{MaxTTL: &metav1.Duration{Duration: 0}}}},
		},
		{
			input:    `{"history":false,"maxTTL":"-1"}`,
			expected: couchbasev2.CouchbaseCollectionGroupSpec{CouchbaseCollectionSpecCommon: collectionSpecCommon(false, -1*time.Second)},
		},
		{
			input:    `{"names":["testName","anotherName"]}`,
			expected: couchbasev2.CouchbaseCollectionGroupSpec{Names: []couchbasev2.ScopeOrCollectionName{"testName", "anotherName"}, CouchbaseCollectionSpecCommon: couchbasev2.CouchbaseCollectionSpecCommon{MaxTTLWithNegativeOverride: couchbasev2.MaxTTLWithNegativeOverride{MaxTTL: &metav1.Duration{Duration: 0}}}},
		},
	}

	for _, testcase := range testcases {
		var output couchbasev2.CouchbaseCollectionGroupSpec
		err := output.UnmarshalJSON([]byte(testcase.input))

		if testcase.shouldFail {
			if err == nil {
				t.Errorf("expected json unmarshal of %v to error, but got none", testcase.input)
			}
		} else if err != nil || !cmp.Equal(testcase.expected, output) {
			t.Errorf("unable to json unmarshal %v correctly, expected %v but got %v, error: %v", testcase.input, testcase.expected, output.MaxTTL, err)
		}
	}
}

func collectionSpecCommon(history bool, maxTTL time.Duration) couchbasev2.CouchbaseCollectionSpecCommon {
	return couchbasev2.CouchbaseCollectionSpecCommon{History: boolPtr(history), MaxTTLWithNegativeOverride: couchbasev2.MaxTTLWithNegativeOverride{MaxTTL: &metav1.Duration{Duration: maxTTL}}}
}

func boolPtr(b bool) *bool {
	return &b
}
