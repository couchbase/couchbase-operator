package v2

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func TestAsMatcher(t *testing.T) {
	tests := []struct {
		name          string
		selector      *ObjectSelector
		expectErr     bool
		expectHasLbl  bool
		expectHasName bool
	}{
		{
			name:          "Nil selector",
			selector:      nil,
			expectErr:     false,
			expectHasLbl:  false,
			expectHasName: false,
		},
		{
			name:          "Empty selector",
			selector:      &ObjectSelector{},
			expectErr:     false,
			expectHasLbl:  false,
			expectHasName: false,
		},
		{
			name: "Valid labels only",
			selector: &ObjectSelector{
				MatchLabels: map[string]string{"env": "prod"},
			},
			expectErr:     false,
			expectHasLbl:  true,
			expectHasName: false,
		},
		{
			name: "Valid names only",
			selector: &ObjectSelector{
				MatchNames: []string{"exact-bucket", "^regex-.*"},
			},
			expectErr:     false,
			expectHasLbl:  false,
			expectHasName: true,
		},
		{
			name: "Both labels and names valid",
			selector: &ObjectSelector{
				MatchLabels: map[string]string{"env": "prod"},
				MatchNames:  []string{"my-bucket"},
			},
			expectErr:     false,
			expectHasLbl:  true,
			expectHasName: true,
		},
		{
			name: "Invalid regex in MatchNames returns error",
			selector: &ObjectSelector{
				MatchNames: []string{"["},
			},
			expectErr: true,
		},
		{
			name: "Invalid label expression returns error",
			selector: &ObjectSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "env",
						Operator: "InvalidOperator",
						Values:   []string{"prod"},
					},
				},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher, err := tt.selector.AsMatcher()

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

			if matcher.hasLabels != tt.expectHasLbl {
				t.Errorf("expected hasLabels=%v, got %v", tt.expectHasLbl, matcher.hasLabels)
			}
			if matcher.hasNames != tt.expectHasName {
				t.Errorf("expected hasNames=%v, got %v", tt.expectHasName, matcher.hasNames)
			}
		})
	}
}

// TestMatches verifies the Union (OR) logic for evaluating buckets.
func TestMatches(t *testing.T) {
	selector := &ObjectSelector{
		MatchLabels: map[string]string{"env": "prod"},
		MatchNames:  []string{"exact-bucket", "^regex-[0-9]+$"},
	}

	matcher, err := selector.AsMatcher()
	if err != nil {
		t.Fatalf("failed to compile valid selector for testing: %v", err)
	}

	emptyMatcher, _ := (*ObjectSelector)(nil).AsMatcher()

	tests := []struct {
		name          string
		matcher       *ObjectSelectorAsSelector
		bucketName    string
		bucketLabels  map[string]string
		expectMatches bool
	}{
		{
			name:          "Empty selector matches everything",
			matcher:       emptyMatcher,
			bucketName:    "random-bucket",
			bucketLabels:  map[string]string{"foo": "bar"},
			expectMatches: true,
		},
		{
			name:          "Matches exact name, labels don't match (OR logic)",
			matcher:       matcher,
			bucketName:    "exact-bucket",
			bucketLabels:  map[string]string{"env": "dev"},
			expectMatches: true,
		},
		{
			name:          "Matches regex name, labels don't match (OR logic)",
			matcher:       matcher,
			bucketName:    "regex-123",
			bucketLabels:  map[string]string{"env": "dev"},
			expectMatches: true,
		},
		{
			name:          "Name doesn't match, but labels match (OR logic)",
			matcher:       matcher,
			bucketName:    "unrelated-bucket-name",
			bucketLabels:  map[string]string{"env": "prod", "region": "east"},
			expectMatches: true,
		},
		{
			name:          "Both name and labels match",
			matcher:       matcher,
			bucketName:    "exact-bucket",
			bucketLabels:  map[string]string{"env": "prod"},
			expectMatches: true,
		},
		{
			name:          "Neither name nor labels match",
			matcher:       matcher,
			bucketName:    "wrong-bucket",
			bucketLabels:  map[string]string{"env": "dev"},
			expectMatches: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.matcher.Matches(tt.bucketName, tt.bucketLabels)
			if got != tt.expectMatches {
				t.Errorf("Matches() = %v, expected %v", got, tt.expectMatches)
			}
		})
	}
}

func TestToLabelSelector(t *testing.T) {
	tests := []struct {
		name     string
		selector *ObjectSelector
		expected *metav1.LabelSelector
	}{
		{
			name:     "Nil receiver returns nil",
			selector: nil,
			expected: nil,
		},
		{
			name:     "Empty struct returns nil",
			selector: &ObjectSelector{},
			expected: nil,
		},
		{
			name: "Only MatchNames returns nil (ignores names)",
			selector: &ObjectSelector{
				MatchNames: []string{"my-bucket"},
			},
			expected: nil,
		},
		{
			name: "MatchLabels populates correctly",
			selector: &ObjectSelector{
				MatchLabels: map[string]string{"env": "prod"},
			},
			expected: &metav1.LabelSelector{
				MatchLabels: map[string]string{"env": "prod"},
			},
		},
		{
			name: "MatchExpressions populates correctly",
			selector: &ObjectSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: "env", Operator: metav1.LabelSelectorOpIn, Values: []string{"prod", "dev"}},
				},
			},
			expected: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: "env", Operator: metav1.LabelSelectorOpIn, Values: []string{"prod", "dev"}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.selector.ToLabelSelector()
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("ToLabelSelector() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestGetBucketLabelSelector(t *testing.T) {
	tests := []struct {
		name          string
		cluster       *CouchbaseCluster
		expectErr     bool
		testLabels    labels.Set
		expectMatches bool
	}{
		{
			name: "Nil selector defaults to Everything",
			cluster: &CouchbaseCluster{
				Spec: ClusterSpec{},
			},
			testLabels:    labels.Set{"any-random-label": "true"},
			expectMatches: true,
		},
		{
			name: "Empty selector block defaults to Everything",
			cluster: &CouchbaseCluster{
				Spec: ClusterSpec{
					Buckets: Buckets{
						Selector: &ObjectSelector{},
					},
				},
			},
			testLabels:    labels.Set{"any-random-label": "true"},
			expectMatches: true,
		},
		{
			name: "Only MatchNames provided defaults to Nothing",
			cluster: &CouchbaseCluster{
				Spec: ClusterSpec{
					Buckets: Buckets{
						Selector: &ObjectSelector{
							MatchNames: []string{"my-bucket"},
						},
					},
				},
			},
			testLabels:    labels.Set{"env": "prod"},
			expectMatches: false,
		},
		{
			name: "MatchLabels correctly filters",
			cluster: &CouchbaseCluster{
				Spec: ClusterSpec{
					Buckets: Buckets{
						Selector: &ObjectSelector{
							MatchLabels: map[string]string{"env": "prod"},
						},
					},
				},
			},
			testLabels:    labels.Set{"env": "prod", "region": "east"},
			expectMatches: true,
		},
		{
			name: "MatchLabels correctly rejects mismatches",
			cluster: &CouchbaseCluster{
				Spec: ClusterSpec{
					Buckets: Buckets{
						Selector: &ObjectSelector{
							MatchLabels: map[string]string{"env": "prod"},
						},
					},
				},
			},
			testLabels:    labels.Set{"env": "dev"},
			expectMatches: false,
		},
		{
			name: "Invalid Label Expression throws error",
			cluster: &CouchbaseCluster{
				Spec: ClusterSpec{
					Buckets: Buckets{
						Selector: &ObjectSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{Key: "env", Operator: "InvalidOp", Values: []string{"prod"}},
							},
						},
					},
				},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel, err := tt.cluster.GetBucketLabelSelector()

			if tt.expectErr {
				if err == nil {
					t.Errorf("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("did not expect an error, got: %v", err)
				return
			}

			// Validate the behavior of the returned selector
			matched := sel.Matches(tt.testLabels)
			if matched != tt.expectMatches {
				t.Errorf("Selector matched=%v, expected=%v for labels %v", matched, tt.expectMatches, tt.testLabels)
			}
		})
	}
}

// TestGetBucketObjectSelector verifies the cluster method successfully delegates
// to the AsMatcher compilation function.
func TestGetBucketObjectSelector(t *testing.T) {
	tests := []struct {
		name          string
		cluster       *CouchbaseCluster
		expectErr     bool
		expectHasLbl  bool
		expectHasName bool
	}{
		{
			name: "Nil selector delegates cleanly",
			cluster: &CouchbaseCluster{
				Spec: ClusterSpec{},
			},
			expectErr:     false,
			expectHasLbl:  false,
			expectHasName: false,
		},
		{
			name: "Valid selector compiles",
			cluster: &CouchbaseCluster{
				Spec: ClusterSpec{
					Buckets: Buckets{
						Selector: &ObjectSelector{
							MatchLabels: map[string]string{"env": "prod"},
							MatchNames:  []string{"^bucket-.*"},
						},
					},
				},
			},
			expectErr:     false,
			expectHasLbl:  true,
			expectHasName: true,
		},
		{
			name: "Invalid regex surfaces error",
			cluster: &CouchbaseCluster{
				Spec: ClusterSpec{
					Buckets: Buckets{
						Selector: &ObjectSelector{
							MatchNames: []string{"["},
						},
					},
				},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher, err := tt.cluster.GetBucketObjectSelector()

			if tt.expectErr {
				if err == nil {
					t.Errorf("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("did not expect an error, got: %v", err)
				return
			}

			if matcher.hasLabels != tt.expectHasLbl {
				t.Errorf("expected hasLabels=%v, got %v", tt.expectHasLbl, matcher.hasLabels)
			}
			if matcher.hasNames != tt.expectHasName {
				t.Errorf("expected hasNames=%v, got %v", tt.expectHasName, matcher.hasNames)
			}
		})
	}
}
