package util

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"path/filepath"

	"k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	CouchbaseServerContainerName = "couchbase-server"
)

// CollectInfoResult is used to communicate execution info
// back from go routines
type CollectInfoResult struct {
	// pod is the pod the result relates to
	Pod *v1.Pod
	// fileName is the absolute filename of the log file
	FileName string
	// fileNameRedacted is the absolute filename of the redacted log file
	FileNameRedacted string
	// err is the error status.  Nil on success
	Err error
}

func CollectInfo(client kubernetes.Interface, config *rest.Config, pod *v1.Pod) (result *CollectInfoResult) {
	// Place the logs in somewhere writable to any user
	baseFilename := "cbinfo-" + pod.Namespace + "-" + pod.Name + "-" + Timestamp()
	fileName := "/tmp/" + baseFilename + ".zip"
	fileNameRedacted := "/tmp/" + baseFilename + "-redacted.zip"

	result = &CollectInfoResult{
		Pod:              pod,
		FileName:         fileName,
		FileNameRedacted: fileNameRedacted,
	}

	// Generate the REST request
	req := client.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(pod.Namespace).
		Name(pod.Name).
		SubResource("exec")
	req.VersionedParams(&v1.PodExecOptions{
		Container: CouchbaseServerContainerName,
		Command: []string{
			"/opt/couchbase/bin/cbcollect_info",
			"--log-redaction-level", "partial",
			"--log-redaction-salt", Salt(),
			fileName,
		},
		Stdout: true,
	}, scheme.ParameterCodec)

	// Create an executor running over HTTP2
	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		result.Err = fmt.Errorf("log collection on %s failed: %v", pod.Name, err)
		return
	}

	// Finally run the collection command
	// Stdout appears to be required for this to work
	stdout := &bytes.Buffer{}
	if err := exec.Stream(remotecommand.StreamOptions{Stdout: stdout}); err != nil {
		result.Err = fmt.Errorf("log collection on %s failed: %v", pod.Name, err)
		return
	}

	return
}

func CopyFromPod(client kubernetes.Interface, config *rest.Config, pod *v1.Pod, paths []string) error {
	// Generate the REST request
	command := []string{"tar", "cf", "-"}
	command = append(command, paths...)
	req := client.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(pod.Namespace).
		Name(pod.Name).
		SubResource("exec")
	req.VersionedParams(&v1.PodExecOptions{
		Container: CouchbaseServerContainerName,
		Command:   command,
		Stdout:    true,
	}, scheme.ParameterCodec)

	// Create an executor running over HTTP2
	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("log collection on %s failed: %v", pod.Name, err)
	}

	// Finally run the copy command
	stdout := &bytes.Buffer{}
	if err := exec.Stream(remotecommand.StreamOptions{Stdout: stdout}); err != nil {
		return fmt.Errorf("log collection on %s failed: %v", pod.Name, err)
	}

	tarReader := tar.NewReader(stdout)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("log collection on %s failed: %v", pod.Name, err)
		}
		ioutil.WriteFile(filepath.Base(header.Name), stdout.Bytes(), 0644)
	}
	return nil
}
