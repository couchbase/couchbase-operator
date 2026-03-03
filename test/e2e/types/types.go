/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

// package types are types that are not dependant on any other part of the e2e framework.
package types

import (
	"net/url"

	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	autoscalingv2 "k8s.io/client-go/kubernetes/typed/autoscaling/v2"
	autoscalingv2beta2 "k8s.io/client-go/kubernetes/typed/autoscaling/v2beta2"
	"k8s.io/client-go/rest"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/typed/apiregistration/v1"
)

// ResultType is used to encode the test case result type.
type ResultType string

const (
	// ResultTypePass means the test passed.
	ResultTypePass ResultType = "✔"

	// ResultTypeFail means the test failed.
	ResultTypeFail ResultType = "✗"

	// ResultTypeSkip means the test was skipped, most likely this is due
	// to the test being incompatible with the environment or dynamic
	// configuration parameters.
	ResultTypeSkip ResultType = "?"

	// ResultTypeErr means the test itself errored, at present this means
	// it raise a panic.
	ResultTypeErr ResultType = "!"
)

// Cluster represents a Kubernetes cluster.
type Cluster struct {
	// Static configuration.

	// KubeClient is a Kubernetes client for the cluster.
	KubeClient kubernetes.Interface
	// DynamicClient is an untyped client.
	DynamicClient dynamic.Interface
	// RESTMapper maps from an object to API paths.
	RESTMapper meta.RESTMapper
	// CRClient is a Kubernetes client for CouchbaseCluster resources.
	CRClient versioned.Interface
	// AutoscaleClient is a Kubernetes client for Autoscaling resources.
	AutoscaleClient autoscalingv2.AutoscalingV2Interface
	// AutoscaleClient is a Kubernetes client for V2Beta2 Autoscaling resources.
	V2Beta2AutoscaleClient autoscalingv2beta2.AutoscalingV2beta2Interface
	// APIRegClient is a Kubernetes client to work with APIService resources.
	APIRegClient *apiregistrationv1.ApiregistrationV1Client
	// Config is the REST configuration to use to directly access the Kubernetes API.
	Config *rest.Config
	// KubeConfPath is the path to use to get the Kubernetes client configuration.
	KubeConfPath string
	// Context is the context used in the Kubernetes config
	Context string
	// Platform is the platform we are running on.
	Platform string
	// PlatformType is the container type we are running on.
	PlatformType string
	// DynamicPlatform is whether the plaform uses cluster autoscaling.
	DynamicPlatform bool
	// IPv6 is whether or not we are using IPv6.
	IPv6 bool

	// Dynamic configuration.

	// Namespace is the namespace to use
	Namespace string
	// PullSecrets is the list of pull secrets defined in this cluster.
	PullSecrets []string
	// OperatorDeployment is a tailored deployment of the Operator for this
	// specific namespace/cluster combination.
	OperatorDeployment *appsv1.Deployment
	// DefaultSecret is the secret to use defining admin credentials.
	DefaultSecret *v1.Secret
}

// Copy performs a shallow copy of a cluster, static content only should be
// populated, the dynamic bits will get filled in during allocation.
func (c *Cluster) Copy() *Cluster {
	out := &Cluster{}

	*out = *c

	return out
}

// APIHost returns the Kubernetes endpoint.  If you are using this please reconsider why
// it's probably being done wrong.
func (c *Cluster) APIHost() string {
	return c.Config.Host
}

// APIHostname returns the hostname of the Kubernetes endpoint. If you are using this please reconsider why
// it's probably being done wrong.
func (c *Cluster) APIHostname() (string, error) {
	u, err := url.Parse(c.Config.Host)
	if err != nil {
		return "", err
	}

	return u.Hostname(), nil
}
