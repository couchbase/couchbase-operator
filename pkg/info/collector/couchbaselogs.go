package collector

import (
	"bytes"
	"fmt"

	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/config"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/resource"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/google/uuid"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	couchbaseServerContainerName = "couchbase-server"
)

// couchbaseLogCollector represents a collection of couchbaseLogs
type couchbaseLogCollector struct {
	context *context.Context
}

// NewCouchbaseLogCollector initializes a new couchbaseLog resource
func NewCouchbaseLogCollector(context *context.Context) Collector {
	return &couchbaseLogCollector{
		context: context,
	}
}

func (r *couchbaseLogCollector) Kind() string {
	return "CouchbaseLog"
}

// hasCouchbaseServerContainer checks whether a pod contains a named couchbase
// server container
func hasCouchbaseServerContainer(pod *v1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		if container.Name == couchbaseServerContainerName {
			return true
		}
	}
	return false
}

// collectInfoResult is used to communicate execution info
// back from go routines
type collectInfoResult struct {
	// pod is the pod the result relates to
	pod *v1.Pod
	// fileName is the absolute filename of the log file
	fileName string
	// fileNameRedacted is the absolute filename of the redacted log file
	fileNameRedacted string
	// err is the error status.  Nil on success
	err error
}

// Fetch collects all couchbaseLogs as defined for the resource
func (r *couchbaseLogCollector) Fetch(res resource.ResourceReference) error {
	// Only collect logs if the type is a cluster and we have enabled collection
	if res.Kind() != "CouchbaseCluster" || !r.context.Config.CollectInfo {
		return nil
	}

	// Gather a list of pods
	selector, err := resource.GetResourceSelector(&config.Configuration{Clusters: []string{res.Name()}})
	if err != nil {
		return err
	}
	pods, err := r.context.KubeClient.CoreV1().Pods(r.context.Config.Namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return err
	}

	// Before we begin ensure each pod has a "couchbase-server" container
	for _, pod := range pods.Items {
		if !hasCouchbaseServerContainer(&pod) {
			return fmt.Errorf(`pod "%s" doesn't contain a "%s" container`, pod.Name, couchbaseServerContainerName)
		}
	}

	// Generate a salt for redaction
	salt := uuid.New().String()

	// This will take an eternity so fan out processing in parallel
	resultChan := make(chan collectInfoResult)
	for index, _ := range pods.Items {
		go func(pod *v1.Pod) {
			// Place the logs in somewhere writable to any user
			baseFilename := "cbinfo-" + pod.Namespace + "-" + pod.Name + "-" + util.Timestamp()
			fileName := "/tmp/" + baseFilename + ".zip"
			fileNameRedacted := "/tmp/" + baseFilename + "-redacted.zip"

			result := collectInfoResult{
				pod:              pod,
				fileName:         fileName,
				fileNameRedacted: fileNameRedacted,
			}
			defer func() { resultChan <- result }()

			// Generate the REST request
			req := r.context.KubeClient.CoreV1().RESTClient().Post().
				Resource("pods").
				Namespace(pod.Namespace).
				Name(pod.Name).
				SubResource("exec")
			req.VersionedParams(&v1.PodExecOptions{
				Container: couchbaseServerContainerName,
				Command: []string{
					"/opt/couchbase/bin/cbcollect_info",
					"--log-redaction-level", "partial",
					"--log-redaction-salt", salt,
					fileName,
				},
				Stdout: true,
			}, scheme.ParameterCodec)

			// Create an executor running over HTTP2
			exec, err := remotecommand.NewSPDYExecutor(r.context.KubeConfig, "POST", req.URL())
			if err != nil {
				result.err = fmt.Errorf("log collection on %s failed: %v", pod.Name, err)
				return
			}

			// Finally run the collection command
			// Stdout appears to be required for this to work
			stdout := &bytes.Buffer{}
			if err := exec.Stream(remotecommand.StreamOptions{Stdout: stdout}); err != nil {
				result.err = fmt.Errorf("log collection on %s failed: %v", pod.Name, err)
				return
			}
		}(&pods.Items[index])
	}

	// Fan in the results and output any pertinent information
	results := []collectInfoResult{}
	for i := 0; i < len(pods.Items); i++ {
		result := <-resultChan
		if result.err != nil {
			fmt.Println(err)
			continue
		}
		results = append(results, result)
	}

	fmt.Println("Plain text server logs accessible with the following commands (use 'oc' command for OpenShift deployments):")
	for _, result := range results {
		fileSpec := result.pod.Namespace + "/" + result.pod.Name + ":" + result.fileName
		fmt.Println("    kubectl cp", fileSpec, ".")
	}

	fmt.Println("Redacted server logs accessible with the following commands (use 'oc' command for OpenShift deployments):")
	for _, result := range results {
		fileSpec := result.pod.Namespace + "/" + result.pod.Name + ":" + result.fileNameRedacted
		fmt.Println("    kubectl cp", fileSpec, ".")
	}

	return nil
}

func (r *couchbaseLogCollector) Write(b backend.Backend) error {
	return nil
}
