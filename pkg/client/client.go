package client

import (
	"context"

	"github.com/couchbaselabs/couchbase-operator/pkg/spec"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/k8sutil"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
)

type CouchbaseClusterCR interface {
	RESTClient() *rest.RESTClient

	// Create creates an couchbase cluster CR with the desired CR
	Create(ctx context.Context, cl *spec.CouchbaseCluster) (*spec.CouchbaseCluster, error)

	// Get returns the specified couchbase cluster CR
	Get(ctx context.Context, namespace, name string) (*spec.CouchbaseCluster, error)

	// Delete deletes the specified couchbase cluster CR
	Delete(ctx context.Context, namespace, name string) error

	// Update updates the couchbase cluster CR.
	Update(ctx context.Context, couchbaseCluster *spec.CouchbaseCluster) (*spec.CouchbaseCluster, error)
}

type couchbaseClusterCR struct {
	client     *rest.RESTClient
	crScheme   *runtime.Scheme
	paramCodec runtime.ParameterCodec
}

func MustNewCRInCluster() CouchbaseClusterCR {
	cfg, err := k8sutil.InClusterConfig()
	if err != nil {
		panic(err)
	}
	cli, err := NewCRClient(cfg)
	if err != nil {
		panic(err)
	}
	return cli
}

func NewCRClient(cfg *rest.Config) (CouchbaseClusterCR, error) {
	cli, crScheme, err := New(cfg)
	if err != nil {
		return nil, err
	}
	return &couchbaseClusterCR{
		client:     cli,
		crScheme:   crScheme,
		paramCodec: runtime.NewParameterCodec(crScheme),
	}, nil
}

// TODO: make this private so that we don't expose RESTClient once operator code uses this client instead of REST calls
func New(cfg *rest.Config) (*rest.RESTClient, *runtime.Scheme, error) {
	crScheme := runtime.NewScheme()
	if err := spec.AddToScheme(crScheme); err != nil {
		return nil, nil, err
	}

	config := *cfg
	config.GroupVersion = &spec.SchemeGroupVersion
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: serializer.NewCodecFactory(crScheme)}

	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, nil, err
	}

	return client, crScheme, nil
}

func (c *couchbaseClusterCR) RESTClient() *rest.RESTClient {
	return c.client
}

func (c *couchbaseClusterCR) Create(ctx context.Context, couchbaseCluster *spec.CouchbaseCluster) (*spec.CouchbaseCluster, error) {
	result := &spec.CouchbaseCluster{}
	err := c.client.Post().Context(ctx).
		Namespace(couchbaseCluster.Namespace).
		Resource(spec.CRDResourcePlural).
		Body(couchbaseCluster).
		Do().
		Into(result)
	return result, err
}

func (c *couchbaseClusterCR) Get(ctx context.Context, namespace, name string) (*spec.CouchbaseCluster, error) {
	result := &spec.CouchbaseCluster{}
	err := c.client.Get().Context(ctx).
		Namespace(namespace).
		Resource(spec.CRDResourcePlural).
		Name(name).
		Do().
		Into(result)
	return result, err
}

func (c *couchbaseClusterCR) Delete(ctx context.Context, namespace, name string) error {
	return c.client.Delete().Context(ctx).
		Namespace(namespace).
		Resource(spec.CRDResourcePlural).
		Name(name).
		Do().
		Error()
}

func (c *couchbaseClusterCR) Update(ctx context.Context, couchbaseCluster *spec.CouchbaseCluster) (*spec.CouchbaseCluster, error) {
	result := &spec.CouchbaseCluster{}
	err := c.client.Put().Context(ctx).
		Namespace(couchbaseCluster.Namespace).
		Resource(spec.CRDResourcePlural).
		Name(couchbaseCluster.Name).
		Body(couchbaseCluster).
		Do().
		Into(result)
	return result, err
}
