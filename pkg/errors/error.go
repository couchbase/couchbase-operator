package errors

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
)

// As of go 1.13, errors are now beginning to be standardized.
// Read https://blog.golang.org/go1.13-errors for a good introduction.
// Basic errors should be directly comparable as the errors below i.e.
// Specializations should wrap base errors (either use fmt.Errorf("%w..."))
// or implement Unwrap().
//
// Why is this cool?  Well each frame in the call stack can wrap the error
// with more and more context until the point it is reported, but it retains
// the ability to ask "were you originally due to error type X?" or "was the
// error X at any point?".

// Generic TLS related errors.
var (
	// ErrCertificateInvalid is raised when a certificate is invalid for any reason.
	ErrCertificateInvalid = errors.New("certificate error")

	// ErrPublicKeyInvalid is raised when a public key is malformed or not supported.
	ErrPublicKeyInvalid = errors.New("public key invalid")

	// ErrPrivateKeyInvalid is raised when a private key is malformed or not supported.
	ErrPrivateKeyInvalid = errors.New("private key invalid")

	// ErrPKCS12NotSupported is raised when attempting to use PKCS#12 when it's not supported by the server version.
	ErrPKCS12NotSupported = errors.New("pkcs12 not supported")

	// ErrTLSInvalid is raised when a TLS error occurs e.g. client/server certs don't
	// validate.
	ErrTLSInvalid = errors.New("TLS invalid")
)

// Generic Kubernetes errors.
var (
	// ErrResourceRequired is raised when an expected resource is not found.
	ErrResourceRequired = errors.New("required resource missing")

	// ErrResourceExists is raised when a resource exists and it shouldn't.
	ErrResourceExists = errors.New("resource unexpectedly exists")

	// ErrResourceAttributeRequired is raised when any data associated with a resource
	// is not found e.g. labels, annotations or specification attributes.
	ErrResourceAttributeRequired = errors.New("required resource attribute missing")

	// ErrKubernetesError is raised when not handled by one of the resource errors e.g.
	// some validation steps fail.
	ErrKubernetesError = errors.New("unexpected kubernetes error")

	// ErrVolumeResizeError is raised when a request to resize a Persistent Volume fails.
	ErrVolumeResizeError = errors.New("volume resize failed")

	// ErrUnknownCondition is raised when unexpectedly encountered condition with unknown status.
	ErrUnknownCondition = errors.New("encountered unknown condition")

	// ErrPodNotFound is raised when a pod is not found for a member.
	ErrPodNotFound = errors.New("pod not found for member")
)

// Couchbase server specific errors.
var (
	// ErrCouchbaseServerError is raised when couchbase doesn't behave in the way that
	// we expect, either by explicitly throwing an error or not doing something asynchronously.
	ErrCouchbaseServerError = errors.New("unexpected couchbase server error")

	// ErrInvalidVersion is raised when we detect a semantic version formatting error
	// or an attempt to use an incompatible version etc.
	ErrInvalidVersion = fmt.Errorf("version error")

	ErrClusterVersionMismatch = errors.New("cluster version mismatch")

	// ErrRebalanceIncomplete is raised when we think the rebalance failed or did not
	// even occur and thus we should avoid deleting anything yet.
	ErrRebalanceIncomplete = errors.New("unexpected rebalance error")

	// ErrNoVolumeMounts is raised when a pod has no volume mounts specified.
	ErrNoVolumeMounts = errors.New("pod has no persistent storage")

	// ErrNodeNotAdded is raised when a node hasn't been added to the cluster.
	ErrNodeNotAdded = errors.New("node not added")
)

// Logging errors

var (
	// ErrNoLoggingVolume is raised when there are no mounts defined when trying to enable logging.
	// Wouldn't be needed if the DAC was used.
	ErrNoLoggingVolume = errors.New("cannot enable logging with no logging mounts defined")
)

// Backup Errors.
var (
	// ErrBackupInvalidConfiguration is raised when fields are missing or misconfigured
	// for backup/restore.
	ErrBackupInvalidConfiguration = errors.New("invalid backup configuration")
)

// Operator specific errors.
var (
	// ErrInternalError is something that should never get raised.
	ErrInternalError = errors.New("unexpected internal error")

	// ErrConfigurationInvalid is raised when we detect that configuration constraints
	// have been violated.  This is indicative that the user has either ignored the DAC
	// or that there is some functionality missing from the DAC.
	ErrConfigurationInvalid = errors.New("user configuration error")

	// ErrDeleteDelayPreventsReconcile is an error that is surfaced when a pod is scheduled
	// for deletion but has not reached the expiration of its delay.
	ErrDeleteDelayPreventsReconcile = errors.New("pod not removed due to delete delay")

	// ErrTooManyServerImages is raised when too many couchbase server images are found
	// in a CouchbaseCluster spec.
	ErrTooManyServerImages = errors.New("expected there to be 1 or 2 server images in the cluster spec but found more")

	// ErrServerClassNotFound is raised when the operator looks for a server class
	// that doesn't exist.
	ErrServerClassNotFound       = errors.New("server class not found")
	ErrImageVersionUnretrievable = errors.New("error extracting image version")

	// ErrNoMatchingServerClass is raised when no server class is found that matches the node.
	ErrNoMatchingServerClass = errors.New("no matching server class found")
)

// CLI errors.
var (
	// ErrUserInput is when an expected input has not been provided.
	ErrUserInput = errors.New("user input error")
)

// StackTracedError allows an error to be wrapped up at the source and store the stack
// trace of where it occurred, useful for determining the context in which the error
// occurred when errors are propagated up the stack without being wrapped further.
type StackTracedError struct {
	// err is the cached base error.
	err error

	// stack is the stack trace where the error was created.
	stack string
}

func NewStackTracedError(err error) error {
	// You may end up wrapping nil accidentally, so just ignore it.
	// This is considered bad as a stack trace isn't free.
	if err == nil {
		return nil
	}

	// Collect upto 64 program counters.
	pcs := make([]uintptr, 64)

	// Get the callers that led to here, skipping this stack frame and the call's.
	runtime.Callers(2, pcs)

	// Yes, you may be thinking that runtime/debug.Stack is easier, and while it
	// is we have no control over formatting.  We could even eventually just
	// keep this as a map (JSON object) and have logify format it correctly...
	frames := runtime.CallersFrames(pcs)

	stack := []string{}

	for frame, more := frames.Next(); more; frame, more = frames.Next() {
		stack = append(stack, frame.Function)
		stack = append(stack, fmt.Sprintf("\t%s:%d", frame.File, frame.Line))
	}

	return &StackTracedError{
		err:   err,
		stack: strings.Join(stack, "\n"),
	}
}

func (e *StackTracedError) Error() string {
	return e.err.Error()
}

func (e *StackTracedError) Unwrap() error {
	return e.err
}

func (e *StackTracedError) GetStack() string {
	return e.stack
}

// joins errors together and skips anything that is nil.
// https://github.com/golang/go/blob/094c75219a160be1667c9757f363ffad8926632b/src/errors/join.go
func Join(errs ...error) error {
	n := 0

	for _, err := range errs {
		if err != nil {
			n++
		}
	}

	if n == 0 {
		return nil
	}

	e := &joinError{
		errs: make([]error, 0, n),
	}

	for _, err := range errs {
		if err != nil {
			e.errs = append(e.errs, err)
		}
	}

	return e
}

type joinError struct {
	errs []error
}

func (e *joinError) Error() string {
	var b []byte

	for i, err := range e.errs {
		if i > 0 {
			b = append(b, '\n')
		}

		b = append(b, err.Error()...)
	}

	return string(b)
}

func (e *joinError) Unwrap() []error {
	return e.errs
}
