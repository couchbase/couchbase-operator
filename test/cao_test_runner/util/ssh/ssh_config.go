package sshutil

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
)

var (
	ErrUserNotProvided     = errors.New("user not provided")
	ErrPasswordNotProvided = errors.New("password not provided")
	ErrHostnameNotProvided = errors.New("hostname not provided")
)

var (
	defaultPort    = 22 // Default SSH Port
	defaultTimeout = 30 * time.Second
)

// SSHConfig holds all the required info for the SSH connection.
type SSHConfig struct {
	User     string
	Password string
	Timeout  time.Duration
	Hostname string
	Port     int

	config *ssh.ClientConfig
	client *ssh.Client
}

func newSSHConfig(user, password, hostname string, port int, timeout time.Duration) (*SSHConfig, error) {
	if user == "" {
		return nil, fmt.Errorf("new ssh config: %w", ErrUserNotProvided)
	}

	if password == "" {
		return nil, fmt.Errorf("new ssh config: %w", ErrPasswordNotProvided)
	}

	if hostname == "" {
		return nil, fmt.Errorf("new ssh config: %w", ErrHostnameNotProvided)
	}

	if port == 0 {
		port = defaultPort
	}

	if timeout == 0 {
		timeout = defaultTimeout
	}

	return &SSHConfig{
		User:     user,
		Password: password,
		Timeout:  timeout,
		Hostname: hostname,
		Port:     port,

		config: &ssh.ClientConfig{
			User: user,
			Auth: []ssh.AuthMethod{
				ssh.Password(password),
			},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         timeout,
		},

		client: &ssh.Client{},
	}, nil
}

// connect establishes an SSH connection to the specified host and sets the SSHConfig.client.
func (sshConfig *SSHConfig) connect() error {
	var err error

	sshConfig.client, err = ssh.Dial("tcp", fmt.Sprintf("%s:%d", sshConfig.Hostname, sshConfig.Port), sshConfig.config)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	return nil
}

// ExecuteCommand runs the command on the remote host.
// Returns stdout, stderr if captureOutput=true.
// Prints stdout, stderr if printOutput=true.
func (sshConfig *SSHConfig) executeCommand(command string, captureOutput, printOutput bool) (string, string, error) {
	session, err := sshConfig.client.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("execute command `%s`: %w", command, err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer

	if captureOutput || printOutput {
		session.Stdout = &stdout
		session.Stderr = &stderr
	}

	err = session.Run(command)
	if err != nil {
		logrus.Warn(stderr)
		return "", "", fmt.Errorf("execute command `%s`: %w", command, err)
	}

	if printOutput {
		logrus.Infof("command: %s | output:\n%s", command, stdout.String())
	}

	if captureOutput {
		return stdout.String(), stderr.String(), nil
	}

	return "", "", nil
}

// close closes the SSH connection.
func (sshConfig *SSHConfig) close() error {
	return sshConfig.client.Close()
}
