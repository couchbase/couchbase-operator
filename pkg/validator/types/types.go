package types

import (
	"context"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	// GetNamespace returns the requested namespace.
	GetNamespace(string) (*corev1.Namespace, error)
	// GetCouchbaseClusters returns all clusters in the specified namespace.
	GetCouchbaseClusters(string) (*couchbasev2.CouchbaseClusterList, error)
	// GetCouchbaseBuckets returns all couchbase buckets for a specified selector.
	GetCouchbaseBuckets(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseBucketList, error)
	// GetCouchbaseEphemeralBuckets returns all ephemeral buckets for a specified selector.
	GetCouchbaseEphemeralBuckets(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseEphemeralBucketList, error)
	// GetCouchbaseMemcachedBuckets returns all memcached buckets for a specified selector.
	GetCouchbaseMemcachedBuckets(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseMemcachedBucketList, error)
	// GetBuckets returns all abstract buckets for a specified selector.
	GetBuckets(string, *metav1.LabelSelector) ([]couchbasev2.AbstractBucket, error)
	// GetCouchbaseReplications returns all replications for a specified selector.
	GetCouchbaseReplications(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseReplicationList, error)
	// GetCouchbaseUsers returns all users for a specified selector
	GetCouchbaseUsers(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseUserList, error)
	// GetCouchbaseGroups returns all groups for a specified selector
	GetCouchbaseGroups(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseGroupList, error)
	// GetCouchbaseRoleBindings returns all user role bindings for a specified selector
	GetCouchbaseRoleBindings(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseRoleBindingList, error)
	// GetCouchbaseBackups returns all backups for a specified selector.
	GetCouchbaseBackups(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseBackupList, error)
	// GetCouchbaseBackups returns all backups for a specified selector.
	GetCouchbaseBackupRestores(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseBackupRestoreList, error)
}

// kubeAbstractionImpl Implements KubeAbstraction, operating on a real kubernetes cluster.
type kubeAbstractionImpl struct {
	client          kubernetes.Interface
	couchbaseClient versioned.Interface
}

// secretExists checks whether the named secret exists in the specified namespace.
func (ab *kubeAbstractionImpl) GetSecret(namespace, name string) (*corev1.Secret, error) {
	// Warning, this returns a valid pointer on error
	secret, err := ab.client.CoreV1().Secrets(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}

		return nil, err
	}

	return secret, nil
}

// storageClassExists checks whether the named stoage class exists.
func (ab *kubeAbstractionImpl) GetStorageClass(name string) (*storagev1.StorageClass, error) {
	// Warning, this returns a valid pointer on error
	storageClass, err := ab.client.StorageV1().StorageClasses().Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}

		return nil, err
	}

	return storageClass, nil
}

// GetNamespace returns the requested namespace.
func (ab *kubeAbstractionImpl) GetNamespace(name string) (*corev1.Namespace, error) {
	return ab.client.CoreV1().Namespaces().Get(context.Background(), name, metav1.GetOptions{})
}

// GetCouchbaseClusters returns all clusters in the specified namespace.
func (ab *kubeAbstractionImpl) GetCouchbaseClusters(namespace string) (*couchbasev2.CouchbaseClusterList, error) {
	return ab.couchbaseClient.CouchbaseV2().CouchbaseClusters(namespace).List(context.Background(), metav1.ListOptions{})
}

// GetCouchbaseBuckets returns all couchbase buckets for a specified selector.
func (ab *kubeAbstractionImpl) GetCouchbaseBuckets(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseBucketList, error) {
	listOpts := metav1.ListOptions{}

	if selector != nil {
		listOpts.LabelSelector = metav1.FormatLabelSelector(selector)
	}

	return ab.couchbaseClient.CouchbaseV2().CouchbaseBuckets(namespace).List(context.Background(), listOpts)
}

// GetCouchbaseEphemeralBuckets returns all ephemeral buckets for a specified selector.
func (ab *kubeAbstractionImpl) GetCouchbaseEphemeralBuckets(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseEphemeralBucketList, error) {
	listOpts := metav1.ListOptions{}

	if selector != nil {
		listOpts.LabelSelector = metav1.FormatLabelSelector(selector)
	}

	return ab.couchbaseClient.CouchbaseV2().CouchbaseEphemeralBuckets(namespace).List(context.Background(), listOpts)
}

// GetCouchbaseMemcachedBuckets returns all memcached buckets for a specified selector.
func (ab *kubeAbstractionImpl) GetCouchbaseMemcachedBuckets(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseMemcachedBucketList, error) {
	listOpts := metav1.ListOptions{}

	if selector != nil {
		listOpts.LabelSelector = metav1.FormatLabelSelector(selector)
	}

	return ab.couchbaseClient.CouchbaseV2().CouchbaseMemcachedBuckets(namespace).List(context.Background(), listOpts)
}

func (ab *kubeAbstractionImpl) GetBuckets(namespace string, selector *metav1.LabelSelector) ([]couchbasev2.AbstractBucket, error) {
	buckets, err := ab.GetCouchbaseBuckets(namespace, selector)
	if err != nil {
		return nil, err
	}

	ephemeral, err := ab.GetCouchbaseEphemeralBuckets(namespace, selector)
	if err != nil {
		return nil, err
	}

	memcached, err := ab.GetCouchbaseMemcachedBuckets(namespace, selector)
	if err != nil {
		return nil, err
	}

	abstract := []couchbasev2.AbstractBucket{}

	for i := range buckets.Items {
		abstract = append(abstract, &buckets.Items[i])
	}

	for i := range ephemeral.Items {
		abstract = append(abstract, &ephemeral.Items[i])
	}

	for i := range memcached.Items {
		abstract = append(abstract, &memcached.Items[i])
	}

	return abstract, nil
}

// GetCouchbaseReplications returns all replications for a specified selector.
func (ab *kubeAbstractionImpl) GetCouchbaseReplications(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseReplicationList, error) {
	listOpts := metav1.ListOptions{}

	if selector != nil {
		listOpts.LabelSelector = metav1.FormatLabelSelector(selector)
	}

	return ab.couchbaseClient.CouchbaseV2().CouchbaseReplications(namespace).List(context.Background(), listOpts)
}

// GetCouchbaseUsers returns all users for a specified selector.
func (ab *kubeAbstractionImpl) GetCouchbaseUsers(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseUserList, error) {
	listOpts := metav1.ListOptions{}

	if selector != nil {
		listOpts.LabelSelector = metav1.FormatLabelSelector(selector)
	}

	return ab.couchbaseClient.CouchbaseV2().CouchbaseUsers(namespace).List(context.Background(), listOpts)
}

// GetCouchbaseGroups returns all users for a specified selector.
func (ab *kubeAbstractionImpl) GetCouchbaseGroups(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseGroupList, error) {
	listOpts := metav1.ListOptions{}

	if selector != nil {
		listOpts.LabelSelector = metav1.FormatLabelSelector(selector)
	}

	return ab.couchbaseClient.CouchbaseV2().CouchbaseGroups(namespace).List(context.Background(), listOpts)
}

// GetCouchbaseRoleBindings returns all user role bindings for a specified selector.
func (ab *kubeAbstractionImpl) GetCouchbaseRoleBindings(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseRoleBindingList, error) {
	listOpts := metav1.ListOptions{}

	if selector != nil {
		listOpts.LabelSelector = metav1.FormatLabelSelector(selector)
	}

	return ab.couchbaseClient.CouchbaseV2().CouchbaseRoleBindings(namespace).List(context.Background(), listOpts)
}

func (ab *kubeAbstractionImpl) GetCouchbaseBackups(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseBackupList, error) {
	listOpts := metav1.ListOptions{}

	if selector != nil {
		listOpts.LabelSelector = metav1.FormatLabelSelector(selector)
	}

	return ab.couchbaseClient.CouchbaseV2().CouchbaseBackups(namespace).List(context.Background(), listOpts)
}

func (ab *kubeAbstractionImpl) GetCouchbaseBackupRestores(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseBackupRestoreList, error) {
	listOpts := metav1.ListOptions{}

	if selector != nil {
		listOpts.LabelSelector = metav1.FormatLabelSelector(selector)
	}

	return ab.couchbaseClient.CouchbaseV2().CouchbaseBackupRestores(namespace).List(context.Background(), listOpts)
}

// ValidatorOptions are configurable, as opposed to required, bits of the
// validator.
type ValidatorOptions struct {
	// Check whether referenced secrets exist, potentially whether the keys
	// are defined correctly, and certs/keys are valid in the case of TLS.
	ValidateSecrets bool

	// Check whether referenced storage classes exist.
	ValidateStorageClasses bool

	// Fill in the default fs group for a pod in order to write persistent
	// volumes.
	DefaultFileSystemGroup bool
}

// Validator is an abstraction layer for communicating with kubernetes
// to sanity check resources.
type Validator struct {
	Abstraction KubeAbstraction

	Options *ValidatorOptions
}

// New instantiates a new Validator with kubeAbstractionImpl.
func New(client kubernetes.Interface, couchbaseClient versioned.Interface, options *ValidatorOptions) *Validator {
	abs := kubeAbstractionImpl{
		client:          client,
		couchbaseClient: couchbaseClient,
	}

	// The main validator code expects this to be populated in order to
	// avoid mad levels of control flow.
	if options == nil {
		options = &ValidatorOptions{}
	}

	return &Validator{
		Abstraction: &abs,
		Options:     options,
	}
}
