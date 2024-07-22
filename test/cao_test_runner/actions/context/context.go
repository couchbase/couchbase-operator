package context

import (
	"context"
	"errors"
)

var (
	ErrNilContext = errors.New("nil context")
)

// DefaultID is returned when a KeyID can not be
// obtained from the context.
const DefaultID = "0"

// Context is a wrapper of context that it will be shared between actions.
type Context struct {
	/* ctx is either the client or server context.
	It should only be modified via copying the whole Context using WithContext.
	It is unexported to prevent people from using Context wrong
	and mutating the contexts held by callers of the same request.
	*/
	ctx context.Context
}

// NewContext is returning a task.Context passing a context.Context.
func NewContext(ctx context.Context) *Context {
	return &Context{
		ctx: ctx,
	}
}

// WithContext is creating a new task.Context and copying it to the original context.
func (c *Context) WithContext(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}

	c2 := new(Context)

	c2.ctx = ctx
	*c = *c2

	return nil
}

// Context is returning the context.Context of the Context.
func (c *Context) Context() context.Context {
	if c.ctx != nil {
		return c.ctx
	}

	return context.Background()
}

// WithID adds a value given a key. contextKey is an int, and it gets its values using go enum and iota.
func (c *Context) WithID(key contextKey, value string) {
	c.ctx = context.WithValue(c.Context(), key, value)
}

// ValueID retrieves a value from context based on a key.
func ValueID(ctx context.Context, key contextKey) string {
	id, ok := ctx.Value(key).(string)
	if !ok || id == "" {
		return DefaultID
	}

	return id
}

func ValueIDWithDefault(ctx context.Context, key contextKey, dflt string) string {
	v := ValueID(ctx, key)
	if v == DefaultID {
		return dflt
	}

	return v
}

// WithIDInterface adds an interface value given a key.
// contextKey is an int, and it gets its values using go enum and iota.
func (c *Context) WithIDInterface(key contextKey, value interface{}) {
	c.ctx = context.WithValue(c.Context(), key, value)
}

// ValueIDInterface retrieves an interface value from context based on a key.
func ValueIDInterface(ctx context.Context, key contextKey) interface{} {
	return ctx.Value(key)
}

func ValueIDInterfaceWithDefault(ctx context.Context, key contextKey, dflt string) interface{} {
	v := ValueIDInterface(ctx, key)
	if v == nil {
		return dflt
	}

	return v
}
