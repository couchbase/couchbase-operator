package persistence

import (
	"fmt"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// PersistentKind is the sybolic name for something kept in persistent storage.
type PersistentKind string

const (
	// PodIndex the current index to allocate pod names from.
	// In theory we can start to use generated names now...
	PodIndex PersistentKind = "podIndex"

	// UUID is the UUID of the cluster under management.
	UUID PersistentKind = "uuid"

	// Version is the Couchbase version we are on or upgrading from.
	Version PersistentKind = "version"
)

// PersistentStorage defines a very simple key value store for persisting data,
// all operations are atomic.
type PersistentStorage interface {
	// Insert a value only if it doesn't exist.
	Insert(PersistentKind, string) error
	// Upsert a key.
	Upsert(PersistentKind, string) error
	// Update a key, if it already exists.
	Update(PersistentKind, string) error
	// Get a value.
	Get(PersistentKind) (string, error)
	// Clear clears persistent storage (e.g. ephemeral disaster recovery)
	Clear() error
}

// KeyError is returned when a key does/doesn't exist when it should't/should.
type KeyError struct {
	message string
}

// NewKeyError return a new error when a key does/doesn't exist when it should't/should.
func NewKeyError(message string) error {
	return &KeyError{
		message: message,
	}
}

func (e *KeyError) Error() string {
	return e.message
}

// IsKeyError indicates that a key did/didn't exist when it shouldn't/should.
func IsKeyError(err error) bool {
	_, ok := err.(*KeyError)
	return ok
}

// persistentStorageImpl provides state to the Operator, at present
// through ConfigMaps to keep configuration simple.
type persistentStorageImpl struct {
	client    kubernetes.Interface
	configMap *corev1.ConfigMap
}

// New creates a new persistent storage object referencing a new or
// existing config map.
func New(client kubernetes.Interface, couchbase *couchbasev2.CouchbaseCluster) (PersistentStorage, error) {
	configMap, err := client.CoreV1().ConfigMaps(couchbase.Namespace).Get(couchbase.Name, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, err
		}

		configMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:   couchbase.Name,
				Labels: k8sutil.LabelsForCluster(couchbase),
				OwnerReferences: []metav1.OwnerReference{
					couchbase.AsOwner(),
				},
			},
		}

		if configMap, err = client.CoreV1().ConfigMaps(couchbase.Namespace).Create(configMap); err != nil {
			return nil, err
		}
	}

	// Because it may be nil when first created.
	if configMap.Data == nil {
		configMap.Data = map[string]string{}
	}

	return &persistentStorageImpl{
		client:    client,
		configMap: configMap,
	}, nil
}

func (p *persistentStorageImpl) Clear() error {
	p.configMap.Data = map[string]string{}

	return p.flush()
}

// flush flushes the config map to etcd to persist changes.
func (p *persistentStorageImpl) flush() error {
	// Note: if there is a CAS collision then some evil 3rd party actor shouldn't have
	// messing with the map.  First up, tell them off.  Second to recover simply restart
	// the Operator.  If Kubernetes makes changes under the hood we may well need to do
	// a read modify write.
	configMap, err := p.client.CoreV1().ConfigMaps(p.configMap.Namespace).Update(p.configMap)
	if err != nil {
		return err
	}

	if configMap.Data == nil {
		configMap.Data = map[string]string{}
	}

	p.configMap = configMap

	return nil
}

// Insert a value only if it doesn't exist.
func (p *persistentStorageImpl) Insert(kind PersistentKind, value string) error {
	if _, ok := p.configMap.Data[string(kind)]; ok {
		return NewKeyError(fmt.Sprintf("insert: key %v exists", kind))
	}

	p.configMap.Data[string(kind)] = value

	return p.flush()
}

// Upsert a key.
func (p *persistentStorageImpl) Upsert(kind PersistentKind, value string) error {
	p.configMap.Data[string(kind)] = value
	return p.flush()
}

// Update a key, if it already exists.
func (p *persistentStorageImpl) Update(kind PersistentKind, value string) error {
	if _, ok := p.configMap.Data[string(kind)]; !ok {
		return NewKeyError(fmt.Sprintf("update: key %v doesn't exist", kind))
	}

	p.configMap.Data[string(kind)] = value

	return p.flush()
}

// Get a value.
func (p *persistentStorageImpl) Get(kind PersistentKind) (string, error) {
	value, ok := p.configMap.Data[string(kind)]
	if !ok {
		return "", NewKeyError(fmt.Sprintf("get: key %v doesn't exist", kind))
	}

	return value, nil
}
