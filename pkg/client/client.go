/*
Copyright 2017-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package client

import (
	"context"
	"fmt"

	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func MustNew(cfg *rest.Config) versioned.Interface {
	cli, err := versioned.NewForConfig(cfg)
	if err != nil {
		panic(err)
	}

	return cli
}

// Client gives full access to the Kubernetes APIs and also caching
// for commonly polled resources.
type Client struct {
	// KubeConfig is the Kubernetes config file
	KubeConfig *rest.Config

	// KubeClient is a client for accessing Kubernetes native resources
	KubeClient kubernetes.Interface

	// Couchbase is a client for accessing Couchbase custom resources
	CouchbaseClient versioned.Interface

	// Pods is a read only cache of pods (Couchbase cluster scoped)
	Pods *PodCache

	// PersistentVolumeClaims is a read only cache of persistent volume claims (Couchbase cluster scoped)
	PersistentVolumeClaims *PersistentVolumeClaimCache

	// Services is a read only cache of services (Couchbase cluster scoped)
	Services *ServiceCache

	// Secrets is a read only cache of secrets (namespace scoped)
	Secrets *SecretCache

	// PodDisruptionBudgets is a read only cache of pod disruption budgets (Couchbase cluster scoped)
	PodDisruptionBudgets *PodDisruptionBudgetCache

	// CouchbaseBuckets is a read only cache of couchbase buckets (namespace scoped)
	CouchbaseBuckets *CouchbaseBucketCache

	// CouchbaseEphemeralBuckets is a read only cache of ephemeral buckets (namespace scoped)
	CouchbaseEphemeralBuckets *CouchbaseEphemeralBucketCache

	// CouchbaseMemcachedBuckets is a read only cache of memcached buckets (namespace scoped)
	CouchbaseMemcachedBuckets *CouchbaseMemcachedBucketCache

	// CouchbaseReplications is a read only cache of couchbase replications (namespace scoped)
	CouchbaseReplications *CouchbaseReplicationCache

	// CouchbaseUsers is a read only cache of couchbase users (namespace scoped)
	CouchbaseUsers *CouchbaseUserCache

	// CouchbaseGroups is a read only cache of couchbase groups (namespace scoped)
	CouchbaseGroups *CouchbaseGroupCache

	// CouchbaseRoleBindings is a read only cache of couchbase rolebindings (namespace scoped)
	CouchbaseRoleBindings *CouchbaseRoleBindingCache

	// CouchbaseBackups is a read only cache of couchbase backups (namespace scoped)
	CouchbaseBackups *CouchbaseBackupCache

	// CouchbaseBackupRestores is a read only cache of couchbase restores (namespace scoped)
	CouchbaseBackupRestores *CouchbaseBackupRestoreCache

	// CouchbaseAutoscalers is a read only cache of couchbase autoscaler resources (namespace scoped)
	CouchbaseAutoscalers *CouchbaseAutoscalerCache

	// Jobs is a read only cache of jobs
	Jobs *JobCache

	// CronJobs is a read only cache of cronjobs
	CronJobs *CronJobCache

	// CouchbaseScopes is a read only cache of scopes (namespace scoped)
	CouchbaseScopes *CouchbaseScopeCache

	// CouchbaseScopeGroups is a read only cache of scope groups (namespace scoped)
	CouchbaseScopeGroups *CouchbaseScopeGroupCache

	// CouchbaseCollections is a read only cache of collections (namespace scoped)
	CouchbaseCollections *CouchbaseCollectionCache

	// CouchbaseCollectionGroups is a read only cache of collection groups (namespace scoped)
	CouchbaseCollectionGroups *CouchbaseCollectionGroupCache

	// CouchbaseMigrationReplications is a read only cache of couchbase migration replications (namespace scoped)
	CouchbaseMigrationReplications *CouchbaseMigrationReplicationCache
}

// NewClient initializes all Kubernetes clients and caches.
func NewClient(ctx context.Context, namespace string, selector fmt.Stringer) (*Client, error) {
	c := &Client{}

	var err error

	c.KubeConfig, err = rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	c.KubeClient, err = kubernetes.NewForConfig(c.KubeConfig)
	if err != nil {
		return nil, err
	}

	c.CouchbaseClient, err = versioned.NewForConfig(c.KubeConfig)
	if err != nil {
		return nil, err
	}

	c.Pods, err = newPodCache(ctx, c.KubeClient, namespace, selector)
	if err != nil {
		return nil, err
	}

	c.PersistentVolumeClaims, err = newPersistentVolumeClaimCache(ctx, c.KubeClient, namespace, selector)
	if err != nil {
		return nil, err
	}

	c.Services, err = newServiceCache(ctx, c.KubeClient, namespace, selector)
	if err != nil {
		return nil, err
	}

	c.Secrets, err = newSecretCache(ctx, c.KubeClient, namespace)
	if err != nil {
		return nil, err
	}

	c.PodDisruptionBudgets, err = newPodDisruptionBudgetCache(ctx, c.KubeClient, namespace, selector)
	if err != nil {
		return nil, err
	}

	c.CouchbaseBuckets, err = newCouchbaseBucketCache(ctx, c.CouchbaseClient, namespace)
	if err != nil {
		return nil, err
	}

	c.CouchbaseEphemeralBuckets, err = newCouchbaseEphemeralBucketCache(ctx, c.CouchbaseClient, namespace)
	if err != nil {
		return nil, err
	}

	c.CouchbaseMemcachedBuckets, err = newCouchbaseMemcachedBucketCache(ctx, c.CouchbaseClient, namespace)
	if err != nil {
		return nil, err
	}

	c.CouchbaseReplications, err = newCouchbaseReplicationCache(ctx, c.CouchbaseClient, namespace)
	if err != nil {
		return nil, err
	}

	c.CouchbaseUsers, err = newCouchbaseUserCache(ctx, c.CouchbaseClient, namespace)
	if err != nil {
		return nil, err
	}

	c.CouchbaseGroups, err = newCouchbaseGroupCache(ctx, c.CouchbaseClient, namespace)
	if err != nil {
		return nil, err
	}

	c.CouchbaseRoleBindings, err = newCouchbaseRoleBindingCache(ctx, c.CouchbaseClient, namespace)
	if err != nil {
		return nil, err
	}

	c.Jobs, err = newJobCache(ctx, c.KubeClient, namespace, selector)
	if err != nil {
		return nil, err
	}

	c.CronJobs, err = newCronJobCache(ctx, c.KubeClient, namespace, selector)
	if err != nil {
		return nil, err
	}

	c.CouchbaseBackups, err = newCouchbaseBackupCache(ctx, c.CouchbaseClient, namespace)
	if err != nil {
		return nil, err
	}

	c.CouchbaseBackupRestores, err = newCouchbaseBackupRestoreCache(ctx, c.CouchbaseClient, namespace)
	if err != nil {
		return nil, err
	}

	c.CouchbaseAutoscalers, err = newCouchbaseAutoscalerCache(ctx, c.CouchbaseClient, namespace, selector)
	if err != nil {
		return nil, err
	}

	c.CouchbaseScopes, err = newCouchbaseScopeCache(ctx, c.CouchbaseClient, namespace)
	if err != nil {
		return nil, err
	}

	c.CouchbaseScopeGroups, err = newCouchbaseScopeGroupCache(ctx, c.CouchbaseClient, namespace)
	if err != nil {
		return nil, err
	}

	c.CouchbaseCollections, err = newCouchbaseCollectionCache(ctx, c.CouchbaseClient, namespace)
	if err != nil {
		return nil, err
	}

	c.CouchbaseCollectionGroups, err = newCouchbaseCollectionGroupCache(ctx, c.CouchbaseClient, namespace)
	if err != nil {
		return nil, err
	}

	c.CouchbaseMigrationReplications, err = newCouchbaseMigrationReplicationCache(ctx, c.CouchbaseClient, namespace)
	if err != nil {
		return nil, err
	}

	return c, nil
}

// Shutdown stops all cache synchronization routines.
func (c *Client) Shutdown() {
	c.Pods.stop()
	c.PersistentVolumeClaims.stop()
	c.Services.stop()
	c.Secrets.stop()
	c.PodDisruptionBudgets.stop()
	c.CouchbaseBuckets.stop()
	c.CouchbaseEphemeralBuckets.stop()
	c.CouchbaseMemcachedBuckets.stop()
	c.CouchbaseReplications.stop()
	c.CouchbaseUsers.stop()
	c.CouchbaseGroups.stop()
	c.CouchbaseRoleBindings.stop()
	c.Jobs.stop()
	c.CronJobs.stop()
	c.CouchbaseBackups.stop()
	c.CouchbaseBackupRestores.stop()
	c.CouchbaseAutoscalers.stop()
	c.CouchbaseScopes.stop()
	c.CouchbaseScopeGroups.stop()
	c.CouchbaseCollections.stop()
	c.CouchbaseCollectionGroups.stop()
	c.CouchbaseMigrationReplications.stop()
}
