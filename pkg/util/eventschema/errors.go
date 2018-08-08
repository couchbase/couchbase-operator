package eventschema

import (
	"fmt"
)

// OverflowError is raised when the schema expects more events than provided.
type OverflowError struct{}

// Error implements the error interface on OverflowError.
func (e OverflowError) Error() string {
	return fmt.Sprintf("schema overflowed event stream")
}

// newOverflowError creates a new overflow error.
func newOverflowError() error {
	return &OverflowError{}
}

// UnderflowError is raised when the schema matches fewer events than provided.
type UnderflowError struct{}

// Error implements the error interface on UnderflowError.
func (e UnderflowError) Error() string {
	return fmt.Sprintf("schema undeflowed event stream")
}

// newUnderflowError creates a new underflow error.
func newUnderflowError() error {
	return &UnderflowError{}
}

// ReasonMismatchError is raised when event reasons do not match.
type ReasonMismatchError struct {
	expected string
	actual   string
}

// Error implements the error interface on ReasonMismatchError.
func (e ReasonMismatchError) Error() string {
	return fmt.Sprintf("event reason mismatch, expected '%s', actual '%s'", e.expected, e.actual)
}

// newReasonMismatchError creates a new reason mismatch error.
func newReasonMismatchError(expected, actual string) error {
	return &ReasonMismatchError{expected: expected, actual: actual}
}

// MessageMismatchError is raised when event messages do not match.
type MessageMismatchError struct {
	expected string
	actual   string
}

// Error implements the error interface on MessageMismatchError.
func (e MessageMismatchError) Error() string {
	return fmt.Sprintf("event message mismatch, expected '%s', actual '%s'", e.expected, e.actual)
}

// newMessageMismatchError creates a new message mismatch error.
func newMessageMismatchError(expected, actual string) error {
	return &MessageMismatchError{expected: expected, actual: actual}
}

// FuzzyMessageMismatchError is raised when event messages do not fuzzy match.
type FuzzyMessageMismatchError struct {
	expected string
	actual   string
}

// Error implements the error interface on FuzzyMessageMismatchError.
func (e FuzzyMessageMismatchError) Error() string {
	return fmt.Sprintf("event fuzzy message mismatch, expected '%s', actual '%s'", e.expected, e.actual)
}

// newFuzzyMessageMismatchError creates a new fuzzy message mismatch error.
func newFuzzyMessageMismatchError(expected, actual string) error {
	return &FuzzyMessageMismatchError{expected: expected, actual: actual}
}

// SetMismatchError is raised when no validors match.
type SetMismatchError struct{}

// Error implements the error interface on SetMismatchError.
func (e SetMismatchError) Error() string {
	return fmt.Sprintf("no set members matched")
}

// newSetMismatchError returns a new set mismatch error.
func newSetMismatchError() error {
	return &SetMismatchError{}
}

// AnyOfError is raise when no validators match.
type AnyOfError struct{}

// Error implements the error interface on AnyOfError.
func (e AnyOfError) Error() string {
	return fmt.Sprintf("no anyof members matched")
}

// newAnyOfError return a new anyof error.
func newAnyOfError() error {
	return &AnyOfError{}
}
