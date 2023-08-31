// Package eventschema allows flexible checking of k8s event streams via a schema like interface.
//
// Previous attempts to solve this problem checked whether two lists were equal.  There were a
// number of drawbacks with this approach.  It was very verbose, you had to define every event.
// It was too specific, tying tests to the exact message was too prone to error if a message
// changed.  It didn't handle non-determinism.
//
// This event schema package aims to provide a more concise and heuristic approach to validating
// event streams are acceptable.  It allows a single event (or other validator) to match multiple
// times reducing code. It allows events to match regardless of message, meaning a single event
// can match multiple times regardless of message, and also allows fuzzy matching, so if you expect
// a single object to be referenced in a message you need not specify the entire string.  Finally
// it supports high level constructs to support non-determinism; sets handle when events may
// be delivered in a random order, and any-of validators allow one-of-many validations to be
// accepted when the event reasons vary based on race conditions.
package eventschema

import (
	"fmt"
	"io"
	"regexp"

	corev1 "k8s.io/api/core/v1"
)

// Validator records state during event validation.
type Validator struct {
	// Events is a list of Kubernetes events.
	Events []corev1.Event
	// Schema is a hierarchy of Event validators.
	Schema Validatable
	// index is an index into the events list.
	index int
}

// Validate checks the Events stream against the specified schema.
func (c *Validator) Validate(out io.Writer) error {
	err := c.Schema.Validate(c)

	if err == nil && c.index < len(c.Events) {
		err = ErrUnderflow
	}

	if err != nil {
		// Calculate the widths for tabulation
		reasonWidth := 0
		messageWidth := 0

		for _, event := range c.Events {
			if w := len(event.Reason); w > reasonWidth {
				reasonWidth = w
			}

			if w := len(event.Message); w > messageWidth {
				messageWidth = w
			}
		}

		// Generate the format string
		format := fmt.Sprintf("| %%-%ds | %%-%ds |", reasonWidth, messageWidth)

		// Print out the event list with the error embedded in the correct place
		if _, err := out.Write([]byte("Event schema validation failed:\n")); err != nil {
			return err
		}

		for index, event := range c.Events {
			line := fmt.Sprintf(format, event.Reason, event.Message)
			if index == c.index {
				line += fmt.Sprintf(" <== %v", err)
			}

			line += "\n"

			if _, err := out.Write([]byte(line)); err != nil {
				return err
			}
		}

		// If we are expecting more events than actually happened, then
		// tell us about it.
		if c.index >= len(c.Events) {
			line := fmt.Sprintf(format, "", "")
			line += fmt.Sprintf(" <== %v", err)
			line += "\n"

			if _, err := out.Write([]byte(line)); err != nil {
				return err
			}
		}
	}

	return err
}

// Validatable is the abstract interface used to process event streams.
type Validatable interface {
	// Validate processes the event type and returns an error on mismatch.
	Validate(*Validator) error
}

// Event represents a single event.
type Event struct {
	// Reason is a short reason for the event.  This property is required.
	Reason string
	// Message is a more verbose message explaining the event. This property is
	// optional.
	Message string
	// FuzzyMessage like Message examines the detailed message attached to the
	// reason.  However the contents are matched via regular expression.  This
	// allows the message to match on specific substrings, so rather than checking
	// the entire content, which is liable to change, it can be checked for a
	// specific named entity for example.  This property is optional.
	FuzzyMessage string
}

// Validate checks the event type and optionally message match.  The message
// is ignored if the zero value.
func (e Event) Validate(c *Validator) error {
	if c.index >= len(c.Events) {
		return ErrOverflow
	}

	if e.Reason != c.Events[c.index].Reason {
		return fmt.Errorf("%w: expected %s, got %s", ErrReasonMismatch, e.Reason, c.Events[c.index].Reason)
	}

	if e.Message != "" && e.Message != c.Events[c.index].Message {
		return fmt.Errorf("%w: expected %s, got %s", ErrMessageMismatch, e.Reason, c.Events[c.index].Reason)
	}

	if e.FuzzyMessage != "" && !regexp.MustCompile(e.FuzzyMessage).MatchString(c.Events[c.index].Message) {
		return fmt.Errorf("%w: expected %s, got %s", ErrFuzzyMessageMismatch, e.Reason, c.Events[c.index].Reason)
	}

	c.index++

	return nil
}

// Repeat represents an event validation which we expect to occur multiple times.
type Repeat struct {
	// Times tells us how many times we expect to match
	Times int
	// Event is an event validator
	Validator Validatable
}

// Validate checks the given event occurs the specified number of times.
func (e Repeat) Validate(c *Validator) error {
	for i := 0; i < e.Times; i++ {
		if err := e.Validator.Validate(c); err != nil {
			return err
		}
	}

	return nil
}

// Sequence represents an ordered list of event validators which are processed in order.
type Sequence struct {
	// Validators is a list of abstract event types
	Validators []Validatable
}

// Validate checks each event in the list in order, breaking on error.
func (e Sequence) Validate(c *Validator) error {
	for _, validator := range e.Validators {
		if err := validator.Validate(c); err != nil {
			return err
		}
	}

	return nil
}

// Set represents an unordered set of event validators which must all validate
// but in any order.
type Set struct {
	// Validators is a list of abstract event validators
	Validators []Validatable
}

// Validate checks each event in the list until success, then removes the event.
// If none is found then an error is raised.  This process continues until each
// event has been processed.
func (e Set) Validate(c *Validator) error {
	// Create a list of validator indices to attempt
	indices := []int{}
	for i := 0; i < len(e.Validators); i++ {
		indices = append(indices, i)
	}

	// Continue until there are no more validators left
	for len(indices) > 0 {
		// Look for a matching validator with an exhaustive search
		matched := false

		for i, index := range indices {
			// If there is a match indicate so and remove the matching index
			if err := e.Validators[index].Validate(c); err == nil {
				matched = true

				indices = append(indices[0:i], indices[i+1:]...)

				break
			}
		}

		if !matched {
			return ErrSetMismatch
		}
	}

	return nil
}

// AnyOf represents a set of possible validations.  If any of them validate
// successfully we break out and report success.
type AnyOf struct {
	// Validators is a list of abstract event validators
	Validators []Validatable
}

// Validate checks that any of the supplied validations passes.  The first to do so
// is chosen and this is reflected in the context index.  If the current validation
// fails we reset the context index and try the next, if all fail we report an error.
func (e AnyOf) Validate(c *Validator) error {
	// Remember the index for resetting
	index := c.index

	for _, validator := range e.Validators {
		// Validates, leave the index updated and return success
		if err := validator.Validate(c); err == nil {
			return nil
		}

		// Roll back the index and try again
		c.index = index
	}

	return ErrAnyOf
}

// Optional represents a validator that may happen.  This is useful for situations
// where behaviour could be observed, or it happens too quickly to be observed.
type Optional struct {
	Validator Validatable
}

// Validate checks that the given validator validates.  If it does then the pointer
// is moved forward.  If it is not it is left where it is and the next validator is
// processed.
func (e Optional) Validate(c *Validator) error {
	index := c.index

	if err := e.Validator.Validate(c); err == nil {
		return nil
	}

	// Roll back the index
	c.index = index

	return nil
}

// RepeatAtLeast is a greedy validator that expects to validate at least N times
// up to any number.
type RepeatAtLeast struct {
	Times     int
	Validator Validatable
}

// Validate matches the validator as many times as it can.  If the total number of
// matches is less than the required minimum then throw an error.
func (e RepeatAtLeast) Validate(c *Validator) error {
	var times int

	for {
		if err := e.Validator.Validate(c); err != nil {
			break
		}

		times++
	}

	if times < e.Times {
		return fmt.Errorf("%w: failed to validate at least %d times", ErrRepeatAtLeast, e.Times)
	}

	return nil
}

// RepeatAtMost is a greedy validator that expects at least one, and fewer than N
// matches.
type RepeatAtMost struct {
	Times     int
	Validator Validatable
}

// Validate matches the validator as many times as it can.  If no matches were found
// and more than the required maximum then an error is raised.
func (e RepeatAtMost) Validate(c *Validator) error {
	var times int

	for {
		if err := e.Validator.Validate(c); err != nil {
			break
		}

		times++
	}

	if times == 0 || times > e.Times {
		return fmt.Errorf("%w: failed to validate at most %d times", ErrRepeatAtMost, e.Times)
	}

	return nil
}
