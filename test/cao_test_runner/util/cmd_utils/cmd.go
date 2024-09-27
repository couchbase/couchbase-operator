package cmdutils

import (
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/shell"
)

type CmdUtils interface {
	ExecWithoutCapturePrint() error
	ExecWithoutOutputCapture() error
	ExecWithOutputCapture() (string, string, error)
	Output() (string, error)

	Exec(captureOutput, printOutput bool) (string, string, error)
}

type Cmd struct {
	RootCommand string
	Command     string
	Args        []string
	Flags       map[string]string
}

// ============================================================
// =================== Execute Cmd in Shell ===================
// ============================================================

func (c *Cmd) ExecWithoutCapturePrint() error {
	return shell.RunWithoutCapturePrint(c.RootCommand, c.ToCliArgs()...)
}

// ExecWithoutOutputCapture runs Cmd via shell, where Cmd is a struct holding the root command, shell command to run, the args, and any flags.
// Returns error only (no capture of results) and also logs the log output.
func (c *Cmd) ExecWithoutOutputCapture() error {
	return shell.RunWithoutOutputCapture(c.RootCommand, c.ToCliArgs()...)
}

// ExecWithOutputCapture runs Cmd via shell, where Cmd is a struct holding the root command, shell command to run, the args, and any flags.
// Returns (stdout, stderr, error) and also logs the log output.
func (c *Cmd) ExecWithOutputCapture() (string, string, error) {
	return shell.RunWithOutputCapture(c.RootCommand, c.ToCliArgs()...)
}

// func (c *Cmd) ExecVPanic() {
// 	shutil.RunVPanic(c.CoreCommand, c.ToCliArgs()...)
// }

func (c *Cmd) Output() (string, error) {
	return shell.Output(c.RootCommand, c.ToCliArgs()...)
}

// func (c *Cmd) OutputPanic() string {
//	return shutil.OutputPanic(c.CoreCommand, c.ToCliArgs()...)
// }

// Exec runs Cmd via shell, where Cmd is a struct holding the root command, shell command to run, the args, and any flags.
// If captureOutput = true,
// Returns (stdout, stderr, error) and does not print the stdout and stderr.
func (c *Cmd) Exec(captureOutput, printOutput bool) (string, string, error) {
	return shell.RunWithOptions(captureOutput, printOutput, c.RootCommand, c.ToCliArgs()...)
}

// ============================================================
// ================ Utility Functions for Cmd =================
// ============================================================

// ToCliArgs adds all the cmd flags, commands and args and returns a slice ready to be executed in shell along with root command.
func (c *Cmd) ToCliArgs() []string {
	var args []string
	// Write out flags first because we don't know if the command args will have a -- in them or not
	// and prevent our flags from working.
	for k, v := range c.Flags {
		args = append(args, fmt.Sprintf("--%s=%s", k, v))
	}

	args = append(args, c.Command)
	args = append(args, c.Args...)

	return args
}
