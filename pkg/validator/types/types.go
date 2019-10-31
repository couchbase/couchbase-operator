package types

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// KubeAbstraction contains methods and data that help facilitate
// the discovery of objects that already exist or not in K8s, so
// we can validate our cbc YAML file against secrets or storage classes
// that may or may not exist before being accepted by K8s itself.
type KubeAbstraction interface {
	// GetSecret checks whether the named secret exists in the specified namespace.
	GetSecret(string, string) (*corev1.Secret, error)
	// GetStorageClass checks whether the named stoage class exists.
	GetStorageClass(string) (*storagev1.StorageClass, error)
	// GetCouchbaseClusters returns all clusters in the specified namespace.
	GetCouchbaseClusters(string) (*couchbasev2.CouchbaseClusterList, error)
	// GetCouchbaseBuckets returns all couchbase buckets for a specified selector.
	GetCouchbaseBuckets(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseBucketList, error)
	// GetCouchbaseEphemeralBuckets returns all ephemeral buckets for a specified selector.
	GetCouchbaseEphemeralBuckets(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseEphemeralBucketList, error)
	// GetCouchbaseMemcachedBuckets returns all memcached buckets for a specified selector.
	GetCouchbaseMemcachedBuckets(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseMemcachedBucketList, error)
	// GetCouchbaseReplications returns all replications for a specified selector.
	GetCouchbaseReplications(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseReplicationList, error)
	// GetCouchbaseUsers returns all users for a specified selector
	GetCouchbaseUsers(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseUserList, error)
	// GetCouchbaseRoles returns all user roles for a specified selector
	GetCouchbaseRoles(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseRoleList, error)
	// GetCouchbaseRoleBindings returns all user role bindings for a specified selector
	GetCouchbaseRoleBindings(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseRoleBindingList, error)
}

// kubeAbstractionImpl Implements KubeAbstraction, operating on a real kubernetes cluster.
type kubeAbstractionImpl struct {
	client          kubernetes.Interface
	couchbaseClient versioned.Interface
}

// secretExists checks whether the named secret exists in the specified namespace.
func (ab *kubeAbstractionImpl) GetSecret(namespace, name string) (*corev1.Secret, error) {
	// Warning, this returns a valid pointer on error
	secret, err := ab.client.CoreV1().Secrets(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		if k8sutil.IsKubernetesResourceNotFoundError(err) {
			return nil, nil
		}
		return nil, err
	}
	return secret, nil
}

// storageClassExists checks whether the named stoage class exists.
func (ab *kubeAbstractionImpl) GetStorageClass(name string) (*storagev1.StorageClass, error) {
	// Warning, this returns a valid pointer on error
	storageClass, err := k8sutil.GetStorageClass(ab.client, name)
	if err != nil {
		if k8sutil.IsKubernetesResourceNotFoundError(err) {
			return nil, nil
		}
		return nil, err
	}
	return storageClass, nil
}

// GetCouchbaseClusters returns all clusters in the specified namespace.
func (ab *kubeAbstractionImpl) GetCouchbaseClusters(namespace string) (*couchbasev2.CouchbaseClusterList, error) {
	return ab.couchbaseClient.CouchbaseV2().CouchbaseClusters(namespace).List(metav1.ListOptions{})
}

// GetCouchbaseBuckets returns all couchbase buckets for a specified selector.
func (ab *kubeAbstractionImpl) GetCouchbaseBuckets(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseBucketList, error) {
	listOpts := metav1.ListOptions{}
	if selector != nil {
		listOpts.LabelSelector = metav1.FormatLabelSelector(selector)
	}
	return ab.couchbaseClient.CouchbaseV2().CouchbaseBuckets(namespace).List(listOpts)
}

// GetCouchbaseEphemeralBuckets returns all ephemeral buckets for a specified selector.
func (ab *kubeAbstractionImpl) GetCouchbaseEphemeralBuckets(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseEphemeralBucketList, error) {
	listOpts := metav1.ListOptions{}
	if selector != nil {
		listOpts.LabelSelector = metav1.FormatLabelSelector(selector)
	}
	return ab.couchbaseClient.CouchbaseV2().CouchbaseEphemeralBuckets(namespace).List(listOpts)
}

// GetCouchbaseMemcachedBuckets returns all memcached buckets for a specified selector.
func (ab *kubeAbstractionImpl) GetCouchbaseMemcachedBuckets(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseMemcachedBucketList, error) {
	listOpts := metav1.ListOptions{}
	if selector != nil {
		listOpts.LabelSelector = metav1.FormatLabelSelector(selector)
	}
	return ab.couchbaseClient.CouchbaseV2().CouchbaseMemcachedBuckets(namespace).List(listOpts)
}

// GetCouchbaseReplications returns all replications for a specified selector.
func (ab *kubeAbstractionImpl) GetCouchbaseReplications(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseReplicationList, error) {
	listOpts := metav1.ListOptions{}
	if selector != nil {
		listOpts.LabelSelector = metav1.FormatLabelSelector(selector)
	}
	return ab.couchbaseClient.CouchbaseV2().CouchbaseReplications(namespace).List(listOpts)
}

// GetCouchbaseUsers returns all users for a specified selector
func (ab *kubeAbstractionImpl) GetCouchbaseUsers(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseUserList, error) {
	listOpts := metav1.ListOptions{}
	if selector != nil {
		listOpts.LabelSelector = metav1.FormatLabelSelector(selector)
	}
	return ab.couchbaseClient.CouchbaseV2().CouchbaseUsers(namespace).List(listOpts)
}

// GetCouchbaseRoles returns all user roles for a specified selector
func (ab *kubeAbstractionImpl) GetCouchbaseRoles(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseRoleList, error) {
	listOpts := metav1.ListOptions{}
	if selector != nil {
		listOpts.LabelSelector = metav1.FormatLabelSelector(selector)
	}
	return ab.couchbaseClient.CouchbaseV2().CouchbaseRoles(namespace).List(listOpts)
}

// GetCouchbaseRoleBindings returns all user role bindings for a specified selector
func (ab *kubeAbstractionImpl) GetCouchbaseRoleBindings(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseRoleBindingList, error) {
	listOpts := metav1.ListOptions{}
	if selector != nil {
		listOpts.LabelSelector = metav1.FormatLabelSelector(selector)
	}
	return ab.couchbaseClient.CouchbaseV2().CouchbaseRoleBindings(namespace).List(listOpts)
}

// Validator is an abstraction layer for communicating with kubernetes
// to sanity check resources.
type Validator struct {
	Abstraction KubeAbstraction
}

// New instantiates a new Validator with kubeAbstractionImpl
func New(client kubernetes.Interface, couchbaseClient versioned.Interface) *Validator {
	abs := kubeAbstractionImpl{
		client:          client,
		couchbaseClient: couchbaseClient,
	}
	return &Validator{
		Abstraction: &abs,
	}
}
