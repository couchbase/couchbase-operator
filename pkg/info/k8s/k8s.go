/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package k8s

import (
	ctx "context"
	"fmt"
	"strings"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/info/context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/restmapper"
)

// InitContext performs Kubernetes specific initialization operations on
// a context.
func InitContext(context *context.Context) error {
	// Add our CRD to the global scheme
	if err := couchbasev2.AddToScheme(scheme.Scheme); err != nil {
		return err
	}

	if err := apiextensionsv1.AddToScheme(scheme.Scheme); err != nil {
		return err
	}

	context.KubeConfigLoader = context.Config.ConfigFlags.ToRawKubeConfigLoader()

	var err error

	if context.KubeConfig, err = context.KubeConfigLoader.ClientConfig(); err != nil {
		return err
	}

	context.KubeConfig.Wrap(client.KubeAPIWrapper)

	// Create Kubernetes clients
	if context.KubeClient, err = kubernetes.NewForConfig(context.KubeConfig); err != nil {
		return err
	}

	if context.KubeExtClient, err = clientset.NewForConfig(context.KubeConfig); err != nil {
		return err
	}

	// Create a Couchbase client
	if context.CouchbaseClient, err = versioned.NewForConfig(context.KubeConfig); err != nil {
		return err
	}

	// Create a dynamic client.
	if context.DynamicClient, err = dynamic.NewForConfig(context.KubeConfig); err != nil {
		return err
	}

	groupResources, err := restmapper.GetAPIGroupResources(context.KubeClient.Discovery())
	if err != nil {
		return err
	}

	context.RESTMapper = restmapper.NewDiscoveryRESTMapper(groupResources)

	return nil
}

// GetPod takes a resource reference and returns a pod from which we are able to collect logs,
// For collections such as deployments it simply picks one.
func GetPod(context *context.Context, kind, name string) (*corev1.Pod, error) {
	// Inspect the resource kind and perform type specific processing
	switch kind {
	case "Pod":
		return context.KubeClient.CoreV1().Pods(context.Namespace()).Get(ctx.Background(), name, metav1.GetOptions{})

	case "Deployment":
		// Grab the deployment
		deployment, err := context.KubeClient.AppsV1().Deployments(context.Namespace()).Get(ctx.Background(), name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}

		// Use the deployment's label selector as the pod selector
		selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
		if err != nil {
			return nil, err
		}

		pods, err := context.KubeClient.CoreV1().Pods(context.Namespace()).List(ctx.Background(), metav1.ListOptions{LabelSelector: selector.String()})
		if err != nil {
			return nil, err
		}

		// Select just one instance
		if len(pods.Items) == 0 {
			return nil, fmt.Errorf("%w: no pods delected for Deployment %s", errors.ErrResourceRequired, name)
		}

		return &pods.Items[0], nil
	}

	return nil, nil
}

// GetDeployments returns all Deployments in the namespace.
func GetDeployments(context *context.Context) (*appsv1.DeploymentList, error) {
	deployments, err := context.KubeClient.AppsV1().Deployments(context.Namespace()).List(ctx.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("%w: unable to poll Deployment resources", err)
	}

	return deployments, nil
}

// IsOperatorDeployment does a best effort guess of whether a deployment is that of the operator.
// By default the tooling is hardcoded to the current release's container image, but this can
// be explcitily set by the user.  If any container in a deployment's image matches, then we
// have a high probability of this being the operator.  If that doesn't work, we guess, because
// having some logs in the event a user has changed the image name and not specified it, is better
// than none.  We look for "couchbase" and "operator", which covers "couchbase/operator" (public)
// and couchbase/couchbase-operator (internal).
func IsOperatorDeployment(context *context.Context, deployment *appsv1.Deployment) bool {
	// First check, does the deployment explicitly contain the official
	// image name?
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Image == context.Config.OperatorImage {
			return true
		}
	}

	// Next, check the label on the deployment
	if deployment.Labels["app"] == "couchbase-operator" {
		return true
	}

	// Next, guess :D Chances are the name will be useful...
	if strings.Contains(deployment.Name, "couchbase") && strings.Contains(deployment.Name, "operator") && !strings.Contains(deployment.Name, "admission") {
		return true
	}

	return false
}

// IsEventCollectorDeployment does a best effor check of whether a deployment is for a EventLogger
// we mainly rely on the labels applied to the deploymet.
func IsEventCollectorDeployment(deployment *appsv1.Deployment) bool {
	// Next, check the label on the deployment
	if deployment.Labels["app"] == "event-collector" {
		return true
	}

	return false
}

// HasOperatorDeployment returns the Couchbase Operator deployment in the namespace.
func HasOperatorDeployment(context *context.Context) (bool, error) {
	deployments, err := GetDeployments(context)
	if err != nil {
		return false, err
	}

	for i := range deployments.Items {
		if IsOperatorDeployment(context, &deployments.Items[i]) {
			return true, nil
		}
	}

	return false, nil
}

// GetCouchbaseClusters returns all Couchbase Clusters in the namespace.
func GetCouchbaseClusters(context *context.Context) (*couchbasev2.CouchbaseClusterList, error) {
	clusters, err := context.CouchbaseClient.CouchbaseV2().CouchbaseClusters(context.Namespace()).List(ctx.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("%w: unable to poll CouchbaseCluster resources", err)
	}

	return clusters, nil
}
