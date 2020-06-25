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

	// Password is the last known good admin password.
	Password PersistentKind = "password"
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
	client kubernetes.Interface
	secret *corev1.Secret
}

// upgrade spots old 2.0 and older config maps and makes them secrets.
func upgrade(client kubernetes.Interface, couchbase *couchbasev2.CouchbaseCluster) error {
	configmap, err := client.CoreV1().ConfigMaps(couchbase.Namespace).Get(couchbase.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}

		return err
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   couchbase.Name,
			Labels: k8sutil.LabelsForCluster(couchbase),
			OwnerReferences: []metav1.OwnerReference{
				couchbase.AsOwner(),
			},
		},
		StringData: configmap.Data,
	}

	if _, err = client.CoreV1().Secrets(couchbase.Namespace).Create(secret); err != nil {
		if errors.IsConflict(err) {
			return fmt.Errorf("cluster persistent storage secret already exists: %w", err)
		}

		return err
	}

	if err := client.CoreV1().ConfigMaps(couchbase.Namespace).Delete(couchbase.Name, metav1.NewDeleteOptions(0)); err != nil {
		return err
	}

	return nil
}

// New creates a new persistent storage object referencing a new or
// existing config map.
func New(client kubernetes.Interface, couchbase *couchbasev2.CouchbaseCluster) (PersistentStorage, error) {
	if err := upgrade(client, couchbase); err != nil {
		return nil, err
	}

	secret, err := client.CoreV1().Secrets(couchbase.Namespace).Get(couchbase.Name, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, err
		}

		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:   couchbase.Name,
				Labels: k8sutil.LabelsForCluster(couchbase),
				OwnerReferences: []metav1.OwnerReference{
					couchbase.AsOwner(),
				},
			},
		}

		if secret, err = client.CoreV1().Secrets(couchbase.Namespace).Create(secret); err != nil {
			return nil, err
		}
	}

	// Because it may be nil when first created.
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}

	return &persistentStorageImpl{
		client: client,
		secret: secret,
	}, nil
}

func (p *persistentStorageImpl) Clear() error {
	p.secret.Data = map[string][]byte{}

	return p.flush()
}

// flush flushes the config map to etcd to persist changes.
func (p *persistentStorageImpl) flush() error {
	// Note: if there is a CAS collision then some evil 3rd party actor shouldn't have
	// messing with the map.  First up, tell them off.  Second to recover simply restart
	// the Operator.  If Kubernetes makes changes under the hood we may well need to do
	// a read modify write.
	secret, err := p.client.CoreV1().Secrets(p.secret.Namespace).Update(p.secret)
	if err != nil {
		return err
	}

	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}

	p.secret = secret

	return nil
}

// Insert a value only if it doesn't exist.
func (p *persistentStorageImpl) Insert(kind PersistentKind, value string) error {
	if _, ok := p.secret.Data[string(kind)]; ok {
		return NewKeyError(fmt.Sprintf("insert: key %v exists", kind))
	}

	p.secret.Data[string(kind)] = []byte(value)

	return p.flush()
}

// Upsert a key.
func (p *persistentStorageImpl) Upsert(kind PersistentKind, value string) error {
	p.secret.Data[string(kind)] = []byte(value)
	return p.flush()
}

// Update a key, if it already exists.
func (p *persistentStorageImpl) Update(kind PersistentKind, value string) error {
	if _, ok := p.secret.Data[string(kind)]; !ok {
		return NewKeyError(fmt.Sprintf("update: key %v doesn't exist", kind))
	}

	p.secret.Data[string(kind)] = []byte(value)

	return p.flush()
}

// Get a value.
func (p *persistentStorageImpl) Get(kind PersistentKind) (string, error) {
	value, ok := p.secret.Data[string(kind)]
	if !ok {
		return "", NewKeyError(fmt.Sprintf("get: key %v doesn't exist", kind))
	}

	return string(value), nil
}
