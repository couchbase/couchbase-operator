/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package eventschema

import (
	"errors"
	"io/ioutil"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

// mustValidate validates an event list against a schema dying on error.
func mustValidate(t *testing.T, events []corev1.Event, schema Validatable) {
	v := Validator{Events: events, Schema: schema}
	if err := v.Validate(ioutil.Discard); err != nil {
		t.Fatal(err)
	}
}

// mustNotValidate validates an event list against a schema dying on success.
func mustNotValidate(t *testing.T, events []corev1.Event, schema Validatable, expectedErr error) {
	v := Validator{Events: events, Schema: schema}

	err := v.Validate(ioutil.Discard)
	if err == nil {
		t.Fatal(err)
	}

	if !errors.Is(err, expectedErr) {
		t.Fatal(err)
	}
}

// TestValidateEventReason tests reason only.
func TestValidateEventReason(t *testing.T) {
	events := []corev1.Event{
		{Reason: "mickey"},
	}
	schema := Event{Reason: "mickey"}
	mustValidate(t, events, schema)
}

// TestValidateEventMessage tests reason and messages match.
func TestValidateEventMessage(t *testing.T) {
	events := []corev1.Event{
		{Reason: "mickey", Message: "mouse"},
	}
	schema := Event{Reason: "mickey", Message: "mouse"}
	mustValidate(t, events, schema)
}

// TestValidateEventMessageFuzzy tests reason and messages match.
func TestValidateEventFuzzyMessage(t *testing.T) {
	events := []corev1.Event{
		{Reason: "mickey", Message: "is a mouse, he is married to minnie"},
	}
	schema := Event{Reason: "mickey", FuzzyMessage: "mouse"}
	mustValidate(t, events, schema)
}

// TestValidateEventMismatchReason tests mismatched reasons fail.
func TestValidateEventMismatchReason(t *testing.T) {
	events := []corev1.Event{
		{Reason: "mickey", Message: "mouse"},
	}
	schema := Event{Reason: "minnie", Message: "mouse"}
	mustNotValidate(t, events, schema, ErrReasonMismatch)
}

// TestValidateEventMismatchMessage tests mismatched messages fail.
func TestValidateEventMismatchMessage(t *testing.T) {
	events := []corev1.Event{
		{Reason: "mickey", Message: "mouse"},
	}
	schema := Event{Reason: "mickey", Message: "duck"}
	mustNotValidate(t, events, schema, ErrMessageMismatch)
}

// TestValidateEventMismatchFuzzyMessage tests mismatched messages fail.
func TestValidateEventMismatchFuzzyMessage(t *testing.T) {
	events := []corev1.Event{
		{Reason: "mickey", Message: "is a duck, he is married to daisy"},
	}
	schema := Event{Reason: "mickey", FuzzyMessage: "mouse"}
	mustNotValidate(t, events, schema, ErrFuzzyMessageMismatch)
}

// TestUnderflow tests we raise an underflow if many few events are present.
func TestUnderflow(t *testing.T) {
	events := []corev1.Event{
		{Reason: "mickey", Message: "mouse"},
	}
	schema := Sequence{}
	mustNotValidate(t, events, schema, ErrUnderflow)
}

// TestOverflow tests we raise an overflow if too few events are present.
func TestOverflow(t *testing.T) {
	events := []corev1.Event{}
	schema := Event{Reason: "mickey", FuzzyMessage: "mouse"}
	mustNotValidate(t, events, schema, ErrOverflow)
}

// TestRepeat tests repeated events match.
func TestRepeat(t *testing.T) {
	events := []corev1.Event{
		{Reason: "mickey"},
		{Reason: "mickey"},
	}
	schema := Repeat{Times: 2, Validator: Event{Reason: "mickey"}}
	mustValidate(t, events, schema)
}

// TestRepeatMismatch tests repeated mismatched events fail.
func TestRepeatMismatch(t *testing.T) {
	events := []corev1.Event{
		{Reason: "mickey"},
		{Reason: "donald"},
	}
	schema := Repeat{Times: 2, Validator: Event{Reason: "mickey"}}
	mustNotValidate(t, events, schema, ErrReasonMismatch)
}

// TestSequence tests sequences of events match.
func TestSequence(t *testing.T) {
	events := []corev1.Event{
		{Reason: "mickey"},
		{Reason: "donald"},
	}
	schema := Sequence{
		Validators: []Validatable{
			Event{Reason: "mickey"},
			Event{Reason: "donald"},
		},
	}
	mustValidate(t, events, schema)
}

// TestSequenceMismatch tests sequences of events mismatched events fail.
func TestSequenceMismatch(t *testing.T) {
	events := []corev1.Event{
		{Reason: "mickey"},
		{Reason: "donald"},
	}
	schema := Sequence{
		Validators: []Validatable{
			Event{Reason: "donald"},
			Event{Reason: "mickey"},
		},
	}
	mustNotValidate(t, events, schema, ErrReasonMismatch)
}

// TestSet tests sets of events match.
func TestSet(t *testing.T) {
	events := []corev1.Event{
		{Reason: "mickey"},
		{Reason: "donald"},
	}
	schema := Set{
		Validators: []Validatable{
			Event{Reason: "donald"},
			Event{Reason: "mickey"},
		},
	}
	mustValidate(t, events, schema)
}

// TestSetMissmatch tests sets of mismatched events fail.
func TestSetMissmatch(t *testing.T) {
	events := []corev1.Event{
		{Reason: "mickey"},
		{Reason: "donald"},
	}
	schema := Set{
		Validators: []Validatable{
			Event{Reason: "minnie"},
			Event{Reason: "mickey"},
		},
	}
	mustNotValidate(t, events, schema, ErrSetMismatch)
}

// TestAnyOf tests whether an event matches any allowed validator.
func TestAnyOf(t *testing.T) {
	events := []corev1.Event{
		{Reason: "mickey"},
	}
	schema := AnyOf{
		Validators: []Validatable{
			Event{Reason: "minnie"},
			Event{Reason: "mickey"},
		},
	}
	mustValidate(t, events, schema)
}

// TestAnyOfMismatch tests whether an event matches non of the allowed validators.
func TestAnyOfMismatch(t *testing.T) {
	events := []corev1.Event{
		{Reason: "mickey"},
	}
	schema := AnyOf{
		Validators: []Validatable{
			Event{Reason: "minnie"},
			Event{Reason: "donald"},
		},
	}
	mustNotValidate(t, events, schema, ErrAnyOf)
}

// TestOptionalMatch tests that events validate with an optional validator on match.
func TestOptionalMatch(t *testing.T) {
	events := []corev1.Event{
		{Reason: "mickey"},
	}
	schema := Optional{
		Validator: Event{Reason: "mickey"},
	}
	mustValidate(t, events, schema)
}

// TestOptionalMismatch tests that events validate with an optional validator on mismatch.
func TestOptionalMismatch(t *testing.T) {
	events := []corev1.Event{}
	schema := Optional{
		Validator: Event{Reason: "mickey"},
	}
	mustValidate(t, events, schema)
}
