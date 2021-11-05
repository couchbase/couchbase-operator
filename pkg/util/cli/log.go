package cli

import (
	"fmt"
)

// Level is a log level. We only provide two enumerations below,
// but you can use any old positive integer valie.
type Level int

const (
	// Info is always printed out.
	Info Level = iota
	// Debug is a level higher.
	Debug
)

// Emitter is an interface that any emitter must implement.
type Emitter interface {
	// Enabled is whether the log would be printed at a specific level.
	Enabled() bool

	// Info provides Println style logging.
	Info(...interface{})

	// Infof provides Println(Sprintf) style logging.
	Infof(string, ...interface{})
}

// emmitter is a basic log emitter that echos to standard out.
type emitter bool

// Enabled is whether the log would be printed at a specific level.
func (e emitter) Enabled() bool {
	return bool(e)
}

// Info provides Println style logging.
func (e emitter) Info(args ...interface{}) {
	if e {
		fmt.Println(args...)
	}
}

// Infof provides Println(Sprintf) style logging.
func (e emitter) Infof(format string, args ...interface{}) {
	if e {
		fmt.Println(fmt.Sprintf(format, args...))
	}
}

// Logger provides an object that provide simple logging and some customization.
type Logger struct {
	Verbosity Level
}

// NewLogger returns a new logger that will print anything at Info level.
func NewLogger() *Logger {
	return &Logger{}
}

// V allows the verbosity to be set at the call site, the logger verbosity must
// be greater than or equal to the specified level to print.
func (l *Logger) V(level Level) Emitter {
	if l.Verbosity < level {
		return emitter(false)
	}

	return emitter(true)
}

// Info provides Println style logging.
func (l *Logger) Info(args ...interface{}) {
	fmt.Println(args...)
}

// Infof provides Println(Sprintf) style logging.
func (l *Logger) Infof(format string, args ...interface{}) {
	fmt.Println(fmt.Sprintf(format, args...))
}
