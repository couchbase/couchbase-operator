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

package framework

import (
	"bytes"
	"io"
	"net/url"
	"strings"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// ExecWithOptions executes a command in the specified container,
// returning stdout, stderr and error. `options` allowed for
// additional parameters to be passed.
func (f *Framework) ExecWithOptions(kubeName string, options ExecOptions) (string, string, error) {
	const tty = false

	req := f.ClusterSpec[kubeName].KubeClient.CoreV1().RESTClient().Post().
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
		TTY:       tty,
	}, scheme.ParameterCodec)

	var stdout, stderr bytes.Buffer
	err := execute("POST", req.URL(), f.ClusterSpec[kubeName].Config, options.Stdin, &stdout, &stderr, tty)
	if err != nil {
		return "", "", err
	}

	if options.PreserveWhitespace {
		return stdout.String(), stderr.String(), err
	}
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
}

// ExecCommandInContainerWithFullOutput executes a command in the
// specified container and return stdout, stderr and error
func (f *Framework) ExecCommandInContainerWithFullOutput(kubeName string, podName, containerName string, cmd ...string) (string, string, error) {
	return f.ExecWithOptions(kubeName, ExecOptions{
		Command:       cmd,
		Namespace:     f.Namespace,
		PodName:       podName,
		ContainerName: containerName,

		Stdin:              nil,
		CaptureStdout:      true,
		CaptureStderr:      true,
		PreserveWhitespace: false,
	})
}

// ExecCommandInContainer executes a command in the specified container.
func (f *Framework) ExecCommandInContainer(kubeName string, podName, containerName string, cmd ...string) (string, error) {
	stdout, _, err := f.ExecCommandInContainerWithFullOutput(kubeName, podName, containerName, cmd...)
	return stdout, err
}

func (f *Framework) ExecShellInContainer(kubeName string, podName, containerName string, cmd string) (string, error) {
	return f.ExecCommandInContainer(kubeName, podName, containerName, "/bin/sh", "-c", cmd)
}

func (f *Framework) ExecCommandInPod(kubeName string, podName string, cmd ...string) (string, error) {
	pod, err := f.PodClient(kubeName).Get(podName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return f.ExecCommandInContainer(kubeName, podName, pod.Spec.Containers[0].Name, cmd...)
}

func (f *Framework) ExecCommandInPodWithFullOutput(kubeName string, podName string, cmd ...string) (string, string, error) {
	pod, err := f.PodClient(kubeName).Get(podName, metav1.GetOptions{})
	if err != nil {
		return "", "", err
	}
	return f.ExecCommandInContainerWithFullOutput(kubeName, podName, pod.Spec.Containers[0].Name, cmd...)
}

func (f *Framework) ExecShellInPod(kubeName string, podName string, cmd string) (string, error) {
	return f.ExecCommandInPod(kubeName, podName, "/bin/sh", "-c", cmd)
}

func (f *Framework) ExecShellInPodWithFullOutput(kubeName string, podName string, cmd string) (string, string, error) {
	return f.ExecCommandInPodWithFullOutput(kubeName, podName, "/bin/sh", "-c", cmd)
}

func execute(method string, url *url.URL, config *restclient.Config, stdin io.Reader, stdout, stderr io.Writer, tty bool) error {
	exec, err := remotecommand.NewSPDYExecutor(config, method, url)
	if err != nil {
		return err
	}
	return exec.Stream(remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Tty:    tty,
	})
}
