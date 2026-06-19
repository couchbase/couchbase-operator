package cluster

import (
	"reflect"
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type TestObject struct {
	Name   string
	Labels map[string]string
}

func (d *TestObject) GetName() string {
	return d.Name
}

func (d *TestObject) GetLabels() map[string]string {
	return d.Labels
}

func TestListFilteredByObjectSelector(t *testing.T) {
	// Setup test items
	item1 := &TestObject{Name: "bucket-1", Labels: map[string]string{"env": "prod", "region": "us-east"}}
	item2 := &TestObject{Name: "bucket-2", Labels: map[string]string{"env": "dev"}}
	item3 := &TestObject{Name: "backup-bucket", Labels: map[string]string{"env": "prod"}}
	item4 := &TestObject{Name: "cache-node", Labels: map[string]string{"type": "cache"}}

	items := []*TestObject{item1, item2, item3, item4, nil}

	tests := []struct {
		name      string
		selector  *couchbasev2.ObjectSelector
		expected  []TestObject
		expectErr bool
	}{
		{
			name:     "Nil selector returns all non-nil items",
			selector: nil,
			expected: []TestObject{*item1, *item2, *item3, *item4},
		},
		{
			name:     "Empty selector returns all non-nil items",
			selector: &couchbasev2.ObjectSelector{},
			expected: []TestObject{*item1, *item2, *item3, *item4},
		},
		{
			name: "Match exact name",
			selector: &couchbasev2.ObjectSelector{
				MatchNames: []string{"bucket-2"},
			},
			expected: []TestObject{*item2},
		},
		{
			name: "Match valid regex",
			selector: &couchbasev2.ObjectSelector{
				MatchNames: []string{"^backup-.*"},
			},
			expected: []TestObject{*item3},
		},
		{
			name: "error if an invalid and valid regex are found",
			selector: &couchbasev2.ObjectSelector{
				MatchNames: []string{"[", "^cache-.*"},
			},
			expectErr: true,
		},
		{
			name: "All regexes invalid, returns error",
			selector: &couchbasev2.ObjectSelector{
				MatchNames:  []string{"["},
				MatchLabels: map[string]string{"env": "dev"},
			},
			expectErr: true,
		},
		{
			name: "All regexes invalid, no labels specified, returns error",
			selector: &couchbasev2.ObjectSelector{
				MatchNames: []string{"["},
			},
			expectErr: true,
		},
		{
			name: "Match labels only",
			selector: &couchbasev2.ObjectSelector{
				MatchLabels: map[string]string{"env": "prod"},
			},
			expected: []TestObject{*item1, *item3},
		},
		{
			name: "Union OR match (Match name OR Match label)",
			selector: &couchbasev2.ObjectSelector{
				MatchNames:  []string{"cache-node"},
				MatchLabels: map[string]string{"region": "us-east"},
			},
			expected: []TestObject{*item1, *item4},
		},
		{
			name: "Invalid label selector operator returns error",
			selector: &couchbasev2.ObjectSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "env",
						Operator: "InvalidOperator", // Fails Kubernetes label parsing
						Values:   []string{"prod"},
					},
				},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := listFilteredByObjectSelector(items, tt.selector)

			if tt.expectErr {
				if err == nil {
					t.Errorf("expected an error, but got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("did not expect an error, but got: %v", err)
				return
			}

			if len(result) == 0 && len(tt.expected) == 0 {
				return
			}

			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("result mismatch.\nGot: %+v\nExpected: %+v", result, tt.expected)
			}
		})
	}
}
func TestReturnAll(t *testing.T) {
	item1 := &TestObject{Name: "item-1"}
	item2 := &TestObject{Name: "item-2"}

	items := []*TestObject{item1, nil, item2}

	expected := []TestObject{*item1, *item2}

	result := returnAll(items)

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("returnAll failed.\nGot: %+v\nExpected: %+v", result, expected)
	}
}
