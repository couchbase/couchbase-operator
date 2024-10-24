package sshutil

import (
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"
)

type SSHUtil interface {
	// Execute runs the command on remote machine.
	// Returns the stdout and stderr if captureOutput=true.
	// Prints the output to console if printOutput=true.
	Execute(cmd string, captureOutput, printOutput bool) (string, string, error)

	// ExecuteConcurrently runs multiple commands on remote machine concurrently.
	// Returns the stdout of all commands if captureOutput=true.
	// Prints the output(s) to console if printOutput=true.
	ExecuteConcurrently(commands []string, captureOutput, printOutput bool) ([]string, error)
}

func NewSSHUtil(user, password, hostname string, port int, timeout time.Duration) (SSHUtil, error) {
	sshConfig, err := newSSHConfig(user, password, hostname, port, timeout)
	if err != nil {
		return nil, fmt.Errorf("new ssh util: %w", err)
	}

	return sshConfig, nil
}

func (sshConfig *SSHConfig) Execute(cmd string, captureOutput, printOutput bool) (string, string, error) {
	err := sshConfig.connect()
	if err != nil {
		return "", "", fmt.Errorf("ssh util execute: %w", err)
	}

	stdout, stderr, err := sshConfig.executeCommand(cmd, captureOutput, printOutput)
	if err != nil {
		return "", stderr, fmt.Errorf("ssh util execute: %w", err)
	}

	err = sshConfig.close()
	if err != nil {
		return "", "", fmt.Errorf("ssh util execute: %w", err)
	}

	return stdout, stderr, nil
}

func (sshConfig *SSHConfig) ExecuteConcurrently(commands []string, captureOutput, printOutput bool) ([]string, error) {
	eg := &errgroup.Group{}

	stdoutList := make([]string, len(commands))

	for i, cmd := range commands {
		eg.Go(func() error {
			tempSSHConfig, err := newSSHConfig(sshConfig.User, sshConfig.Password, sshConfig.Hostname, sshConfig.Port, sshConfig.Timeout)
			if err != nil {
				return fmt.Errorf("execute concurrently: %w", err)
			}

			err = tempSSHConfig.connect()
			if err != nil {
				return fmt.Errorf("execute concurrently: %w", err)
			}

			stdout, _, err := tempSSHConfig.executeCommand(cmd, captureOutput, printOutput)
			if err != nil {
				return fmt.Errorf("execute concurrently: %w", err)
			}

			err = tempSSHConfig.close()
			if err != nil {
				return fmt.Errorf("execute concurrently: %w", err)
			}

			stdoutList[i] = stdout

			return nil
		})
	}

	err := eg.Wait()
	if err != nil {
		return nil, err
	}

	return stdoutList, nil
}
