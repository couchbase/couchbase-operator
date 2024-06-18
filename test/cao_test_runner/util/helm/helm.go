package helm

import (
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/shell"
)

const (
	NumOfArgs = 12
)

type Cmd struct {
	Command string
	Args    []string
	Flags   map[string]string
}

// ============================================================
// ================  Execute Cmd in Shell  ================
// ============================================================

func (h Cmd) ExecWithoutCapturePrint() error {
	return shell.RunWithoutCapturePrint("helm", h.ToCliArgs()...)
}

// ExecWithoutOutputCapture runs Cmd via `helm`, where Cmd is a struct holding the helm command to run (not including
// `helm` itself), the args, and any flags.
// Returns error only (no capture of results) and also logs the log output.
func (h Cmd) ExecWithoutOutputCapture() error {
	return shell.RunWithoutOutputCapture("helm", h.ToCliArgs()...)
}

// ExecWithOutputCapture runs Cmd via `helm`, where Cmd is a struct holding the helm command to run
// (not including `helm` itself), the args, and any flags.
// Returns (stdout, stderr, error) and also logs the log output.
func (h Cmd) ExecWithOutputCapture() (string, string, error) {
	return shell.RunWithOutputCapture("helm", h.ToCliArgs()...)
}

func (h Cmd) Output() (string, error) {
	return shell.Output("helm", h.ToCliArgs()...)
}

// ============================================================
// =============== Utility Functions for Cmd ==============
// ============================================================

// ToCliArgs adds all the helm flags, commands and args and returns a string slice ready to be executed in shell.
func (h Cmd) ToCliArgs() []string {
	// var args []string
	args := make([]string, 0, NumOfArgs)

	// Write out flags first because we don't know if the command args will have a -- in them or not
	// and prevent our flags from working.
	for h, v := range h.Flags {
		args = append(args, fmt.Sprintf("--%s=%s", h, v))
	}

	args = append(args, h.Command)
	args = append(args, h.Args...)

	return args
}

// WithFlag adds a flag to the helm command. Flags are appended just after "helm" like: "helm --flag command".
func (h Cmd) WithFlag(name string, value string) Cmd {
	if h.Flags == nil {
		h.Flags = make(map[string]string)
	}

	h.Flags[name] = value

	return h
}

func (h Cmd) InNamespace(namespace string) Cmd {
	return h.WithFlag("namespace", namespace)
}

func (h Cmd) InContext(ctxt string) Cmd {
	return h.WithFlag("kube-context", ctxt)
}

func (h Cmd) Set(args ...string) Cmd {
	value := strings.Join(args, " ")
	h.WithFlag("set", value)

	return h
}

func (h Cmd) Edit(args ...string) Cmd {
	value := strings.Join(args, " ")
	h.WithFlag("edit", value)

	return h
}

// =============================================
// =============== Helm Commands ===============
// =============================================

func List(args ...string) Cmd {
	return Cmd{Command: "list", Args: args}
}

func Repo(args ...string) Cmd {
	return Cmd{Command: "repo", Args: args}
}

func Pull(args ...string) Cmd {
	return Cmd{Command: "pull", Args: args}
}

func Install(args ...string) Cmd {
	return Cmd{Command: "install", Args: args}
}

func Upgrade(args ...string) Cmd {
	return Cmd{Command: "upgrade", Args: args}
}

func Uninstall(args ...string) Cmd {
	return Cmd{Command: "uninstall", Args: args}
}
