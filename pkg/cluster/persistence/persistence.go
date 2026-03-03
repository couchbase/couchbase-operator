/*
Copyright 2019-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package persistence

import (
	"context"
	goerrors "errors"
	"fmt"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// persistenceCacheTimeout caters for retries in case the platform has
	// modified the underlying secret (CAS error), the initial race between
	// the secret being created and appearing in the cache to be read.  It
	// also affects start up time when a cluster exists, but the secret
	// doesn't, so don't set this too high!
	persistenceCacheTimeout = 5 * time.Second
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

	// Upgrading is flagged when an upgrade starts and removed on termination.
	Upgrading PersistentKind = "upgrading"

	// Password is the last known good admin password.
	Password PersistentKind = "password"

	// CACertificate is the last known good CA certificate.
	CACertificate PersistentKind = "ca"

	// ClientCertificate is the last known good client certificate.
	ClientCertificate PersistentKind = "clientCertificate"

	// ClientKey is the last known good client key.
	ClientKey PersistentKind = "clientKey"

	// VolumeExpansion is flagged when an volume expansion task starts and removed on termination.
	VolumeExpansion PersistentKind = "volumeExpansion"
)

// PersistentKindXDCR is a type for XDCR persistence keys.  These are defined on
// a per-connection basis.
type PersistentKindXDCR string

const (
	// XDCRHostname records the hostname of the connection because XDCR actuall mutates
	// it and breaks the read/modify/write contract.
	XDCRHostname PersistentKindXDCR = "hostname"

	// XDCRPassword records the password so we can compare it against the secret
	// in Kubernetes.  This is not returned by a HTTP GET.
	XDCRPassword PersistentKindXDCR = "password"

	// XDCRClientKey records the client key so we can compare it against the secret
	// in Kubernetes.  This is not returned by a HTTP GET.
	XDCRClientKey PersistentKindXDCR = "clientKey"

	// XDCRClientCertificate records the client cert so we can compare it against the secret
	// in Kubernetes.  This is not returned by a HTTP GET.
	XDCRClientCertificate PersistentKindXDCR = "clientCertificate"
)

func getPersistentKindPrefixXDCR(connectionName string) string {
	return fmt.Sprintf("xdcr-connection-%s", connectionName)
}

// GetPersistentKindXDCR generates a unique key for XDCR connection parameters.
func GetPersistentKindXDCR(connectionName string, kind PersistentKindXDCR) PersistentKind {
	return PersistentKind(fmt.Sprintf("%s-%s", getPersistentKindPrefixXDCR(connectionName), string(kind)))
}

// PersistentStorage defines a very simple key value store for persisting data,
// all operations are atomic.
type PersistentStorage interface {
	// Insert a value only if it doesn't exist.
	Insert(PersistentKind, string) error
	// Upsert a key.
	Upsert(PersistentKind, string) error
	// Update a key, if it already exists.
	Update(PersistentKind, string) error
	// Delete a key, if it already exists.
	Delete(PersistentKind) error
	// DeleteXDCR deletes all keys associated with this connection.
	DeleteXDCR(string) error
	// Get a value.
	Get(PersistentKind) (string, error)
	// Clear clears persistent storage (e.g. ephemeral disaster recovery)
	Clear() error
}

// ErrKeyError is returned when a key does/doesn't exist when it should't/should.
var ErrKeyError = goerrors.New("key error")

// persistentStorageImpl provides state to the Operator, at present
// through ConfigMaps to keep configuration simple.
type persistentStorageImpl struct {
	client    *client.Client
	couchbase *couchbasev2.CouchbaseCluster
}

// upgrade spots old 2.0 and older config maps and makes them secrets.
func upgrade(client *client.Client, couchbase *couchbasev2.CouchbaseCluster) error {
	configmap, err := client.KubeClient.CoreV1().ConfigMaps(couchbase.Namespace).Get(context.Background(), couchbase.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}

		return errors.NewStackTracedError(err)
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

	if _, err = client.KubeClient.CoreV1().Secrets(couchbase.Namespace).Create(context.Background(), secret, metav1.CreateOptions{}); err != nil {
		if apierrors.IsConflict(err) {
			return fmt.Errorf("cluster persistent storage secret already exists: %w", err)
		}

		return errors.NewStackTracedError(err)
	}

	if err := client.KubeClient.CoreV1().ConfigMaps(couchbase.Namespace).Delete(context.Background(), couchbase.Name, *metav1.NewDeleteOptions(0)); err != nil {
		return errors.NewStackTracedError(err)
	}

	return nil
}

// New creates a new persistent storage object referencing a new or
// existing config map.
func New(client *client.Client, couchbase *couchbasev2.CouchbaseCluster) (PersistentStorage, error) {
	if err := upgrade(client, couchbase); err != nil {
		return nil, err
	}

	if _, ok := client.Secrets.Get(couchbase.Name); !ok {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:   couchbase.Name,
				Labels: k8sutil.LabelsForCluster(couchbase),
				OwnerReferences: []metav1.OwnerReference{
					couchbase.AsOwner(),
				},
			},
		}

		if _, err := client.KubeClient.CoreV1().Secrets(couchbase.Namespace).Create(context.Background(), secret, metav1.CreateOptions{}); err != nil {
			return nil, errors.NewStackTracedError(err)
		}
	}

	return &persistentStorageImpl{
		client:    client,
		couchbase: couchbase,
	}, nil
}

func (p *persistentStorageImpl) get() (*corev1.Secret, error) {
	s, ok := p.client.Secrets.Get(p.couchbase.Name)
	if !ok {
		return nil, fmt.Errorf("%w: persistent secret does not exist", errors.NewStackTracedError(errors.ErrResourceRequired))
	}

	secret := s.DeepCopy()

	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}

	return secret, nil
}

func (p *persistentStorageImpl) Clear() error {
	f := func(secret *corev1.Secret) error {
		secret.Data = nil

		return nil
	}

	return p.apply(f)
}

// apply applies and flushes the config map to etcd to persist changes.
func (p *persistentStorageImpl) apply(f func(*corev1.Secret) error) error {
	callback := func() error {
		secret, err := p.get()
		if err != nil {
			return err
		}

		if err := f(secret); err != nil {
			return nil
		}

		if _, err := p.client.KubeClient.CoreV1().Secrets(secret.Namespace).Update(context.Background(), secret, metav1.UpdateOptions{}); err != nil {
			return errors.NewStackTracedError(err)
		}

		return nil
	}

	if err := retryutil.RetryFor(persistenceCacheTimeout, callback); err != nil {
		return err
	}

	return nil
}

// Insert a value only if it doesn't exist.
func (p *persistentStorageImpl) Insert(kind PersistentKind, value string) error {
	f := func(secret *corev1.Secret) error {
		if _, ok := secret.Data[string(kind)]; ok {
			return fmt.Errorf("%w: key %v exists", errors.NewStackTracedError(ErrKeyError), kind)
		}

		secret.Data[string(kind)] = []byte(value)

		return nil
	}

	return p.apply(f)
}

// Upsert a key.
func (p *persistentStorageImpl) Upsert(kind PersistentKind, value string) error {
	f := func(secret *corev1.Secret) error {
		secret.Data[string(kind)] = []byte(value)

		return nil
	}

	return p.apply(f)
}

// Update a key, if it already exists.
func (p *persistentStorageImpl) Update(kind PersistentKind, value string) error {
	f := func(secret *corev1.Secret) error {
		if _, ok := secret.Data[string(kind)]; !ok {
			return fmt.Errorf("%w: key %v doesn't exist", errors.NewStackTracedError(ErrKeyError), kind)
		}

		secret.Data[string(kind)] = []byte(value)

		return nil
	}

	return p.apply(f)
}

// Delete a key if it exists.
func (p *persistentStorageImpl) Delete(kind PersistentKind) error {
	f := func(secret *corev1.Secret) error {
		if _, ok := secret.Data[string(kind)]; !ok {
			return fmt.Errorf("%w: key %v doesn't exist", errors.NewStackTracedError(ErrKeyError), kind)
		}

		delete(secret.Data, string(kind))

		return nil
	}

	return p.apply(f)
}

// DeleteXDCR deletes all keys associated with this connection.
func (p *persistentStorageImpl) DeleteXDCR(connectionName string) error {
	prefix := getPersistentKindPrefixXDCR(connectionName)

	f := func(secret *corev1.Secret) error {
		for key := range secret.Data {
			if !strings.HasPrefix(key, prefix) {
				continue
			}

			delete(secret.Data, key)
		}

		return nil
	}

	return p.apply(f)
}

// Get a value.
func (p *persistentStorageImpl) Get(kind PersistentKind) (string, error) {
	var value string

	callback := func() error {
		secret, err := p.get()
		if err != nil {
			return err
		}

		raw, ok := secret.Data[string(kind)]
		if !ok {
			return fmt.Errorf("%w: key %v doesn't exist", errors.NewStackTracedError(ErrKeyError), kind)
		}

		value = string(raw)

		return nil
	}

	if err := retryutil.RetryFor(persistenceCacheTimeout, callback); err != nil {
		return "", err
	}

	return value, nil
}
