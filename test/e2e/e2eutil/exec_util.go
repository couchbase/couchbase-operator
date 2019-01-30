/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2eutil

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/couchbase/couchbase-operator/test/e2e/types"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

// ExecOptions passed to ExecWithOptions
type ExecOptions struct {
	// Command is the command and argurments to run.
	Command []string
	// Namespace is the namespace the pod resides in.
	Namespace string
	// PodName is the name of the pod to run the command on.
	PodName string
	// ContainerName is the name of the container within the pod to run the command on.
	ContainerName string
	// Stdin is the standard input to supply to the command.
	Stdin io.Reader
	// CaptureStdout declates whether we want to collect standard out.
	CaptureStdout bool
	// CaptureStderr declates whether we want to collect standard error.
	CaptureStderr bool
	// If false, whitespace in std{err,out} will be removed.
	PreserveWhitespace bool
}

// ExecWithOptions executes a command in the specified container,
// returning stdout, stderr and error. `options` allowed for
// additional parameters to be passed.
func ExecWithOptions(k8s *types.Cluster, options ExecOptions) (string, string, error) {
	req := k8s.KubeClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(options.PodName).
		Namespace(options.Namespace).
		SubResource("exec").
		Param("container", options.ContainerName)
	req.VersionedParams(&v1.PodExecOptions{
		Container: options.ContainerName,
		Command:   options.Command,
		Stdin:     options.Stdin != nil,
		Stdout:    options.CaptureStdout,
		Stderr:    options.CaptureStderr,
	}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(k8s.Config, "POST", req.URL())
	if err != nil {
		return "", "", err
	}

	var stdout, stderr bytes.Buffer
	if err := exec.Stream(remotecommand.StreamOptions{Stdin: options.Stdin, Stdout: &stdout, Stderr: &stderr}); err != nil {
		return "", "", err
	}

	if options.PreserveWhitespace {
		return stdout.String(), stderr.String(), err
	}

	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
}

// ExecCommandInContainerWithFullOutput executes a command in the
// specified container and return stdout, stderr and error
func ExecCommandInContainerWithFullOutput(k8s *types.Cluster, namespace, podName, containerName string, cmd ...string) (string, string, error) {
	return ExecWithOptions(k8s, ExecOptions{
		Command:       cmd,
		Namespace:     namespace,
		PodName:       podName,
		ContainerName: containerName,

		Stdin:              nil,
		CaptureStdout:      true,
		CaptureStderr:      true,
		PreserveWhitespace: false,
	})
}

// ExecCommandInContainer executes a command in the specified container.
func ExecCommandInContainer(k8s *types.Cluster, namespace, podName, containerName string, cmd ...string) (string, string, error) {
	return ExecCommandInContainerWithFullOutput(k8s, namespace, podName, containerName, cmd...)
}

// ExecCommandInPod executes a command in the specified pod, selecting the first container.
func ExecCommandInPod(k8s *types.Cluster, namespace, name string, cmd ...string) (string, string, error) {
	pod, err := k8s.KubeClient.CoreV1().Pods(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return "", "", err
	}
	return ExecCommandInContainer(k8s, namespace, name, pod.Spec.Containers[0].Name, cmd...)
}

// MustExecCommandInPod behaves like ExecCommandInPod, dying on error.
func MustExecCommandInPod(t *testing.T, k8s *types.Cluster, namespace, name string, cmd ...string) (string, string) {
	stdout, stderr, err := ExecCommandInPod(k8s, namespace, name, cmd...)
	if err != nil {
		t.Logf("Command: %s", strings.Join(cmd, ""))
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
		Die(t, err)
	}
	return stdout, stderr
}

// ExecShellInPod executes a command in the specified pod, selecting the first container.  The command is run via the default shell.
func ExecShellInPod(k8s *types.Cluster, namespace, name string, cmd string) (string, string, error) {
	return ExecCommandInPod(k8s, namespace, name, "/bin/sh", "-c", cmd)
}

// MustExecShellInPod behaves like ExecShellInPod, dying on error.
func MustExecShellInPod(t *testing.T, k8s *types.Cluster, namespace, name string, cmd string) (string, string) {
	stdout, stderr, err := ExecShellInPod(k8s, namespace, name, cmd)
	if err != nil {
		t.Logf("Command: %s", cmd)
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
		Die(t, err)
	}
	return stdout, stderr
}
