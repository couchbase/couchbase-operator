package eventschema_test

import (
	"os"

	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"

	corev1 "k8s.io/api/core/v1"
)

func ExampleValidator_Validate() {
	// In a perfect world this is what I'd expect QE to look like, we don't care about the
	// minute details, just that things happen heuristically based on expectations and
	// variable behaviours of Couchbase server

	events := []corev1.Event{
		{Reason: "NewMemberAdded", Message: "New member test-couchbase-jsstw-0000 added to cluster"},
		{Reason: "NewMemberAdded", Message: "New member test-couchbase-jsstw-0001 added to cluster"},
		{Reason: "NewMemberAdded", Message: "New member test-couchbase-jsstw-0002 added to cluster"},
		{Reason: "NewMemberAdded", Message: "New member test-couchbase-jsstw-0003 added to cluster"},
		{Reason: "NewMemberAdded", Message: "New member test-couchbase-jsstw-0004 added to cluster"},
		{Reason: "RebalanceStarted", Message: "A rebalance has been started to balance data across the cluster"},
		{Reason: "RebalanceCompleted", Message: "A rebalance has completed"},
		{Reason: "MemberDown", Message: "Existing member test-couchbase-jsstw-0004 down"},
		{Reason: "MemberDown", Message: "Existing member test-couchbase-jsstw-0002 down"},
		// Nodes go down in an unexpected order...
		{Reason: "MemberFailedOver", Message: "Existing member test-couchbase-jsstw-0002 failed over"},
		{Reason: "MemberFailedOver", Message: "Existing member test-couchbase-jsstw-0004 failed over"},
		// We change the scheme to generate pod names because it's stateless
		{Reason: "NewMemberAdded", Message: "New member test-couchbase-jsstw-ks72f added to cluster"},
		{Reason: "NewMemberAdded", Message: "New member test-couchbase-jsstw-sl2ep added to cluster"},
		{Reason: "RebalanceStarted", Message: "A rebalance has been started to balance data across the cluster"},
		{Reason: "RebalanceIncomplete", Message: "A rebalance is incomplete"},
		// A race condition causes one of many outcomes...
		{Reason: "MemberDown", Message: "Existing member test-couchbase-jsstw-ks72f down"},
		{Reason: "MemberFailedOver", Message: "Existing member test-couchbase-jsstw-ks72f failed over"},
		{Reason: "FailedAddNode", Message: "Removed existing member test-couchbase-jsstw-sl2ep because it failed before it could be added to the cluster"},
	}

	schema := eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Repeat{Times: 5, Validator: eventschema.Event{Reason: "NewMemberAdded"}},
			eventschema.Event{Reason: "RebalanceStarted"},
			eventschema.Event{Reason: "RebalanceCompleted"},
			eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: "MemberDown"}},
			eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: "MemberFailedOver"}},
			eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: "NewMemberAdded"}},
			eventschema.Event{Reason: "RebalanceStarted"},
			eventschema.Event{Reason: "RebalanceIncomplete"},
			eventschema.AnyOf{
				Validators: []eventschema.Validatable{
					eventschema.Sequence{
						Validators: []eventschema.Validatable{
							eventschema.Repeat{Times: 2, Validator: &eventschema.Event{Reason: "MemberDown"}},
							eventschema.Repeat{Times: 2, Validator: &eventschema.Event{Reason: "MemberFailedOver"}},
						},
					},
					eventschema.Sequence{
						Validators: []eventschema.Validatable{
							eventschema.Event{Reason: "MemberDown"},
							eventschema.Event{Reason: "MemberFailedOver"},
							eventschema.Event{Reason: "FailedAddNode"},
						},
					},
				},
			},
		},
	}

	v := &eventschema.Validator{Events: events, Schema: schema}
	_ = v.Validate(os.Stdout)

	// Output:
}

func ExampleValidator_Validate_strict() {
	// A strict validation eventschema which checks explicit messages.  This is more correct but
	// suffers from the problem that messages need to remain the same or validation fails.

	events := []corev1.Event{
		{Reason: "NewMemberAdded", Message: "New member test-couchbase-jsstw-0000 added to cluster"},
		{Reason: "NewMemberAdded", Message: "New member test-couchbase-jsstw-0001 added to cluster"},
		{Reason: "NewMemberAdded", Message: "New member test-couchbase-jsstw-0002 added to cluster"},
		{Reason: "NewMemberAdded", Message: "New member test-couchbase-jsstw-0003 added to cluster"},
		{Reason: "NewMemberAdded", Message: "New member test-couchbase-jsstw-0004 added to cluster"},
		{Reason: "RebalanceStarted", Message: "A rebalance has been started to balance data across the cluster"},
		{Reason: "RebalanceCompleted", Message: "A rebalance has completed"},
		{Reason: "MemberDown", Message: "Existing member test-couchbase-jsstw-0004 down"},
		{Reason: "MemberDown", Message: "Existing member test-couchbase-jsstw-0002 down"},
		// Nodes go down in an unexpected order...
		{Reason: "MemberFailedOver", Message: "Existing member test-couchbase-jsstw-0002 failed over"},
		{Reason: "MemberFailedOver", Message: "Existing member test-couchbase-jsstw-0004 failed over"},
		// We change the scheme to generate pod names because it's stateless
		{Reason: "NewMemberAdded", Message: "New member test-couchbase-jsstw-ks72f added to cluster"},
		{Reason: "NewMemberAdded", Message: "New member test-couchbase-jsstw-sl2ep added to cluster"},
		{Reason: "RebalanceStarted", Message: "A rebalance has been started to balance data across the cluster"},
		{Reason: "RebalanceIncomplete", Message: "A rebalance is incomplete"},
		// A race condition causes one of many outcomes...
		{Reason: "MemberDown", Message: "Existing member test-couchbase-jsstw-ks72f down"},
		{Reason: "MemberFailedOver", Message: "Existing member test-couchbase-jsstw-ks72f failed over"},
		{Reason: "FailedAddNode", Message: "Removed existing member test-couchbase-jsstw-sl2ep because it failed before it could be added to the cluster"},
	}

	schema := &eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Event{
				Reason:  "NewMemberAdded",
				Message: "New member test-couchbase-jsstw-0000 added to cluster",
			},
			eventschema.Event{
				Reason:  "NewMemberAdded",
				Message: "New member test-couchbase-jsstw-0001 added to cluster",
			},
			eventschema.Event{
				Reason:  "NewMemberAdded",
				Message: "New member test-couchbase-jsstw-0002 added to cluster",
			},
			eventschema.Event{
				Reason:  "NewMemberAdded",
				Message: "New member test-couchbase-jsstw-0003 added to cluster",
			},
			eventschema.Event{
				Reason:  "NewMemberAdded",
				Message: "New member test-couchbase-jsstw-0004 added to cluster",
			},
			// I don't care about the message here, too hard-coded and error prone for my liking...
			eventschema.Event{
				Reason: "RebalanceStarted",
			},
			eventschema.Event{
				Reason: "RebalanceCompleted",
			},
			// I don't care about the order here, this is a race condition...
			eventschema.Set{
				Validators: []eventschema.Validatable{
					eventschema.Event{
						Reason:  "MemberDown",
						Message: "Existing member test-couchbase-jsstw-0002 down",
					},
					eventschema.Event{
						Reason:  "MemberDown",
						Message: "Existing member test-couchbase-jsstw-0004 down",
					},
				},
			},
			eventschema.Event{
				Reason:  "MemberFailedOver",
				Message: "Existing member test-couchbase-jsstw-0002 failed over",
			},
			eventschema.Event{
				Reason:  "MemberFailedOver",
				Message: "Existing member test-couchbase-jsstw-0004 failed over",
			},
			// Ah the naming changed, oh well, I care that we get two replacements at least
			eventschema.Repeat{
				Times: 2,
				Validator: eventschema.Event{
					Reason: "NewMemberAdded",
				},
			},
			// Still don't care about the messages
			eventschema.Event{
				Reason: "RebalanceStarted",
			},
			eventschema.Event{
				Reason: "RebalanceIncomplete",
			},
			// And more complex race conditions are a piece of cake...
			eventschema.AnyOf{
				Validators: []eventschema.Validatable{
					// I expect two nodes down then fail over, ordering is racy...
					eventschema.Sequence{
						Validators: []eventschema.Validatable{
							eventschema.Repeat{
								Times: 2,
								Validator: eventschema.Event{
									Reason: "MemberDown",
								},
							},
							eventschema.Repeat{
								Times: 2,
								Validator: eventschema.Event{
									Reason: "MemberFailedOver",
								},
							},
						},
					},
					// ...But we may see something different 1 in 5 times
					eventschema.Sequence{
						Validators: []eventschema.Validatable{
							eventschema.Event{
								Reason: "MemberDown",
							},
							eventschema.Event{
								Reason: "MemberFailedOver",
							},
							eventschema.Event{
								Reason: "FailedAddNode",
							},
						},
					},
				},
			},
		},
	}

	v := &eventschema.Validator{Events: events, Schema: schema}
	_ = v.Validate(os.Stdout)

	// Output:
}
