package collector

import (
	"fmt"

	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/config"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/resource"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		if container.Name == util.CouchbaseServerContainerName {
			return true
		}
	}
	return false
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
			return fmt.Errorf(`pod "%s" doesn't contain a "%s" container`, pod.Name, util.CouchbaseServerContainerName)
		}
	}

	// This will take an eternity so fan out processing in parallel
	resultChan := make(chan *util.CollectInfoResult)
	for index, _ := range pods.Items {
		go func(pod *v1.Pod) {
			resultChan <- util.CollectInfo(r.context.KubeClient, r.context.KubeConfig, pod)
		}(&pods.Items[index])
	}

	// Fan in the results and output any pertinent information
	results := []*util.CollectInfoResult{}
	for i := 0; i < len(pods.Items); i++ {
		result := <-resultChan
		if result.Err != nil {
			fmt.Println(err)
			continue
		}
		results = append(results, result)
	}

	fmt.Println("Plain text server logs accessible with the following commands (use 'oc' command for OpenShift deployments):")
	for _, result := range results {
		fileSpec := result.Pod.Namespace + "/" + result.Pod.Name + ":" + result.FileName
		fmt.Println("    kubectl cp", fileSpec, ".")
	}

	fmt.Println("Redacted server logs accessible with the following commands (use 'oc' command for OpenShift deployments):")
	for _, result := range results {
		fileSpec := result.Pod.Namespace + "/" + result.Pod.Name + ":" + result.FileNameRedacted
		fmt.Println("    kubectl cp", fileSpec, ".")
	}

	return nil
}

func (r *couchbaseLogCollector) Write(b backend.Backend) error {
	return nil
}
