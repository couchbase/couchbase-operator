package client

import (
	"context"
	"fmt"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	couchbaseclientv2 "github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const (
	resyncPeriod = 5 * time.Minute
)

// resourceCache is a generic cache for any resource type.
type resourceCache struct {
	// informer is a shared informer, it is essentially a cache that is automatically
	// populated and updated by a watch.  It optionally informs subscribers about events
	// to a particular resource type.
	informer cache.SharedInformer

	// stopChan is a channel to signal the cache synchronization process should terminate.
	stopChan chan struct{}
}

// newResourceCache creates a resource cache that is only concerned with resources identified
// by the provided selector.
func newResourceCache(ctx context.Context, client cache.Getter, resource runtime.Object, selector fmt.Stringer, endpoint, namespace string) (*resourceCache, error) {
	// Create a listwatcher to propulate the cache with resources of the specified
	// type.
	optionsModifier := func(options *metav1.ListOptions) {
		options.LabelSelector = selector.String()
	}
	lw := cache.NewFilteredListWatchFromClient(client, endpoint, namespace, optionsModifier)

	// Create the cache object with a shared informer.  We don't worry about being informed
	// we just want the cache.
	rc := &resourceCache{
		informer: cache.NewSharedInformer(lw, resource, resyncPeriod),
		stopChan: make(chan struct{}),
	}

	// Start the informer in the background, it will populate the cache with a list
	// then keep it up to date with watches.
	go rc.informer.Run(rc.stopChan)

	// Wait for the cache to heat up before continuing.
	ready := func() error {
		if !rc.informer.HasSynced() {
			return errors.NewStackTracedError(fmt.Errorf("%w: client has not synced", errors.ErrInternalError))
		}

		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	if err := retryutil.Retry(ctx, time.Second, ready); err != nil {
		rc.stop()
		return nil, err
	}

	return rc, nil
}

// stop stops the cache synchronization and frees resources.
func (rc *resourceCache) stop() {
	rc.stopChan <- struct{}{}
}

// PodCache is a wrapper around a resourceCache that provides concrete typing for
// Pod resources.
type PodCache struct {
	resourceCache *resourceCache
	namespace     string
}

// newPodCache creates a new synchronized cache for Pod resources.
func newPodCache(ctx context.Context, client kubernetes.Interface, namespace string, selector fmt.Stringer) (*PodCache, error) {
	resourceCache, err := newResourceCache(ctx, client.CoreV1().RESTClient(), &corev1.Pod{}, selector, "pods", namespace)
	if err != nil {
		return nil, err
	}

	return &PodCache{
		resourceCache: resourceCache,
		namespace:     namespace,
	}, nil
}

// get returns the requested pod based on name.
func (c *PodCache) Get(name string) (*corev1.Pod, bool) {
	key := c.namespace + "/" + name

	// Cannot error
	obj, exists, _ := c.resourceCache.informer.GetStore().GetByKey(key)
	if !exists {
		return nil, exists
	}

	return obj.(*corev1.Pod), true
}

// list returns pods by label matchers.
func (c *PodCache) List(label string) (pods []*corev1.Pod) {
	objs := c.resourceCache.informer.GetStore().List()
	for _, obj := range objs {
		pod, ok := obj.(*corev1.Pod)
		if ok {
			// Avoid polling deleted pods
			if pod.DeletionTimestamp != nil {
				continue
			}

			if _, exists := pod.Labels[label]; exists {
				pods = append(pods, pod)
			}
		}
	}

	return
}

// stop stops the cache synchronization and frees resources.
func (c *PodCache) stop() {
	c.resourceCache.stop()
}

// PersistentVolumeClaimCache is a wrapper around a resourceCache that provides concrete typing for
// PersistentVolumeClaim resources.
type PersistentVolumeClaimCache struct {
	resourceCache *resourceCache
	namespace     string
}

// newPersistentVolumeClaimCache creates a new synchronized cache for Pod resources.
func newPersistentVolumeClaimCache(ctx context.Context, client kubernetes.Interface, namespace string, selector fmt.Stringer) (*PersistentVolumeClaimCache, error) {
	resourceCache, err := newResourceCache(ctx, client.CoreV1().RESTClient(), &corev1.PersistentVolumeClaim{}, selector, "persistentvolumeclaims", namespace)
	if err != nil {
		return nil, err
	}

	return &PersistentVolumeClaimCache{
		resourceCache: resourceCache,
		namespace:     namespace,
	}, nil
}

// get returns the requested persistentvolumeclaim based on name.
func (c *PersistentVolumeClaimCache) Get(name string) (*corev1.PersistentVolumeClaim, bool) {
	key := c.namespace + "/" + name

	// Cannot error
	obj, exists, _ := c.resourceCache.informer.GetStore().GetByKey(key)
	if !exists {
		return nil, exists
	}

	return obj.(*corev1.PersistentVolumeClaim), true
}

// list returns all persistentvolumeclaims.
func (c *PersistentVolumeClaimCache) List() (pvcs []*corev1.PersistentVolumeClaim) {
	objs := c.resourceCache.informer.GetStore().List()
	for _, obj := range objs {
		pvcs = append(pvcs, obj.(*corev1.PersistentVolumeClaim))
	}

	return
}

// stop stops the cache synchronization and frees resources.
func (c *PersistentVolumeClaimCache) stop() {
	c.resourceCache.stop()
}

// ServiceCache is a wrapper around a resourceCache that provides concrete typing for
// Service resources.
type ServiceCache struct {
	resourceCache *resourceCache
	namespace     string
}

// newServiceCache creates a new synchronized cache for Service resources.
func newServiceCache(ctx context.Context, client kubernetes.Interface, namespace string, selector fmt.Stringer) (*ServiceCache, error) {
	resourceCache, err := newResourceCache(ctx, client.CoreV1().RESTClient(), &corev1.Service{}, selector, "services", namespace)
	if err != nil {
		return nil, err
	}

	return &ServiceCache{
		resourceCache: resourceCache,
		namespace:     namespace,
	}, nil
}

// get returns the requested service based on name.
func (c *ServiceCache) Get(name string) (*corev1.Service, bool) {
	key := c.namespace + "/" + name

	// Cannot error
	obj, exists, _ := c.resourceCache.informer.GetStore().GetByKey(key)
	if !exists {
		return nil, exists
	}

	return obj.(*corev1.Service), true
}

// list returns all services.
func (c *ServiceCache) List() (svcs []*corev1.Service) {
	objs := c.resourceCache.informer.GetStore().List()
	for _, obj := range objs {
		svcs = append(svcs, obj.(*corev1.Service))
	}

	return
}

// stop stops the cache synchronization and frees resources.
func (c *ServiceCache) stop() {
	c.resourceCache.stop()
}

// SecretCache is a wrapper around a resourceCache that provides concrete typing for
// Secret resources.
type SecretCache struct {
	resourceCache *resourceCache
	namespace     string
}

// newSecretCache creates a new synchronized cache for Secret resources.
func newSecretCache(ctx context.Context, client kubernetes.Interface, namespace string) (*SecretCache, error) {
	selector := labels.Everything()

	resourceCache, err := newResourceCache(ctx, client.CoreV1().RESTClient(), &corev1.Secret{}, selector, "secrets", namespace)
	if err != nil {
		return nil, err
	}

	return &SecretCache{
		resourceCache: resourceCache,
		namespace:     namespace,
	}, nil
}

// get returns the requested secret based on name.
func (c *SecretCache) Get(name string) (*corev1.Secret, bool) {
	key := c.namespace + "/" + name

	// Cannot error
	obj, exists, _ := c.resourceCache.informer.GetStore().GetByKey(key)
	if !exists {
		return nil, exists
	}

	return obj.(*corev1.Secret), true
}

// list returns all secrets.
func (c *SecretCache) List() (secrets []*corev1.Secret) {
	objs := c.resourceCache.informer.GetStore().List()
	for _, obj := range objs {
		secrets = append(secrets, obj.(*corev1.Secret))
	}

	return
}

// stop stops the cache synchronization and frees resources.
func (c *SecretCache) stop() {
	c.resourceCache.stop()
}

// PodDisruptionBudgetCache is a wrapper around a resourceCache that provides concrete typing for
// PodDisruptionBudget resources.
type PodDisruptionBudgetCache struct {
	resourceCache *resourceCache
	namespace     string
}

// newPodDisruptionBudgetCache creates a new synchronized cache for PodDisruptionBudget resources.
func newPodDisruptionBudgetCache(ctx context.Context, client kubernetes.Interface, namespace string, selector fmt.Stringer) (*PodDisruptionBudgetCache, error) {
	resourceCache, err := newResourceCache(ctx, client.PolicyV1().RESTClient(), &policyv1.PodDisruptionBudget{}, selector, "poddisruptionbudgets", namespace)
	if err != nil {
		return nil, err
	}

	return &PodDisruptionBudgetCache{
		resourceCache: resourceCache,
		namespace:     namespace,
	}, nil
}

// get returns the requested poddisruptionbudget based on name.
func (c *PodDisruptionBudgetCache) Get(name string) (*policyv1.PodDisruptionBudget, bool) {
	key := c.namespace + "/" + name

	// Cannot error
	obj, exists, _ := c.resourceCache.informer.GetStore().GetByKey(key)
	if !exists {
		return nil, exists
	}

	return obj.(*policyv1.PodDisruptionBudget), true
}

// list returns all poddisruptionbudgets.
func (c *PodDisruptionBudgetCache) List() (poddisruptionbudgets []*policyv1.PodDisruptionBudget) {
	objs := c.resourceCache.informer.GetStore().List()
	for _, obj := range objs {
		poddisruptionbudgets = append(poddisruptionbudgets, obj.(*policyv1.PodDisruptionBudget))
	}

	return
}

// stop stops the cache synchronization and frees resources.
func (c *PodDisruptionBudgetCache) stop() {
	c.resourceCache.stop()
}

// CouchbaseBucketCache is a wrapper around a resourceCache that provides concrete typing for
// CouchbaseBucket resources.
type CouchbaseBucketCache struct {
	resourceCache *resourceCache
	namespace     string
}

// newCouchbaseBucketCache creates a new synchronized cache for CouchbaseBucket resources.
func newCouchbaseBucketCache(ctx context.Context, client couchbaseclientv2.Interface, namespace string) (*CouchbaseBucketCache, error) {
	selector := labels.Everything()

	resourceCache, err := newResourceCache(ctx, client.CouchbaseV2().RESTClient(), &couchbasev2.CouchbaseBucket{}, selector, couchbasev2.BucketCRDResourcePlural, namespace)
	if err != nil {
		return nil, err
	}

	return &CouchbaseBucketCache{
		resourceCache: resourceCache,
		namespace:     namespace,
	}, nil
}

// get returns the requested couchbasebucket based on name.
func (c *CouchbaseBucketCache) Get(name string) (*couchbasev2.CouchbaseBucket, bool) {
	key := c.namespace + "/" + name

	// Cannot error
	obj, exists, _ := c.resourceCache.informer.GetStore().GetByKey(key)
	if !exists {
		return nil, exists
	}

	return obj.(*couchbasev2.CouchbaseBucket), true
}

// list returns all couchbasebuckets.
func (c *CouchbaseBucketCache) List() (resources []*couchbasev2.CouchbaseBucket) {
	objs := c.resourceCache.informer.GetStore().List()
	for _, obj := range objs {
		resources = append(resources, obj.(*couchbasev2.CouchbaseBucket))
	}

	return
}

func (c *CouchbaseBucketCache) Update(resource *couchbasev2.CouchbaseBucket) error {
	return c.resourceCache.informer.GetStore().Update(resource)
}

// stop stops the cache synchronization and frees resources.
func (c *CouchbaseBucketCache) stop() {
	c.resourceCache.stop()
}

// CouchbaseEphemeralBucketCache is a wrapper around a resourceCache that provides concrete typing for
// CouchbaseEphemeralBucket resources.
type CouchbaseEphemeralBucketCache struct {
	resourceCache *resourceCache
	namespace     string
}

// newCouchbaseEphemeralBucketCache creates a new synchronized cache for CouchbaseEphemeralBucket resources.
func newCouchbaseEphemeralBucketCache(ctx context.Context, client couchbaseclientv2.Interface, namespace string) (*CouchbaseEphemeralBucketCache, error) {
	selector := labels.Everything()

	resourceCache, err := newResourceCache(ctx, client.CouchbaseV2().RESTClient(), &couchbasev2.CouchbaseEphemeralBucket{}, selector, couchbasev2.EphemeralBucketCRDResourcePlural, namespace)
	if err != nil {
		return nil, err
	}

	return &CouchbaseEphemeralBucketCache{
		resourceCache: resourceCache,
		namespace:     namespace,
	}, nil
}

// get returns the requested couchbaseephemeralbucket based on name.
func (c *CouchbaseEphemeralBucketCache) Get(name string) (*couchbasev2.CouchbaseEphemeralBucket, bool) {
	key := c.namespace + "/" + name

	// Cannot error
	obj, exists, _ := c.resourceCache.informer.GetStore().GetByKey(key)
	if !exists {
		return nil, exists
	}

	return obj.(*couchbasev2.CouchbaseEphemeralBucket), true
}

// list returns all couchbaseephemeralbuckets.
func (c *CouchbaseEphemeralBucketCache) List() (resources []*couchbasev2.CouchbaseEphemeralBucket) {
	objs := c.resourceCache.informer.GetStore().List()
	for _, obj := range objs {
		resources = append(resources, obj.(*couchbasev2.CouchbaseEphemeralBucket))
	}

	return
}

func (c *CouchbaseEphemeralBucketCache) Update(resource *couchbasev2.CouchbaseEphemeralBucket) error {
	return c.resourceCache.informer.GetStore().Update(resource)
}

// stop stops the cache synchronization and frees resources.
func (c *CouchbaseEphemeralBucketCache) stop() {
	c.resourceCache.stop()
}

// CouchbaseMemcachedBucketCache is a wrapper around a resourceCache that provides concrete typing for
// CouchbaseMemcachedBucket resources.
type CouchbaseMemcachedBucketCache struct {
	resourceCache *resourceCache
	namespace     string
}

// newCouchbaseMemcachedBucketCache creates a new synchronized cache for CouchbaseMemcachedBucket resources.
func newCouchbaseMemcachedBucketCache(ctx context.Context, client couchbaseclientv2.Interface, namespace string) (*CouchbaseMemcachedBucketCache, error) {
	selector := labels.Everything()

	resourceCache, err := newResourceCache(ctx, client.CouchbaseV2().RESTClient(), &couchbasev2.CouchbaseMemcachedBucket{}, selector, couchbasev2.MemcachedBucketCRDResourcePlural, namespace)
	if err != nil {
		return nil, err
	}

	return &CouchbaseMemcachedBucketCache{
		resourceCache: resourceCache,
		namespace:     namespace,
	}, nil
}

// get returns the requested couchbasememcachedbucketbased on name.
func (c *CouchbaseMemcachedBucketCache) Get(name string) (*couchbasev2.CouchbaseMemcachedBucket, bool) {
	key := c.namespace + "/" + name

	// Cannot error
	obj, exists, _ := c.resourceCache.informer.GetStore().GetByKey(key)
	if !exists {
		return nil, exists
	}

	return obj.(*couchbasev2.CouchbaseMemcachedBucket), true
}

// list returns all couchbasememcachedbuckets.
func (c *CouchbaseMemcachedBucketCache) List() (resources []*couchbasev2.CouchbaseMemcachedBucket) {
	objs := c.resourceCache.informer.GetStore().List()
	for _, obj := range objs {
		resources = append(resources, obj.(*couchbasev2.CouchbaseMemcachedBucket))
	}

	return
}

func (c *CouchbaseMemcachedBucketCache) Update(resource *couchbasev2.CouchbaseMemcachedBucket) error {
	return c.resourceCache.informer.GetStore().Update(resource)
}

// stop stops the cache synchronization and frees resources.
func (c *CouchbaseMemcachedBucketCache) stop() {
	c.resourceCache.stop()
}

// CouchbaseReplicationCache is a wrapper around a resourceCache that provides concrete typing for
// CouchbaseReplication resources.
type CouchbaseReplicationCache struct {
	resourceCache *resourceCache
	namespace     string
}

// newCouchbaseReplicationCache creates a new synchronized cache for CouchbaseReplication resources.
func newCouchbaseReplicationCache(ctx context.Context, client couchbaseclientv2.Interface, namespace string) (*CouchbaseReplicationCache, error) {
	selector := labels.Everything()

	resourceCache, err := newResourceCache(ctx, client.CouchbaseV2().RESTClient(), &couchbasev2.CouchbaseReplication{}, selector, couchbasev2.ReplicationCRDResourcePlural, namespace)
	if err != nil {
		return nil, err
	}

	return &CouchbaseReplicationCache{
		resourceCache: resourceCache,
		namespace:     namespace,
	}, nil
}

// get returns the requested couchbasereplication based on name.
func (c *CouchbaseReplicationCache) Get(name string) (*couchbasev2.CouchbaseReplication, bool) {
	key := c.namespace + "/" + name

	// Cannot error
	obj, exists, _ := c.resourceCache.informer.GetStore().GetByKey(key)
	if !exists {
		return nil, exists
	}

	return obj.(*couchbasev2.CouchbaseReplication), true
}

// list returns all couchbasereplications.
func (c *CouchbaseReplicationCache) List() (resources []*couchbasev2.CouchbaseReplication) {
	objs := c.resourceCache.informer.GetStore().List()
	for _, obj := range objs {
		resources = append(resources, obj.(*couchbasev2.CouchbaseReplication))
	}

	return
}

// stop stops the cache synchronization and frees resources.
func (c *CouchbaseReplicationCache) stop() {
	c.resourceCache.stop()
}

// CouchbaseUserCache is a wrapper around a resourceCache that provides concrete typing for
// CouchbaseUser resources.
type CouchbaseUserCache struct {
	resourceCache *resourceCache
	namespace     string
}

// newCouchbaseiUserCache creates a new synchronized cache for CouchbaseUser resources.
func newCouchbaseUserCache(ctx context.Context, client couchbaseclientv2.Interface, namespace string) (*CouchbaseUserCache, error) {
	selector := labels.Everything()

	resourceCache, err := newResourceCache(ctx, client.CouchbaseV2().RESTClient(), &couchbasev2.CouchbaseUser{}, selector, couchbasev2.UserCRDResourcePlural, namespace)
	if err != nil {
		return nil, err
	}

	return &CouchbaseUserCache{
		resourceCache: resourceCache,
		namespace:     namespace,
	}, nil
}

// get returns the requested couchbaseuser based on name.
func (c *CouchbaseUserCache) Get(name string) (*couchbasev2.CouchbaseUser, bool) {
	key := c.namespace + "/" + name

	// Cannot error
	obj, exists, _ := c.resourceCache.informer.GetStore().GetByKey(key)
	if !exists {
		return nil, exists
	}

	return obj.(*couchbasev2.CouchbaseUser), true
}

// list returns all couchbaseusers.
func (c *CouchbaseUserCache) List() (resources []*couchbasev2.CouchbaseUser) {
	objs := c.resourceCache.informer.GetStore().List()
	for _, obj := range objs {
		resources = append(resources, obj.(*couchbasev2.CouchbaseUser))
	}

	return
}

func (c *CouchbaseUserCache) Update(resource *couchbasev2.CouchbaseUser) error {
	return c.resourceCache.informer.GetStore().Update(resource)
}

// stop stops the cache synchronization and frees resources.
func (c *CouchbaseUserCache) stop() {
	c.resourceCache.stop()
}

// CouchbaseGroupCache is a wrapper around a resourceCache that provides concrete typing for
// CouchbaseGroup resources.
type CouchbaseGroupCache struct {
	resourceCache *resourceCache
	namespace     string
}

// newCouchbaseiGroupCache creates a new synchronized cache for CouchbaseGroup resources.
func newCouchbaseGroupCache(ctx context.Context, client couchbaseclientv2.Interface, namespace string) (*CouchbaseGroupCache, error) {
	selector := labels.Everything()

	resourceCache, err := newResourceCache(ctx, client.CouchbaseV2().RESTClient(), &couchbasev2.CouchbaseGroup{}, selector, couchbasev2.GroupCRDResourcePlural, namespace)
	if err != nil {
		return nil, err
	}

	return &CouchbaseGroupCache{
		resourceCache: resourceCache,
		namespace:     namespace,
	}, nil
}

// get returns the requested couchbaseuser based on name.
func (c *CouchbaseGroupCache) Get(name string) (*couchbasev2.CouchbaseGroup, bool) {
	key := c.namespace + "/" + name

	// Cannot error
	obj, exists, _ := c.resourceCache.informer.GetStore().GetByKey(key)
	if !exists {
		return nil, exists
	}

	return obj.(*couchbasev2.CouchbaseGroup), true
}

// list returns all couchbaseusers.
func (c *CouchbaseGroupCache) List() (resources []*couchbasev2.CouchbaseGroup) {
	objs := c.resourceCache.informer.GetStore().List()
	for _, obj := range objs {
		resources = append(resources, obj.(*couchbasev2.CouchbaseGroup))
	}

	return
}

func (c *CouchbaseGroupCache) Update(resource *couchbasev2.CouchbaseGroup) error {
	return c.resourceCache.informer.GetStore().Update(resource)
}

// stop stops the cache synchronization and frees resources.
func (c *CouchbaseGroupCache) stop() {
	c.resourceCache.stop()
}

// CouchbaseRoleBindingCache is a wrapper around a resourceCache that provides concrete typing for
// CouchbaseRoleBinding resources.
type CouchbaseRoleBindingCache struct {
	resourceCache *resourceCache
	namespace     string
}

// newCouchbaseRoleBindingCache creates a new synchronized cache for CouchbaseRoleBinding resources.
func newCouchbaseRoleBindingCache(ctx context.Context, client couchbaseclientv2.Interface, namespace string) (*CouchbaseRoleBindingCache, error) {
	selector := labels.Everything()

	resourceCache, err := newResourceCache(ctx, client.CouchbaseV2().RESTClient(), &couchbasev2.CouchbaseRoleBinding{}, selector, couchbasev2.RoleBindingCRDResourcePlural, namespace)
	if err != nil {
		return nil, err
	}

	return &CouchbaseRoleBindingCache{
		resourceCache: resourceCache,
		namespace:     namespace,
	}, nil
}

// get returns the requested couchbaserolebinding based on name.
func (c *CouchbaseRoleBindingCache) Get(name string) (*couchbasev2.CouchbaseRoleBinding, bool) {
	key := c.namespace + "/" + name

	// Cannot error
	obj, exists, _ := c.resourceCache.informer.GetStore().GetByKey(key)
	if !exists {
		return nil, exists
	}

	return obj.(*couchbasev2.CouchbaseRoleBinding), true
}

// list returns all couchbaserolebindings.
func (c *CouchbaseRoleBindingCache) List() (resources []*couchbasev2.CouchbaseRoleBinding) {
	objs := c.resourceCache.informer.GetStore().List()
	for _, obj := range objs {
		resources = append(resources, obj.(*couchbasev2.CouchbaseRoleBinding))
	}

	return
}

// stop stops the cache synchronization and frees resources.
func (c *CouchbaseRoleBindingCache) stop() {
	c.resourceCache.stop()
}

// JobCache is a wrapper around a resourceCache that provides concrete typing for
// Job resources.
type JobCache struct {
	resourceCache *resourceCache
	namespace     string
}

// newJobCache creates a new synchronized cache for Job resources.
func newJobCache(ctx context.Context, client kubernetes.Interface, namespace string, selector fmt.Stringer) (*JobCache, error) {
	resourceCache, err := newResourceCache(ctx, client.BatchV1().RESTClient(), &batchv1.Job{}, selector, "jobs", namespace)
	if err != nil {
		return nil, err
	}

	return &JobCache{
		resourceCache: resourceCache,
		namespace:     namespace,
	}, nil
}

// get returns the requested job based on name.
func (c *JobCache) Get(name string) (*batchv1.Job, bool) {
	key := c.namespace + "/" + name

	// Cannot error
	obj, exists, _ := c.resourceCache.informer.GetStore().GetByKey(key)
	if !exists {
		return nil, exists
	}

	return obj.(*batchv1.Job), true
}

// list returns all jobs.
func (c *JobCache) List() (resources []*batchv1.Job) {
	objs := c.resourceCache.informer.GetStore().List()
	for _, obj := range objs {
		resources = append(resources, obj.(*batchv1.Job))
	}

	return
}

// stop stops the cache synchronization and frees resources.
func (c *JobCache) stop() {
	c.resourceCache.stop()
}

// JobCache is a wrapper around a resourceCache that provides concrete typing for
// Job resources.
type CronJobCache struct {
	resourceCache *resourceCache
	namespace     string
}

// newJobCache creates a new synchronized cache for Job resources.
func newCronJobCache(ctx context.Context, client kubernetes.Interface, namespace string, selector fmt.Stringer) (*CronJobCache, error) {
	resourceCache, err := newResourceCache(ctx, client.BatchV1().RESTClient(), &batchv1.CronJob{}, selector, "cronjobs", namespace)
	if err != nil {
		return nil, err
	}

	return &CronJobCache{
		resourceCache: resourceCache,
		namespace:     namespace,
	}, nil
}

// get returns the requested job based on name.
func (c *CronJobCache) Get(name string) (*batchv1.CronJob, bool) {
	key := c.namespace + "/" + name

	// Cannot error
	obj, exists, _ := c.resourceCache.informer.GetStore().GetByKey(key)
	if !exists {
		return nil, exists
	}

	return obj.(*batchv1.CronJob), true
}

// list returns all jobs.
func (c *CronJobCache) List() (resources []*batchv1.CronJob) {
	objs := c.resourceCache.informer.GetStore().List()
	for _, obj := range objs {
		resources = append(resources, obj.(*batchv1.CronJob))
	}

	return
}

// stop stops the cache synchronization and frees resources.
func (c *CronJobCache) stop() {
	c.resourceCache.stop()
}

// ConfigMapCache is a wrapper around a resourceCache that provides concrete typing for
// ConfigMap resources.
type ConfigMapCache struct {
	resourceCache *resourceCache
	namespace     string
}

// newConfigMapCache creates a new synchronized cache for ConfigMap resources.
func newConfigMapCache(ctx context.Context, client kubernetes.Interface, namespace string, selector fmt.Stringer) (*ConfigMapCache, error) {
	resourceCache, err := newResourceCache(ctx, client.CoreV1().RESTClient(), &corev1.ConfigMap{}, selector, "configmaps", namespace)
	if err != nil {
		return nil, err
	}

	return &ConfigMapCache{
		resourceCache: resourceCache,
		namespace:     namespace,
	}, nil
}

// get returns the requested configmap based on name.
func (c *ConfigMapCache) Get(name string) (*corev1.ConfigMap, bool) {
	key := c.namespace + "/" + name

	// Cannot error
	obj, exists, _ := c.resourceCache.informer.GetStore().GetByKey(key)
	if !exists {
		return nil, exists
	}

	return obj.(*corev1.ConfigMap), true
}

// list returns all configmaps.
func (c *ConfigMapCache) List() (resources []*corev1.ConfigMap) {
	objs := c.resourceCache.informer.GetStore().List()
	for _, obj := range objs {
		resources = append(resources, obj.(*corev1.ConfigMap))
	}

	return
}

// stop stops the cache synchronization and frees resources.
func (c *ConfigMapCache) stop() {
	c.resourceCache.stop()
}

// CouchbaseBackupCache is a wrapper around a resourceCache that provides concrete typing for
// CouchbaseBackup resources.
type CouchbaseBackupCache struct {
	resourceCache *resourceCache
	namespace     string
}

// newCouchbaseBackupCache creates a new synchronized cache for CouchbaseBackup resources.
func newCouchbaseBackupCache(ctx context.Context, client couchbaseclientv2.Interface, namespace string) (*CouchbaseBackupCache, error) {
	selector := labels.Everything()

	resourceCache, err := newResourceCache(ctx, client.CouchbaseV2().RESTClient(), &couchbasev2.CouchbaseBackup{}, selector, couchbasev2.BackupCRDResourcePlural, namespace)
	if err != nil {
		return nil, err
	}

	return &CouchbaseBackupCache{
		resourceCache: resourceCache,
		namespace:     namespace,
	}, nil
}

// get returns the requested couchbasebackup based on name.
func (c *CouchbaseBackupCache) Get(name string) (*couchbasev2.CouchbaseBackup, bool) {
	key := c.namespace + "/" + name

	// Cannot error
	obj, exists, _ := c.resourceCache.informer.GetStore().GetByKey(key)
	if !exists {
		return nil, exists
	}

	return obj.(*couchbasev2.CouchbaseBackup), true
}

// list returns all couchbasebackups.
func (c *CouchbaseBackupCache) List() (resources []*couchbasev2.CouchbaseBackup) {
	objs := c.resourceCache.informer.GetStore().List()
	for _, obj := range objs {
		resources = append(resources, obj.(*couchbasev2.CouchbaseBackup))
	}

	return
}

func (c *CouchbaseBackupCache) Update(resource *couchbasev2.CouchbaseBackup) error {
	return c.resourceCache.informer.GetStore().Update(resource)
}

// stop stops the cache synchronization and frees resources.
func (c *CouchbaseBackupCache) stop() {
	c.resourceCache.stop()
}

// CouchbaseBackupRestoreCache is a wrapper around a resourceCache that provides concrete typing for
// CouchbaseBackupRestore resources.
type CouchbaseBackupRestoreCache struct {
	resourceCache *resourceCache
	namespace     string
}

// newCouchbaseBackupRestoreCache creates a new synchronized cache for CouchbaseBackupRestore resources.
func newCouchbaseBackupRestoreCache(ctx context.Context, client couchbaseclientv2.Interface, namespace string) (*CouchbaseBackupRestoreCache, error) {
	selector := labels.Everything()

	resourceCache, err := newResourceCache(ctx, client.CouchbaseV2().RESTClient(), &couchbasev2.CouchbaseBackupRestore{}, selector, couchbasev2.BackupRestoreCRDResourcePlural, namespace)
	if err != nil {
		return nil, err
	}

	return &CouchbaseBackupRestoreCache{
		resourceCache: resourceCache,
		namespace:     namespace,
	}, nil
}

// get returns the requested couchbasebackuprestore based on name.
func (c *CouchbaseBackupRestoreCache) Get(name string) (*couchbasev2.CouchbaseBackupRestore, bool) {
	key := c.namespace + "/" + name

	// Cannot error
	obj, exists, _ := c.resourceCache.informer.GetStore().GetByKey(key)
	if !exists {
		return nil, exists
	}

	return obj.(*couchbasev2.CouchbaseBackupRestore), true
}

// list returns all couchbasebackuprestores.
func (c *CouchbaseBackupRestoreCache) List() (resources []*couchbasev2.CouchbaseBackupRestore) {
	objs := c.resourceCache.informer.GetStore().List()
	for _, obj := range objs {
		resources = append(resources, obj.(*couchbasev2.CouchbaseBackupRestore))
	}

	return
}

func (c *CouchbaseBackupRestoreCache) Update(resource *couchbasev2.CouchbaseBackupRestore) error {
	return c.resourceCache.informer.GetStore().Update(resource)
}

// stop stops the cache synchronization and frees resources.
func (c *CouchbaseBackupRestoreCache) stop() {
	c.resourceCache.stop()
}

// CouchbaseAutoscalerCache is a wrapper around a resourceCache that provides concrete typing for
// CouchbaseAutoscaler resources.
type CouchbaseAutoscalerCache struct {
	resourceCache *resourceCache
	namespace     string
}

// newCouchbaseiAutoscalerCache creates a new synchronized cache for CouchbaseAutoscaler resources.
func newCouchbaseAutoscalerCache(ctx context.Context, client couchbaseclientv2.Interface, namespace string, selector fmt.Stringer) (*CouchbaseAutoscalerCache, error) {
	resourceCache, err := newResourceCache(ctx, client.CouchbaseV2().RESTClient(), &couchbasev2.CouchbaseAutoscaler{}, selector, couchbasev2.AutoscalerCRDResourcePlural, namespace)
	if err != nil {
		return nil, err
	}

	return &CouchbaseAutoscalerCache{
		resourceCache: resourceCache,
		namespace:     namespace,
	}, nil
}

// get returns the requested couchbaseautoscaler based on name.
func (c *CouchbaseAutoscalerCache) Get(name string) (*couchbasev2.CouchbaseAutoscaler, bool) {
	key := c.namespace + "/" + name

	// Cannot error
	obj, exists, _ := c.resourceCache.informer.GetStore().GetByKey(key)
	if !exists {
		return nil, exists
	}

	return obj.(*couchbasev2.CouchbaseAutoscaler), true
}

// list returns all couchbaseautoscalers.
func (c *CouchbaseAutoscalerCache) List() (resources []*couchbasev2.CouchbaseAutoscaler) {
	objs := c.resourceCache.informer.GetStore().List()
	for _, obj := range objs {
		resources = append(resources, obj.(*couchbasev2.CouchbaseAutoscaler))
	}

	return
}

func (c *CouchbaseAutoscalerCache) Update(resource *couchbasev2.CouchbaseAutoscaler) error {
	return c.resourceCache.informer.GetStore().Update(resource)
}

// stop stops the cache synchronization and frees resources.
func (c *CouchbaseAutoscalerCache) stop() {
	c.resourceCache.stop()
}

// CouchbaseScopeCache is a wrapper around a resourceCache that provides concrete typing for
// CouchbaseScope resources.
type CouchbaseScopeCache struct {
	resourceCache *resourceCache
	namespace     string
}

// newCouchbaseScopeCache creates a new synchronized cache.
func newCouchbaseScopeCache(ctx context.Context, client couchbaseclientv2.Interface, namespace string) (*CouchbaseScopeCache, error) {
	selector := labels.Everything()

	resourceCache, err := newResourceCache(ctx, client.CouchbaseV2().RESTClient(), &couchbasev2.CouchbaseScope{}, selector, couchbasev2.ScopeCRDResourcePlural, namespace)
	if err != nil {
		return nil, err
	}

	return &CouchbaseScopeCache{
		resourceCache: resourceCache,
		namespace:     namespace,
	}, nil
}

// get returns the requested resource based on name.
func (c *CouchbaseScopeCache) Get(name string) (*couchbasev2.CouchbaseScope, bool) {
	key := c.namespace + "/" + name

	// Cannot error
	obj, exists, _ := c.resourceCache.informer.GetStore().GetByKey(key)
	if !exists {
		return nil, exists
	}

	return obj.(*couchbasev2.CouchbaseScope), true
}

// list returns all resources.
func (c *CouchbaseScopeCache) List() (resources []*couchbasev2.CouchbaseScope) {
	objs := c.resourceCache.informer.GetStore().List()
	for _, obj := range objs {
		resources = append(resources, obj.(*couchbasev2.CouchbaseScope))
	}

	return
}

func (c *CouchbaseScopeCache) Update(resource *couchbasev2.CouchbaseScope) error {
	return c.resourceCache.informer.GetStore().Update(resource)
}

// stop stops the cache synchronization and frees resources.
func (c *CouchbaseScopeCache) stop() {
	c.resourceCache.stop()
}

// CouchbaseScopeGroupCache is a wrapper around a resourceCache that provides concrete typing for
// CouchbaseScopeGroup resources.
type CouchbaseScopeGroupCache struct {
	resourceCache *resourceCache
	namespace     string
}

// newCouchbaseScopeGroupCache creates a new synchronized cache.
func newCouchbaseScopeGroupCache(ctx context.Context, client couchbaseclientv2.Interface, namespace string) (*CouchbaseScopeGroupCache, error) {
	selector := labels.Everything()

	resourceCache, err := newResourceCache(ctx, client.CouchbaseV2().RESTClient(), &couchbasev2.CouchbaseScopeGroup{}, selector, couchbasev2.ScopeGroupCRDResourcePlural, namespace)
	if err != nil {
		return nil, err
	}

	return &CouchbaseScopeGroupCache{
		resourceCache: resourceCache,
		namespace:     namespace,
	}, nil
}

// get returns the requested resource based on name.
func (c *CouchbaseScopeGroupCache) Get(name string) (*couchbasev2.CouchbaseScopeGroup, bool) {
	key := c.namespace + "/" + name

	// Cannot error
	obj, exists, _ := c.resourceCache.informer.GetStore().GetByKey(key)
	if !exists {
		return nil, exists
	}

	return obj.(*couchbasev2.CouchbaseScopeGroup), true
}

// list returns all resources.
func (c *CouchbaseScopeGroupCache) List() (resources []*couchbasev2.CouchbaseScopeGroup) {
	objs := c.resourceCache.informer.GetStore().List()
	for _, obj := range objs {
		resources = append(resources, obj.(*couchbasev2.CouchbaseScopeGroup))
	}

	return
}

func (c *CouchbaseScopeGroupCache) Update(resource *couchbasev2.CouchbaseScopeGroup) error {
	return c.resourceCache.informer.GetStore().Update(resource)
}

// stop stops the cache synchronization and frees resources.
func (c *CouchbaseScopeGroupCache) stop() {
	c.resourceCache.stop()
}

// CouchbaseCollectionCache is a wrapper around a resourceCache that provides concrete typing for
// CouchbaseCollection resources.
type CouchbaseCollectionCache struct {
	resourceCache *resourceCache
	namespace     string
}

// newCouchbaseCollectionCache creates a new synchronized cache.
func newCouchbaseCollectionCache(ctx context.Context, client couchbaseclientv2.Interface, namespace string) (*CouchbaseCollectionCache, error) {
	selector := labels.Everything()

	resourceCache, err := newResourceCache(ctx, client.CouchbaseV2().RESTClient(), &couchbasev2.CouchbaseCollection{}, selector, couchbasev2.CollectionCRDResourcePlural, namespace)
	if err != nil {
		return nil, err
	}

	return &CouchbaseCollectionCache{
		resourceCache: resourceCache,
		namespace:     namespace,
	}, nil
}

// get returns the requested resource based on name.
func (c *CouchbaseCollectionCache) Get(name string) (*couchbasev2.CouchbaseCollection, bool) {
	key := c.namespace + "/" + name

	// Cannot error
	obj, exists, _ := c.resourceCache.informer.GetStore().GetByKey(key)
	if !exists {
		return nil, exists
	}

	return obj.(*couchbasev2.CouchbaseCollection), true
}

// list returns all resources.
func (c *CouchbaseCollectionCache) List() (resources []*couchbasev2.CouchbaseCollection) {
	objs := c.resourceCache.informer.GetStore().List()
	for _, obj := range objs {
		resources = append(resources, obj.(*couchbasev2.CouchbaseCollection))
	}

	return
}

func (c *CouchbaseCollectionCache) Update(resource *couchbasev2.CouchbaseCollection) error {
	return c.resourceCache.informer.GetStore().Update(resource)
}

// stop stops the cache synchronization and frees resources.
func (c *CouchbaseCollectionCache) stop() {
	c.resourceCache.stop()
}

// CouchbaseCollectionGroupCache is a wrapper around a resourceCache that provides concrete typing for
// CouchbaseCollectionGroup resources.
type CouchbaseCollectionGroupCache struct {
	resourceCache *resourceCache
	namespace     string
}

// newCouchbaseCollectionGroupCache creates a new synchronized cache.
func newCouchbaseCollectionGroupCache(ctx context.Context, client couchbaseclientv2.Interface, namespace string) (*CouchbaseCollectionGroupCache, error) {
	selector := labels.Everything()

	resourceCache, err := newResourceCache(ctx, client.CouchbaseV2().RESTClient(), &couchbasev2.CouchbaseCollectionGroup{}, selector, couchbasev2.CollectionGroupCRDResourcePlural, namespace)
	if err != nil {
		return nil, err
	}

	return &CouchbaseCollectionGroupCache{
		resourceCache: resourceCache,
		namespace:     namespace,
	}, nil
}

// get returns the requested resource based on name.
func (c *CouchbaseCollectionGroupCache) Get(name string) (*couchbasev2.CouchbaseCollectionGroup, bool) {
	key := c.namespace + "/" + name

	// Cannot error
	obj, exists, _ := c.resourceCache.informer.GetStore().GetByKey(key)
	if !exists {
		return nil, exists
	}

	return obj.(*couchbasev2.CouchbaseCollectionGroup), true
}

// list returns all resources.
func (c *CouchbaseCollectionGroupCache) List() (resources []*couchbasev2.CouchbaseCollectionGroup) {
	objs := c.resourceCache.informer.GetStore().List()
	for _, obj := range objs {
		resources = append(resources, obj.(*couchbasev2.CouchbaseCollectionGroup))
	}

	return
}

func (c *CouchbaseCollectionGroupCache) Update(resource *couchbasev2.CouchbaseCollectionGroup) error {
	return c.resourceCache.informer.GetStore().Update(resource)
}

// stop stops the cache synchronization and frees resources.
func (c *CouchbaseCollectionGroupCache) stop() {
	c.resourceCache.stop()
}

// CouchbaseMigrationReplicationCache is a wrapper around a resourceCache that provides concrete typing for
// CouchbaseMigrationReplication resources.
type CouchbaseMigrationReplicationCache struct {
	resourceCache *resourceCache
	namespace     string
}

// newCouchbaseCollectionGroupCache creates a new synchronized cache.
func newCouchbaseMigrationReplicationCache(ctx context.Context, client couchbaseclientv2.Interface, namespace string) (*CouchbaseMigrationReplicationCache, error) {
	selector := labels.Everything()

	resourceCache, err := newResourceCache(ctx, client.CouchbaseV2().RESTClient(), &couchbasev2.CouchbaseMigrationReplication{}, selector, couchbasev2.MigrationReplicationCRDResourcePlural, namespace)
	if err != nil {
		return nil, err
	}

	return &CouchbaseMigrationReplicationCache{
		resourceCache: resourceCache,
		namespace:     namespace,
	}, nil
}

// get returns the requested resource based on name.
func (c *CouchbaseMigrationReplicationCache) Get(name string) (*couchbasev2.CouchbaseMigrationReplication, bool) {
	key := c.namespace + "/" + name

	// Cannot error
	obj, exists, _ := c.resourceCache.informer.GetStore().GetByKey(key)
	if !exists {
		return nil, exists
	}

	return obj.(*couchbasev2.CouchbaseMigrationReplication), true
}

// list returns all resources.
func (c *CouchbaseMigrationReplicationCache) List() (resources []*couchbasev2.CouchbaseMigrationReplication) {
	objs := c.resourceCache.informer.GetStore().List()
	for _, obj := range objs {
		resources = append(resources, obj.(*couchbasev2.CouchbaseMigrationReplication))
	}

	return
}

// stop stops the cache synchronization and frees resources.
func (c *CouchbaseMigrationReplicationCache) stop() {
	c.resourceCache.stop()
}
