package k8s

import (
	"fmt"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/resource"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
)

// InitContext performs Kubernetes specific initialization operations on
// a context
func InitContext(context *context.Context) error {
	// Add our CRD to the global scheme
	if err := couchbasev2.AddToScheme(scheme.Scheme); err != nil {
		return err
	}

	context.KubeConfigLoader = context.Config.ConfigFlags.ToRawKubeConfigLoader()

	var err error
	context.KubeConfig, err = context.KubeConfigLoader.ClientConfig()
	if err != nil {
		return err
	}

	// Create Kubernetes clients
	context.KubeClient, err = kubernetes.NewForConfig(context.KubeConfig)
	if err != nil {
		return err
	}
	context.KubeExtClient, err = clientset.NewForConfig(context.KubeConfig)
	if err != nil {
		return err
	}

	// Create a Couchbase client
	context.CouchbaseClient, err = versioned.NewForConfig(context.KubeConfig)
	if err != nil {
		return err
	}

	return nil
}

// GetPod takes a resource reference and returns a pod from which we are able to collect logs,
// For collections such as deployments it simply picks one.
func GetPod(context *context.Context, resource resource.Reference) (*corev1.Pod, error) {
	// Inspect the resource kind and perform type specific processing
	switch resource.Kind() {
	case "Pod":
		return context.KubeClient.CoreV1().Pods(context.Namespace()).Get(resource.Name(), metav1.GetOptions{})

	case "Deployment":
		// Grab the deployment
		deployment, err := context.KubeClient.AppsV1().Deployments(context.Namespace()).Get(resource.Name(), metav1.GetOptions{})
		if err != nil {
			return nil, err
		}

		// Use the deployment's label selector as the pod selector
		selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
		if err != nil {
			return nil, err
		}

		pods, err := context.KubeClient.CoreV1().Pods(context.Namespace()).List(metav1.ListOptions{LabelSelector: selector.String()})
		if err != nil {
			return nil, err
		}

		// Select just one instance
		if len(pods.Items) == 0 {
			return nil, fmt.Errorf("no pods delected for Deployment %s", resource.Name())
		}
		return &pods.Items[0], nil
	}

	return nil, nil
}

// GetDeployments returns all Deployments in the namespace.
func GetDeployments(context *context.Context) (*appsv1.DeploymentList, error) {
	deployments, err := context.KubeClient.AppsV1().Deployments(context.Namespace()).List(metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("unable to poll Deployment resources: %v", err)
	}
	return deployments, nil
}

// GetOperatorDeployment returns the Couchbase Operator deployment in the namespace.
func GetOperatorDeployment(context *context.Context) (*appsv1.Deployment, error) {
	deployments, err := GetDeployments(context)
	if err != nil {
		return nil, err
	}

	for _, deployment := range deployments.Items {
		for _, container := range deployment.Spec.Template.Spec.Containers {
			if container.Image == context.Config.OperatorImage {
				return &deployment, nil
			}
		}
	}

	return nil, nil
}

// GetCouchbaseClusters returns all Couchbase Clusters in the namespace.
func GetCouchbaseClusters(context *context.Context) (*couchbasev2.CouchbaseClusterList, error) {
	clusters, err := context.CouchbaseClient.CouchbaseV2().CouchbaseClusters(context.Namespace()).List(metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("unable to poll CouchbaseCluster resources: %v", err)
	}
	return clusters, nil
}
