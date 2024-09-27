package shell

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

func RunWithoutCapturePrint(cmd string, args ...string) error {
	var output io.Writer
	_, err := Exec(output, os.Stderr, cmd, args...)

	return err
}

// RunWithoutOutputCapture command and print any output to stdout/stderr.
func RunWithoutOutputCapture(cmd string, args ...string) error {
	_, err := Exec(os.Stdout, os.Stderr, cmd, args...)

	return err
}

// RunWithOutputCapture runs command and print any output to stdout/stderr.
// Also return stdout/stderr as strings.
func RunWithOutputCapture(cmd string, args ...string) (string, string, error) {
	captureOut := new(bytes.Buffer)
	captureErr := new(bytes.Buffer)

	// Duplicate the output/error to our buffer and the test stdout/stderr
	multiOut := io.MultiWriter(captureOut, os.Stdout)
	multiErr := io.MultiWriter(captureErr, os.Stderr)

	_, err := Exec(multiOut, multiErr, cmd, args...)

	return captureOut.String(), captureErr.String(), err
}

// Output returns output from stdout.
// stderr gets used as normal here.
func Output(cmd string, args ...string) (string, error) {
	buf := &bytes.Buffer{}
	_, err := Exec(buf, os.Stderr, cmd, args...)

	return strings.TrimSuffix(buf.String(), "\n"), err
}

// RunWithOptions executes the command and provides options to capture or print the outputs.
// If captureOutput = true then it captures and returns the output as (stdout, stderr).
// If printOutput = true then it prints the output.
func RunWithOptions(captureOutput, printOutput bool, cmd string, args ...string) (string, string, error) {
	switch {
	case captureOutput == true && printOutput == true:
		{
			captureOut := new(bytes.Buffer)
			captureErr := new(bytes.Buffer)

			multiOut := io.MultiWriter(captureOut, os.Stdout)
			multiErr := io.MultiWriter(captureErr, os.Stderr)

			_, err := Exec(multiOut, multiErr, cmd, args...)

			return captureOut.String(), captureErr.String(), err
		}
	case captureOutput == true && printOutput == false:
		{
			captureOut := new(bytes.Buffer)
			captureErr := new(bytes.Buffer)

			_, err := Exec(captureOut, captureErr, cmd, args...)

			return captureOut.String(), captureErr.String(), err
		}
	case captureOutput == false && printOutput == true:
		{
			_, err := Exec(os.Stdout, os.Stderr, cmd, args...)

			return "", "", err
		}
	default:
		{
			_, err := Exec(new(bytes.Buffer), new(bytes.Buffer), cmd, args...)

			return "", "", err
		}
	}
}

func Exec(stdout, stderr io.Writer, cmd string, args ...string) (bool, error) {
	ran, code, err := run(stdout, stderr, cmd, args...)
	if err == nil {
		return true, nil
	}

	if ran {
		return ran, fmt.Errorf(`running "%s %s" failed with exit code %d: %w`, cmd, strings.Join(args, " "), code, err)
	}

	return ran, fmt.Errorf(`failed to run "%s %s: %w"`, cmd, strings.Join(args, " "), err)
}

func run(stdout, stderr io.Writer, cmd string, args ...string) (bool, int, error) {
	c := exec.Command(cmd, args...)
	c.Stderr = stderr
	c.Stdout = stdout
	c.Stdin = os.Stdin
	err := c.Run()

	return CmdRan(err), ExitStatus(err), err
}

func CmdRan(err error) bool {
	if err == nil {
		return true
	}

	var errTarget *exec.ExitError

	errors.As(err, &errTarget)

	return errTarget.Exited()
}

type exitStatus interface {
	ExitStatus() int
}

// ExitStatus returns the exit status of the error if it is an exec.ExitError
// or if it implements ExitStatus() int.
// 0 if it is nil or 1 if it is a different error.
func ExitStatus(err error) int {
	if err == nil {
		return 0
	}

	var errStatus exitStatus
	if errors.As(err, &errStatus) {
		if errStatus.ExitStatus() != 0 {
			return errStatus.ExitStatus()
		}
	}

	var errTarget *exec.ExitError
	if errors.As(err, &errTarget) {
		if ex, ok := errTarget.Sys().(exitStatus); ok {
			return ex.ExitStatus()
		}
	}

	return 1
}
