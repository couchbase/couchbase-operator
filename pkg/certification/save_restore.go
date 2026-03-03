/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package certification

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"sort"
	"strings"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/conversion"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/util"
	"github.com/couchbase/couchbase-operator/pkg/util/ansi"
	"github.com/couchbase/couchbase-operator/pkg/util/cli"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/netutil"
	"github.com/couchbase/couchbase-operator/pkg/util/portforward"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/ghodss/yaml"
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

var (
	// errParameterError is raised when the user messed up (most common).
	errParameterError = errors.New("parameter error")

	// errEnvironmentError is raised when things aren't as they should be (usually users too).
	errEnvironmentError = errors.New("environment error")

	// errRuntimeError is raised when something outside of our control went wrong.
	errRuntimeError = errors.New("runtime error")

	// errInternalError is raised when we messed up, and shouldn't ever be seen.
	errInternalError = errors.New("internal error")

	// minVersion is the lowest version this will work with.
	minVersion = "7.0.0"
)

var (
	// log is the interface all logging should use, so we can control the verbosity
	// of logs for supportability and UX.
	log = cli.NewLogger()
)

// intMin returns the smaller of two signed integers.
func intMin(a, b int) int {
	if a < b {
		return a
	}

	return b
}

// nonEmptyStrings is a slice that should have no empty elements.
type nonEmptyStrings []string

// Len returns the length of the slice.
func (s nonEmptyStrings) Len() int {
	return len(s)
}

// Prediate returns whether the elemenet is non empty.
func (s nonEmptyStrings) Predicate(i int) bool {
	return s[i] != ""
}

// These are array indices and must be integers in the range 0-MAX.
const (
	// hierarchyFullTree means a save is the full tree e.g. everything.
	hierarchyFullTree = iota

	// hierarchyBucket means a save is just the contents of a bucket.
	hierarchyBucket

	// hierarchyScope means a save is just the contents of a scope.
	hierarchyScope

	// hierarchyMax is the largest number of entries possible e.g. 3.
	hierarchyMax
)

// pathVar defines a path within the data topology e.g. /bucket/scope/collection.
type pathVar struct {
	// path is the full path supplied.
	path string

	// elements is the path split on the separator.
	elements []string

	// bucket is the bucket component from the path.  Zero value if not set.
	bucket string

	// scope is the scope component from the path.  Zero value if not set.
	scope string

	// level tells us how much of a save we are doing/expecting.
	level int
}

// Set processes the CLI input and unmarshals into our interal representation.
func (v *pathVar) Set(s string) error {
	// Note: we have /bucket and /bucket/scope as valid parameters, semanically
	// this returns "all scopes in the bucket" and "all collections in the scope".
	// In theory /bucket/ and /bucket/scope/ are better suited to to this as it
	// follows URL path semantics where the path is a directory and you want
	// all things in that directory.  We could then use the non-/ suffixed version
	// for a filter that returns the named resource and all it's subordinates.
	if len(s) == 0 {
		return fmt.Errorf("%w: path must be non-zero length", errParameterError)
	}

	if !strings.HasPrefix(s, "/") {
		return fmt.Errorf("%w: path must start with a /", errParameterError)
	}

	if len(s) > 1 && strings.HasSuffix(s, "/") {
		return fmt.Errorf("%w: path must not end with a /", errParameterError)
	}

	// The root path will have no parts to its path...
	var parts []string

	if s != "/" {
		// We can have 2 components max, /bucket/scope becomes ["bucket", "scope"]
		// after the split.
		parts := strings.Split(s, "/")[1:]
		if len(parts) > 2 {
			return fmt.Errorf("%w: path must only have bucket and scope components", errParameterError)
		}

		// Check that the elements that matter are non empty.
		if ok := util.All(nonEmptyStrings(parts)); !ok {
			return fmt.Errorf("%w: path elements must be non empty", errParameterError)
		}
	}

	v.path = s
	v.elements = parts
	v.level = len(v.elements)

	if v.level > 0 {
		v.bucket = parts[0]
	}

	if v.level > 1 {
		v.scope = parts[1]
	}

	return nil
}

// String returns the variable default.
func (v *pathVar) String() string {
	return v.path
}

// Type returns the type of the expected value.
func (v *pathVar) Type() string {
	return "string"
}

// newPathVar is a path varible constructor that sets a sane default
// i.e. / - all buckets, scopes and collections.
func newPathVar() pathVar {
	return pathVar{
		path: "/",
	}
}

var (
	// defaultPath is used as default for operations that work implictly
	// on full trees.
	defaultPath = newPathVar()
)

// clients is a container for various Kubernetes interfaces.
type clients struct {
	// namespace is the namespace as defined by kubeconfig/the CLI flags.
	namespace string

	// config is the Kubernetes config.
	config *rest.Config

	// client is the Kubernetes client set.
	client kubernetes.Interface

	// couchbaseClient is the couchbase client set.
	couchbaseClient versioned.Interface

	// dynamicClient is a type agnostic client set.
	dynamicClient dynamic.Interface

	// restMapper allows us to go from a type to a REST API call.
	restMapper meta.RESTMapper
}

// newClients registers custom types with the client scheme and then
// returns a set of clients for use anywhere.
func newClients(flags *genericclioptions.ConfigFlags) (*clients, error) {
	// Register custom/non-standard types.
	if err := couchbasev2.AddToScheme(scheme.Scheme); err != nil {
		return nil, err
	}

	// Load the config and any uesful data.
	loader := flags.ToRawKubeConfigLoader()
	namespace, _, _ := loader.Namespace()

	config, err := loader.ClientConfig()
	if err != nil {
		return nil, err
	}

	// Initialize the clients.
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	couchbaseClient, err := versioned.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	// Initialize the REST mapper.
	groupResources, err := restmapper.GetAPIGroupResources(client.Discovery())
	if err != nil {
		return nil, err
	}

	restMapper := restmapper.NewDiscoveryRESTMapper(groupResources)

	clients := &clients{
		namespace:       namespace,
		config:          config,
		client:          client,
		couchbaseClient: couchbaseClient,
		dynamicClient:   dynamicClient,
		restMapper:      restMapper,
	}

	return clients, nil
}

// saveOptions allow a save of a cluster's data topology to be customized.
type saveOptions struct {
	// cluster is the cluster name to interrogate, this is the Kubernetes
	// resource name that relates to a CouchbaseCluster resource.  If none is
	// specified we will use whatever we find, but there can only be one due
	// to ambiguity.
	cluster string

	// path defines where to take the save from in the data hierarchy.  It defaults
	// to '/' for everything -- all buckets and descendants, but can be /bucket --
	// all scopes in a bucket, or /bucket/scope -- for all collections in a scope.
	path pathVar

	// filePath is where to write the save data to, and may be an absolute or relative
	// path.  This parameter is required.
	filePath string

	// debug is a secret hidden flag that shows a lot more detail.
	debug bool
}

// getSaveDataTopologyCommand returns a cobra command that encapsulates the save functionality.
func getSaveDataTopologyCommand(flags *genericclioptions.ConfigFlags) *cobra.Command {
	o := &saveOptions{
		path: newPathVar(),
	}

	cmd := &cobra.Command{
		Use:   "save",
		Short: "Save a cluster's data topology",
		Long: normalize(`
			Save a cluster's data topology

			In a development environment it may be desirable to manually manage
			the data topology in a rapid and agile fashion, rather than use the
			native Kubernetes resource types we provide.  For example you may
			wish to create buckets, scopes and collections using the UI, or an
			SDK, without having the overhead of change control, review and
			auditing of changes that using native resources would provide.

			This command allows a specific cluster to be probed and all data
			topology resources saved, direct from the Couchbase cluster.  Saved
			data topology represents data as Kubernetes native resource types and
			can later be used to restore data topology, allow it to be managed by
			the Operator, or even replicated to a completely new cluster.

                        Save and restore of resources will modify Kubernetes resources, so
                        therefore should never be used with any other form of lifecycle
                        management tool (e.g. Helm or Red Hat OLM) as these may revert changes
                        and lead to catastrophic data loss.
		`),
		Example: normalize(`
			# Save the full data topology on the only cluster in a namespace
			cao save --filename save.yaml

			# Save the full data topology for a specific cluster
			cao save --couchbase-cluster cluster-name --filename save.yaml

			# Save all scope and collections in a bucket
			cao save --path /bucket --filename save.yaml

			# Save all collections in a scope
			cao save --path /bucket/scope --filename save.yaml
		`),
		Run: func(cmd *cobra.Command, args []string) {
			if err := o.validate(args); err != nil {
				log.Info(err)
				os.Exit(1)
			}

			o.prepare()

			if err := o.save(flags); err != nil {
				log.Info(err)
				os.Exit(1)
			}
		},
	}

	cmd.Flags().StringVar(&o.cluster, "couchbase-cluster", "", "Cluster to save from (CouchbaseCluster resource name)")
	cmd.Flags().Var(&o.path, "path", "Path to save data from.  Default will save all buckets, scopes and collections.  '/bucket' will save all scopes and collection in Couchbase bucket 'bucket'.  '/bucket/scope' will save all collections in Couchbase bucket 'bucket' and Couchbase scope 'scope'.")
	cmd.Flags().StringVarP(&o.filePath, "filename", "f", "", "Filename to write the save data to.  This flag is required.")

	cmd.Flags().BoolVar(&o.debug, "debug", false, "Enable debug output")
	_ = cmd.Flags().MarkHidden("debug")

	return cmd
}

// validate checks any complex requirements for save command flags.
func (o *saveOptions) validate(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("%w: unexpected arguments", errParameterError)
	}

	if o.filePath == "" {
		return fmt.Errorf("%w: --filename flag is required", errParameterError)
	}

	return nil
}

// prepare does any necessary setup work.
func (o *saveOptions) prepare() {
	if o.debug {
		log.Verbosity = cli.Debug
	}
}

// save is the top level save function.
func (o *saveOptions) save(flags *genericclioptions.ConfigFlags) error {
	// Get some clients as we will be interacting with Kubernetes.
	clients, err := newClients(flags)
	if err != nil {
		return err
	}

	// Work out the cluster to connect to.
	cluster, err := getCluster(clients, o.cluster)
	if err != nil {
		return err
	}

	// Gather all the resources associated with that cluster.
	resources, err := gatherClusterResources(clients, cluster, o.path)
	if err != nil {
		return err
	}

	if len(resources) == 0 {
		return fmt.Errorf("%w: no resources detected", errEnvironmentError)
	}

	// Dump out the resources now they have been collated and linked.
	out, err := os.OpenFile(o.filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0640)
	if err != nil {
		return err
	}

	defer func() {
		_ = out.Close()
	}()

	for _, resource := range resources {
		j, err := yaml.Marshal(resource)
		if err != nil {
			return err
		}

		fmt.Fprintln(out, "---")
		fmt.Fprintln(out, strings.Trim(string(j), "\n"))
	}

	return nil
}

// getCluster returns the cluster as defined by getClusterName.
func getCluster(clients *clients, cliName string) (*couchbasev2.CouchbaseCluster, error) {
	// Work out the cluster to connect to.
	clusterName, err := getClusterName(clients, cliName)
	if err != nil {
		return nil, err
	}

	cluster, err := clients.couchbaseClient.CouchbaseV2().CouchbaseClusters(clients.namespace).Get(context.Background(), clusterName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return cluster, nil
}

// getClusterName either returns the explcitly stated cluster name, or does a lookup of
// all clusters in the namespace.  It's an ambiguous error to have more than one cluster.
func getClusterName(clients *clients, cliName string) (string, error) {
	if cliName != "" {
		return cliName, nil
	}

	clusters, err := clients.couchbaseClient.CouchbaseV2().CouchbaseClusters(clients.namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return "", err
	}

	if len(clusters.Items) != 1 {
		return "", fmt.Errorf("%w: ambiguous request, expected 1 cluster, found %d", errParameterError, len(clusters.Items))
	}

	return clusters.Items[0].Name, nil
}

// getCredentials interrogates the provided couchbase cluster, resolves the admin credentials
// secret, and returns the username and password.
func getCredentials(clients *clients, cluster *couchbasev2.CouchbaseCluster) (string, string, error) {
	secret, err := clients.client.CoreV1().Secrets(clients.namespace).Get(context.Background(), cluster.Spec.Security.AdminSecret, metav1.GetOptions{})
	if err != nil {
		return "", "", err
	}

	username, ok := secret.Data[couchbasev2.AdminSecretUsernameKey]
	if !ok {
		return "", "", fmt.Errorf("%w: cluster admin secret doesn't contain a username", errEnvironmentError)
	}

	password, ok := secret.Data[couchbasev2.AdminSecretPasswordKey]
	if !ok {
		return "", "", fmt.Errorf("%w: cluster admin secret doesn't contain a username", errEnvironmentError)
	}

	return string(username), string(password), nil
}

// selectClusterPod lists all pods that are port of the cluster and selects one at
// random to open hailing frequencies to.
func selectClusterPod(clients *clients, cluster *couchbasev2.CouchbaseCluster) (*corev1.Pod, error) {
	selector := k8sutil.SelectorForClusterResource(cluster)

	pods, err := clients.client.CoreV1().Pods(clients.namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labels.FormatLabels(selector)})
	if err != nil {
		return nil, err
	}

	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("%w: no pods found for cluster", errEnvironmentError)
	}

	return &pods.Items[0], nil
}

// checkVersion stops you from using an incompatible Server version.
func checkVersion(client *couchbaseutil.Client, host string) error {
	var pools couchbaseutil.PoolsInfo

	if err := couchbaseutil.GetPools(&pools).On(client, host); err != nil {
		return err
	}

	version, err := couchbaseutil.NewVersion(pools.Version)
	if err != nil {
		return err
	}

	if !version.GreaterEqualString(minVersion) {
		return fmt.Errorf("%w: unsupported version, minimum %s", errEnvironmentError, minVersion)
	}

	return nil
}

// gatherResources collects all buckets, scopes and collections, under the guidance
// of a path filter.
func gatherResources(client *couchbaseutil.Client, host string, path pathVar) ([]runtime.Object, error) {
	// Gather all buckets.
	buckets := couchbaseutil.BucketList{}
	if err := couchbaseutil.ListBuckets(&buckets).On(client, host); err != nil {
		return nil, err
	}

	// Filter buckets based on path.
	if err := filterBuckets(&buckets, path); err != nil {
		return nil, err
	}

	// So, now for the fun bit... iterate through all buckets, their scopes and their
	// collections.  As we go along add them to the resource list (ordering itsn't
	// important).  The parent object reference is still in scope so we can build
	// up the descendants as we travel through the data topology.
	var resources []runtime.Object

	for _, bucket := range buckets {
		bucketResource, err := conversion.ConvertAbstractBucketToAPIBucket(&bucket, defaultSaveRestoreNamer)
		if err != nil {
			return nil, err
		}

		// Only accumulate the bucket if it's unfiltered, we're asking for
		// only scopes or collections otherwise.
		if path.bucket == "" {
			resources = append(resources, bucketResource)
		}

		scopes := couchbaseutil.ScopeList{}
		if err := couchbaseutil.ListScopes(bucket.BucketName, &scopes).On(client, host); err != nil {
			return nil, err
		}

		// Filter scopes passed on path.
		if err := filterScopes(&scopes, path); err != nil {
			return nil, err
		}

		for _, scope := range scopes.Scopes {
			s := conversion.ConvertCouchbaseScopeToAPIScope(&bucket, &scope, defaultSaveRestoreNamer)

			abstractBucket, ok := bucketResource.(couchbasev2.AbstractBucket)
			if !ok {
				return nil, fmt.Errorf("%w: cannot convert resource type", errInternalError)
			}

			scopeReference := couchbasev2.ScopeLocalObjectReference{
				Kind: couchbasev2.CouchbaseScopeKindScope,
				Name: couchbasev2.ScopeOrCollectionName(s.Name),
			}

			abstractBucket.AddScopeResource(scopeReference)

			// Only accumulate the scope if it's unflitered, we are only asking
			// for collections otherwise.
			if path.scope == "" {
				resources = append(resources, s)
			}

			for _, collection := range scope.Collections {
				if collection.Name == constants.DefaultScopeOrCollectionName {
					s.Spec.Collections.PreserveDefaultCollection = true
				} else {
					c := conversion.ConvertCouchbaseCollectionToAPICollection(&bucket, &scope, &collection, defaultSaveRestoreNamer)

					collectionReference := couchbasev2.CollectionLocalObjectReference{
						Kind: couchbasev2.CouchbaseCollectionKindCollection,
						Name: couchbasev2.ScopeOrCollectionName(c.Name),
					}

					s.Spec.Collections.Resources = append(s.Spec.Collections.Resources, collectionReference)

					resources = append(resources, c)
				}
			}
		}
	}

	return resources, nil
}

// filterBuckets updates the bucket list to contain only the selected bucket,
// if set.  Returns an error if the bucket doesn't exist.
func filterBuckets(buckets *couchbaseutil.BucketList, path pathVar) error {
	if path.bucket == "" {
		return nil
	}

	bucket, err := buckets.Get(path.bucket)
	if err != nil {
		return err
	}

	*buckets = couchbaseutil.BucketList{*bucket}

	return nil
}

// filterScopes updates the scope list to contain only the selected scope,
// if set.  Returns an error if the scope doesn't exist.
func filterScopes(scopes *couchbaseutil.ScopeList, path pathVar) error {
	if path.scope == "" {
		return nil
	}

	if !scopes.HasScope(path.scope) {
		return fmt.Errorf("%w: requested scope in path %s does not exist", errEnvironmentError, path.path)
	}

	scopes.Scopes = []couchbaseutil.Scope{scopes.GetScope(path.scope)}

	return nil
}

// gatherClusterResources collects all buckets, scopes and collections for the given cluster.
func gatherClusterResources(clients *clients, cluster *couchbasev2.CouchbaseCluster, path pathVar) ([]runtime.Object, error) {
	// Select a pod.
	pod, err := selectClusterPod(clients, cluster)
	if err != nil {
		return nil, err
	}

	// Setup a channel to talk to the server instance.
	port, err := netutil.GetFreePort()
	if err != nil {
		return nil, err
	}

	// Default to "plaintext" (we still run over a HTTP/2 transport),
	// but if using TLS, the inherent danger is you are using strict
	// mode TLS, therefore no plaintext ports are open.
	scheme := "http"
	targetPort := k8sutil.AdminServicePort

	if cluster.IsTLSEnabled() {
		log.V(1).Info("Cluster using TLS, defaulting to https")

		scheme = "https"
		targetPort = k8sutil.AdminServicePortTLS
	}

	host := fmt.Sprintf("%s://localhost:%v", scheme, port)

	pf := portforward.PortForwarder{
		Config:    clients.config,
		Client:    clients.client,
		Namespace: clients.namespace,
		Pod:       pod.Name,
		Port:      fmt.Sprintf("%v:%v", port, targetPort),
	}

	if err := pf.ForwardPorts(); err != nil {
		return nil, fmt.Errorf("%w: unable to forward admin port for pod %s", errRuntimeError, pod.Name)
	}

	defer pf.Close()

	// Silence!  I've made my decision.  Bring back my girls.
	restorer := portforward.Silent()
	defer restorer()

	// Grab the username and password.
	username, password, err := getCredentials(clients, cluster)
	if err != nil {
		return nil, err
	}

	// Create a client to talk to the pod.
	apiClient := couchbaseutil.New(context.Background(), "", username, password)

	// Here's where it gets *really* complicated, we need to gather all sources of
	// CAs in the configuration in order to verify who we are talking to.  Then as
	// an added annoyance, if mTLS is in use we need to extract the client certificate.
	if cluster.IsTLSEnabled() {
		rootCAs, err := getCACertificates(clients, cluster)
		if err != nil {
			return nil, err
		}

		tls := &couchbaseutil.TLSAuth{
			RootCAs: rootCAs,
		}

		if cluster.IsMutualTLSEnabled() {
			cert, key, err := getClientCertificate(clients, cluster)
			if err != nil {
				return nil, err
			}

			tls.ClientAuth = &couchbaseutil.TLSClientAuth{
				Cert: cert,
				Key:  key,
			}
		}

		apiClient.SetTLS(tls)
	}

	// Insert this here while we have the client tunnel open.
	if err := checkVersion(apiClient, host); err != nil {
		return nil, err
	}

	// Gather all resources.
	return gatherResources(apiClient, host, path)
}

// getCACertificates inspects the cluster and pulls out all CAs it can find,
// one should validate the server certificate...
func getCACertificates(clients *clients, cluster *couchbasev2.CouchbaseCluster) ([][]byte, error) {
	var rootCAs [][]byte

	// Legacy TLS, if specified must have a CA in the operator secret.
	// This is the only configuration allowed in this mode.
	if !cluster.IsTLSShadowed() {
		secret, err := clients.client.CoreV1().Secrets(clients.namespace).Get(context.Background(), cluster.Spec.Networking.TLS.Static.OperatorSecret, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}

		cert, ok := secret.Data[constants.OperatorSecretCAKey]
		if !ok {
			return nil, fmt.Errorf("%w: TLS operator secret has no CA key", errEnvironmentError)
		}

		rootCAs = append(rootCAs, cert)

		return rootCAs, nil
	}

	// Modern TLS may have a CA if generated by cert-manager.
	if cluster.Spec.Networking.TLS.SecretSource != nil {
		secret, err := clients.client.CoreV1().Secrets(clients.namespace).Get(context.Background(), cluster.Spec.Networking.TLS.SecretSource.ServerSecretName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}

		cert, ok := secret.Data[constants.CertManagerCAKey]
		if ok {
			rootCAs = append(rootCAs, cert)
		}
	}

	// CAs may be optionally specified in the root CAs list.
	for _, rootCA := range cluster.Spec.Networking.TLS.RootCAs {
		secret, err := clients.client.CoreV1().Secrets(clients.namespace).Get(context.Background(), rootCA, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}

		cert, ok := secret.Data[corev1.TLSCertKey]
		if !ok {
			return nil, fmt.Errorf("%w: TLS CA secret %s has no certificate key", errEnvironmentError, rootCA)
		}

		rootCAs = append(rootCAs, cert)
	}

	return rootCAs, nil
}

// getClientCertificate returns the Operator's client certificate and key.
func getClientCertificate(clients *clients, cluster *couchbasev2.CouchbaseCluster) ([]byte, []byte, error) {
	if !cluster.IsTLSShadowed() {
		secret, err := clients.client.CoreV1().Secrets(clients.namespace).Get(context.Background(), cluster.Spec.Networking.TLS.Static.OperatorSecret, metav1.GetOptions{})
		if err != nil {
			return nil, nil, err
		}

		cert, ok := secret.Data["couchbase-operator.crt"]
		if !ok {
			return nil, nil, fmt.Errorf("%w: TLS operator secret has no client cert", errEnvironmentError)
		}

		key, ok := secret.Data["couchbase-operator.key"]
		if !ok {
			return nil, nil, fmt.Errorf("%w: TLS operator secret has no client key", errEnvironmentError)
		}

		return cert, key, nil
	}

	secret, err := clients.client.CoreV1().Secrets(clients.namespace).Get(context.Background(), cluster.Spec.Networking.TLS.SecretSource.ClientSecretName, metav1.GetOptions{})
	if err != nil {
		return nil, nil, err
	}

	cert, ok := secret.Data[corev1.TLSCertKey]
	if !ok {
		return nil, nil, fmt.Errorf("%w: TLS client secret has no cert", errEnvironmentError)
	}

	key, ok := secret.Data[corev1.TLSPrivateKeyKey]
	if !ok {
		return nil, nil, fmt.Errorf("%w: TLS client secret has no key", errEnvironmentError)
	}

	return cert, key, nil
}

// saveRestoreResourceNamer handles name generation of resource names in the context
// of save and restore.  It conforms to the conversion.BucketNameGenerator interface.
type saveRestoreResourceNamer struct{}

// defaultSaveRestoreNamer is a global naming abstraction.
var defaultSaveRestoreNamer = &saveRestoreResourceNamer{}

// generateName generates a unique name for a type.
func (n *saveRestoreResourceNamer) generateName(t string) string {
	return fmt.Sprintf("%s-%s", t, uuid.New().String())
}

// GenerateBucketName generates a unique bucket name.
func (n *saveRestoreResourceNamer) GenerateBucketName(_ *couchbaseutil.Bucket) string {
	return n.generateName("bucket")
}

// GenerateEphemeralBucketName generates a unique ephemeral bucket name.
func (n *saveRestoreResourceNamer) GenerateEphemeralBucketName(_ *couchbaseutil.Bucket) string {
	return n.generateName("ephemeralbucket")
}

// GenerateMemcachedBucketName generates a unique memcached bucket name.
func (n *saveRestoreResourceNamer) GenerateMemcachedBucketName(_ *couchbaseutil.Bucket) string {
	return n.generateName("memcachedbucket")
}

// GenerateScopeName generates a unique scope name.
func (n *saveRestoreResourceNamer) GenerateScopeName(_ *couchbaseutil.Bucket, _ *couchbaseutil.Scope) string {
	return n.generateName("scope")
}

// generateScopeGroupName generates a unique scope group name.
func (n *saveRestoreResourceNamer) generateScopeGroupName() string {
	return n.generateName("scopegroup")
}

// GenerateCollectionName generates a unique collection name.
func (n *saveRestoreResourceNamer) GenerateCollectionName(_ *couchbaseutil.Bucket, _ *couchbaseutil.Scope, _ *couchbaseutil.Collection) string {
	return n.generateName("collection")
}

// generateCollectionGroupName generates a unique collection group name.
func (n *saveRestoreResourceNamer) generateCollectionGroupName() string {
	return n.generateName("collectiongroup")
}

// restoreStrategy defines how to handle conflicts between the current data topology
// and the requested data topology.
type restoreStrategy string

const (
	// restoreStrategyReplace tells the restore algorthm to fully replace
	// the root of the restore with what's provided in the save data.  This
	// mode is akin to an enforcement mode, deleting things that exist but
	// aren't requested, but risks data loss.
	restoreStrategyReplace restoreStrategy = "replace"

	// restoreStrategyMerge tells the restore algorithm to merge the save from
	// the root of the save data.  This mode is safer in that it leaves items
	// that exist in the current cluster, but aren't in the save, thus doesn't
	// risk data loss, but may fall foul of data retention legislation.
	restoreStrategyMerge restoreStrategy = "merge"
)

// newRestoreStrategy creates a new restore strategy flag with a safe default.
func newRestoreStrategy() restoreStrategy {
	return restoreStrategyMerge
}

// Set parses and validates a restore strategy string from the CLI.
func (v *restoreStrategy) Set(s string) error {
	switch restoreStrategy(s) {
	case restoreStrategyReplace, restoreStrategyMerge:
		*v = restoreStrategy(s)
		return nil
	}

	return fmt.Errorf("%w: invalid restore strategy: %v", errParameterError, s)
}

// Type returns the type of data to be supplied by the user.
func (v *restoreStrategy) Type() string {
	return "string"
}

// String returns the default/surrent restore strategy as a string.
func (v *restoreStrategy) String() string {
	return string(*v)
}

// mergeAction converts the restore strategy into a merge action for existing
// resources that weren't requested in the save data.
func (v *restoreStrategy) mergeAction() mergeAction {
	if *v == restoreStrategyReplace {
		return mergeActionDelete
	}

	return mergeActionRetain
}

// restoreOptions allow a restore of a cluster's data topology to be customized.
type restoreOptions struct {
	// cluster is the cluster name to interrogate, this is the Kubernetes
	// resource name that relates to a CouchbaseCluster resource.  If none is
	// specified we will use whatever we find, but there can only be one due
	// to ambiguity.
	cluster string

	// path defines where to restore the save to in the data hierarchy.  It defaults
	// to '/' for everything -- all buckets and descendants, but can be /bucket --
	// all scopes in a bucket, or /bucket/scope -- for all collections in a scope.
	path pathVar

	// strategy defines how a save is merged with the existing data topology tree.
	strategy restoreStrategy

	// filePath is where to read the save data from, and may be an absolute or relative
	// path.  This parameter is required.
	filePath string

	// yes is basically super dangerous and will just blindly overwrite everything.
	// This is hidden from end users, but of greate use to test.
	yes bool

	// debug is a secret hidden flag that shows a lot more detail.
	debug bool
}

// getRestoreDataTopologyCommand generates a cobra command capable of restoring and
// managing data topology resources to a cluster.
func getRestoreDataTopologyCommand(flags *genericclioptions.ConfigFlags) *cobra.Command {
	o := &restoreOptions{
		path:     newPathVar(),
		strategy: newRestoreStrategy(),
	}

	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore a cluster's data topology",
		Long: normalize(`
			Restore a cluster's data topology

			In a development environment it may be desirable to manually manage
			the data topology in a rapid and agile fashion, rather than use the
			native Kubernetes resource types we provide.  For example you may
			wish to create buckets, scopes and collections using the UI, or an
			SDK, without having the overhead of change control, review and
			auditing of changes that using native resources would provide.

			This command allows existing save data (as generated by 'cao save')
			to be applied to the selected cluster.  Restoration of data topology
			occurs as follows: the Couchbase cluster is interrogated for all data
			topology (including unmanaged buckets, scopes and collections). This
			is then compared with the contents of the save data to detect resources
			that will be added, updated or deleted as a result of this restore
			operation.  The user will be prompted for confimation that the outcome
			is as desired, giving you an opportunity to back out of unintentionally
			destructive operations.

			A new, full tree of resources (buckets, scopes and collections) is
			created then atomically swapped with the old tree, providing roll back
			in the event of an error.  Finally any old Kubernetes resources are
			automatically cleaned up.

			The atomic swap of resources is performed using label selectors,
			allowing restores when multiple Couchbase clusters are running in the
			same namespace.  As a precaution, the tool will only function if your
			cluster's buckets are unmanaged, there is no label selector set and
			there are no existing resources, or a label selector is already in use.
			It is your reponsibility to ensure that when multiple Couchbase clusters
			are running in the same namespace, they will not be affected by a
			restore operation e.g. they are not sharing any resources that may be
			modified or deleted.  It is usually safest to run a single Couchbase
			cluster per-namespace.

			All resources discovered when polling the Couchbase cluster will be
			backed by a Kubernetes resource, and managed by the Operator after
			a restore.  You may manually disable management of a particular bucket
			or scope if you so wish.

			Save and restore of resources will modify Kubernetes resources, so
			therefore should never be used with any other form of lifecycle
			management tool (e.g. Helm or Red Hat OLM) as these may revert changes
			and lead to catastrophic data loss.
		`),
		Example: normalize(`
			# Restore the full data topology on the only cluster in a namespace
			cao restore -f save-data.yaml

			# Restore the full data topology to the specific cluster
			cao restore --couchbase-cluster squirrel -f save-data.yaml

			# Restore all scope and collections in a bucket
			cao restore --path /bucket -f save-data.yaml

			# Restore all collections in a scope
			cao restore --path /bucket/scope -f save-data.yaml
		`),
		Run: func(cmd *cobra.Command, args []string) {
			if err := o.validate(args); err != nil {
				log.Info(err)
				os.Exit(1)
			}

			o.prepare()

			if err := o.restore(flags); err != nil {
				log.Info(err)
				os.Exit(1)
			}
		},
	}

	cmd.Flags().StringVar(&o.cluster, "couchbase-cluster", "", "Cluster to save from (CouchbaseCluster resource name)")
	cmd.Flags().Var(&o.path, "path", "Path restore data to.  Default will restore all buckets, scopes and collections.  '/bucket' will restore all scopes and collection in Couchbase bucket 'bucket'.  '/bucket/scope' will restore all collections in Couchbase bucket 'bucket' and Couchbase scope 'scope'.")
	cmd.Flags().Var(&o.strategy, "strategy", "Strategy to use when merging the save data with the current cluster's data.  When 'merge', this will retain any existing items that are in the current cluster, but not in the save.  When 'replace', this will fully replace the existing items that exist in the current cluster, but don't exist in the save.  Merging protects the user from accidental data loss, whereas replacement may cause data loss, but ensures old data is purged to enforce data retention policies.  This flag defaults to 'merge'.")
	cmd.Flags().StringVarP(&o.filePath, "filename", "f", "", "Filename to read the save data from.")

	// Ninja!  For us developers only.
	cmd.Flags().BoolVarP(&o.yes, "yes", "y", false, "Answer 'yes' to all interactive questions")
	_ = cmd.Flags().MarkHidden("yes")

	cmd.Flags().BoolVar(&o.debug, "debug", false, "Enable debug output")
	_ = cmd.Flags().MarkHidden("debug")

	return cmd
}

// validate checks any complex requirements for restore command flags.
func (o *restoreOptions) validate(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("%w: unexpected arguments", errParameterError)
	}

	if o.filePath == "" {
		return fmt.Errorf("%w: --filename flag is required", errParameterError)
	}

	return nil
}

// prepare does any necessary setup work.
func (o *restoreOptions) prepare() {
	if o.debug {
		log.Verbosity = cli.Debug
	}
}

// restore is the top level restoration function.
func (o *restoreOptions) restore(flags *genericclioptions.ConfigFlags) error {
	// Get some clients as we will be interacting with Kubernetes.
	clients, err := newClients(flags)
	if err != nil {
		return err
	}

	// Work out the cluster to connect to.
	cluster, err := getCluster(clients, o.cluster)
	if err != nil {
		return err
	}

	// Grab any existing resources associated with the cluster, we're going
	// to kill them off for the user.
	log.V(cli.Debug).Info("Collecting data topology resources...")

	resources, err := gatherDataTopologyResources(clients, cluster)
	if err != nil {
		return err
	}

	// Safety check, ensure the user isn't doing anything likely to endanger
	// other clusters.
	if cluster.Spec.Buckets.Managed && len(resources) != 0 && cluster.Spec.Buckets.Selector == nil {
		return fmt.Errorf("%w: cluster must use bucket label selectors", errEnvironmentError)
	}

	// Gather all the resources associated with that cluster and create a topology tree.
	log.V(cli.Debug).Info("Loading current cluster topology...")

	currentResources, err := gatherClusterResources(clients, cluster, defaultPath)
	if err != nil {
		return err
	}

	current, err := build(currentResources, defaultPath, false)
	if err != nil {
		return err
	}

	if log.V(cli.Debug).Enabled() {
		Dump(current)
	}

	// Load up the save data and create a topology tree.
	log.V(cli.Debug).Info("Loading requested cluster topology...")

	requestedResources, err := o.loadRestoreObjects()
	if err != nil {
		return err
	}

	requested, err := build(requestedResources, o.path, true)
	if err != nil {
		return err
	}

	if log.V(cli.Debug).Enabled() {
		Dump(requested)
	}

	// Next up, splice the requested tree into the current tree at the chosen
	// path if required.
	log.V(cli.Debug).Info("Splicing requested cluster topology...")

	spliced, err := splice(current, requested, o.path)
	if err != nil {
		return err
	}

	if log.V(cli.Debug).Enabled() {
		Dump(spliced)
	}

	// Finally for phase 1, solve the problem e.g. what's going to happen?
	log.V(cli.Debug).Info("Merging cluster topology...")

	merged, err := o.merge(current, spliced)
	if err != nil {
		return err
	}

	// Report back to the user what's going to happen, and get them to agree,
	// especially when you're about to delete all your data.
	log.Info(ansi.Color(ansi.White) + "Data topology solution:" + ansi.Reset())
	log.Info()

	Dump(merged)
	log.Info()

	log.Info(ansi.Color(ansi.Yellow) + "WARNING!" + ansi.Reset() + " resources marked as " + ansi.Color(ansi.Red) + "delete" + ansi.Reset() + " may result in data loss.")
	log.Info()

	if err := o.promptOkay(); err != nil {
		return err
	}

	// Compact the merged tree to yield the resources we need to create.
	log.V(cli.Debug).Info("Compacting cluster topology...")

	compacted, err := o.compact(merged)
	if err != nil {
		return err
	}

	if log.V(cli.Debug).Enabled() {
		Dump(compacted)
	}

	// Link the final tree together into a set of resources, and also grab
	// a label selector for atomically switching trees...
	log.V(cli.Debug).Info("Linking cluster topology...")

	l, linked, err := o.link(cluster.Name, compacted)
	if err != nil {
		return err
	}

	// Go live!  As we are using a pivot, and that's the last operation, then
	// we can safely do a rollback but just deleting the new stuff.
	log.V(cli.Debug).Info("Creating new resources and pivoting root...")

	if err := pivotRoot(clients, cluster, l, linked); err != nil {
		log.V(cli.Debug).Info("Rolling back new resources...")

		_ = cleanup(clients, linked)

		return err
	}

	log.V(cli.Debug).Info("Cleaning up old resources...")

	if err := cleanup(clients, resources); err != nil {
		return err
	}

	return nil
}

// promptOkay creates a barrier until the user explcitly agrees to something.
func (o *restoreOptions) promptOkay() error {
	if o.yes {
		return nil
	}

	for {
		fmt.Print(ansi.Color(ansi.White) + "OK to proceed? (y/N) " + ansi.Reset())

		reader := bufio.NewReader(os.Stdin)

		raw, err := reader.ReadString('\n')
		if err != nil {
			return err
		}

		ok, err := cli.ParseYesNo(raw, false)
		if err != nil {
			continue
		}

		if !ok {
			os.Exit(0)
		}

		break
	}

	return nil
}

// diffResources is the only real way to compare Kubernetes resources, as doing it
// in native go would yield false positives e.g. a nil slice and an empty slice are
// different, likewise complex types may be unmarshalled differently to how we create
// them.  Thus it is always safest to compare resources in a canonical form, e.g. JSON.
// Returns whether the resources are equal, and a JSON merge patch.  More details on
// JSON merge patches can be found in the following, suffice to say it works like Helm
// i.e. null to unset:
// https://datatracker.ietf.org/doc/html/draft-ietf-appsawg-json-merge-patch-07
func diffResources(a, b interface{}) (bool, []byte, error) {
	ja, err := json.Marshal(a)
	if err != nil {
		return false, nil, err
	}

	jb, err := json.Marshal(b)
	if err != nil {
		return false, nil, err
	}

	patch, err := jsonpatch.CreateMergePatch(ja, jb)
	if err != nil {
		return false, nil, err
	}

	// An empty merge patch means nothing needs doing.
	if bytes.Equal([]byte("{}"), patch) {
		return true, nil, nil
	}

	return false, patch, nil
}

// TreeNode forms an abstraction layer for all resource types.
type TreeNode interface {
	// Name is the Couchbase resource name.
	Name() string

	// Describe returns a verbose description of the resource.
	Describe() string

	// AddChild adds a child to this node.
	AddChild(TreeNode)

	// AddChildren adds a list of children to the node.
	AddChildren(TreeNodeList)

	// GetChildren returns a list of all children.
	GetChildren() TreeNodeList

	// GetChild returns the child that matches the Couchbase name.
	GetChild(string) (TreeNode, error)

	// ClearChildren resets the child list for the current node.
	ClearChildren()

	// DeepCopy does exactly that, clones everything in a tree.
	DeepCopy() TreeNode

	// SemanticEqual checks for semantic equality of resources i.e. do
	// specifications match up.  If false, then the byte array is a
	// JSON merge patch that can be used to communicate what has changed.
	SemanticEqual(TreeNode) (bool, []byte, error)

	// DeepEqual checks for actual equality of resources i.e. do the
	// resources have the same specification, name and descendants.
	DeepEqual(TreeNode) (bool, error)

	// GetMergeAction retrieves the merge action from a node.
	GetMergeAction() mergeAction

	// SetMergeAction recursively sets the merge action for the
	// subtree.
	SetMergeAction(mergeAction)

	// SetMergePatch allows a patch to be associated with a node
	// to show the differences when the action is 'update'.
	SetMergePatch([]byte)

	// DeadCode removes any nodes marked with a delete merge action.
	DeadCode()

	// GenerateResourceName creates a unique resource name for
	// concrete resource types.
	GenerateResourceName()

	// ResourceName returns the nodes's resource Kubernetes name.
	ResourceName() string

	// Link looks at a node's abstract children, and creates type
	// specific linkage in the resource.  The labels are only relevant
	// for buckets, as they are label selected.
	Link(labels.Set) error

	// Resource extracts the resource from a node.
	Resource() runtime.Object
}

// TreeNodeList allows peers to be ordered, allowing for
// merge sorts etc.
type TreeNodeList []TreeNode

// Len returns the length of the list.
func (t TreeNodeList) Len() int {
	return len(t)
}

// Less returns whether an element at index i is less than that at element j.
func (t TreeNodeList) Less(i, j int) bool {
	return t[i].Name() < t[j].Name()
}

// Swap echanges elements at index i and j.
func (t TreeNodeList) Swap(i, j int) {
	t[i], t[j] = t[j], t[i]
}

// Reverse returns a new tree list with the element order reversed.
func (t TreeNodeList) Reverse() TreeNodeList {
	n := make(TreeNodeList, t.Len())
	copy(n, t)

	for i := n.Len()/2 - 1; i >= 0; i-- {
		j := n.Len() - 1 - i
		n.Swap(i, j)
	}

	return n
}

// DeepEqual checks for actual equality of lists of resources i.e.
// do they have the same elements, in the same order, and do the
// resources have the same specification, name and descendants.
func (t TreeNodeList) DeepEqual(o TreeNodeList) (bool, error) {
	if t.Len() != o.Len() {
		return false, nil
	}

	for i, child := range t {
		ok, err := child.DeepEqual(o[i])
		if err != nil {
			return false, err
		}

		if !ok {
			return false, nil
		}
	}

	return true, nil
}

// Mergeable checks whether this set of nodes can be merged.  The criteria
// for merging are that each child is semanitcally equivalent (and thus
// can share a specification), and that all of their descendants are
// strictly the same.
func (t TreeNodeList) Mergeable() (bool, error) {
	head := t[0]

	for _, node := range t[1:] {
		// All children need to be semantically equal i.e. are
		// the same with the exception of their name.
		ok, _, err := head.SemanticEqual(node)
		if err != nil {
			return false, err
		}

		if !ok {
			return false, nil
		}

		// They also need to have exactly the same descendants
		// taking the names into account.
		ok, err = head.GetChildren().DeepEqual(node.GetChildren())
		if err != nil {
			return false, err
		}

		if !ok {
			return false, nil
		}
	}

	return true, nil
}

// TreeNodeList removes all children with a delete merge action and returns
// the new list.
func (t TreeNodeList) DeadCode() TreeNodeList {
	var n TreeNodeList

	for _, child := range t {
		if child.GetMergeAction() == mergeActionDelete {
			continue
		}

		// Scopes are special, in that default can also be implicit...
		// This was originally written because the node merging algorithm
		// was a little bit naive, and handing defaults hanging around
		// caused optimisations to be not applied.
		if node, ok := child.(*scopeNode); ok {
			if node.resource.CanBeImplied() {
				continue
			}
		}

		n = append(n, child)
	}

	return n
}

// mergeAction is an action that needs to be taken after current and
// requested tress have been merged.
type mergeAction string

const (
	// mergeActionRetain means the resources match and nothing needs to be done.
	mergeActionRetain mergeAction = "retain"

	// mergeActionCreate means something was requested, doesn't exist, and needs creating.
	mergeActionCreate mergeAction = "create"

	// mergeActionDelete means something wasn't requested, exists, and needs deleting.
	mergeActionDelete mergeAction = "delete"

	// mergeActionUpdate means that resources are requested, exist, but have differences that
	// need updating.
	mergeActionUpdate mergeAction = "update"
)

// treeNode is an aggregate that is included in all concrete node types.
// This provides common functionality across all tree node types.
type treeNode struct {
	name string

	// children are the subordinates of this node.
	children TreeNodeList

	// action, if set, tells us what we need to do to make this node as intended.
	action mergeAction

	// patch, if set, shows what resources differences have caused an update
	// merge action.
	patch []byte
}

// newTreeNode creates a minimal tree node with all required fields.
func newTreeNode(name string) treeNode {
	return treeNode{
		name: name,
	}
}

// Name returns the node's unique name (within a certain scope).  This is
// the Couchbase resource name.
func (t *treeNode) Name() string {
	return t.name
}

// AddChild adds a child to the list of children, then sorts them by Couchbase
// resource name.
func (t *treeNode) AddChild(child TreeNode) {
	t.children = append(t.children, child)
	sort.Sort(t.children)
}

// GetChildren returns the sorted list of children this node points to.
func (t *treeNode) GetChildren() TreeNodeList {
	return t.children
}

// AddChildren does a bulk addition of children, maintaining lexical ordering.
func (t *treeNode) AddChildren(children TreeNodeList) {
	for _, child := range children {
		t.AddChild(child)
	}
}

// GetChild looks up and returns a child based on Couchbase resource name.
func (t *treeNode) GetChild(name string) (TreeNode, error) {
	for _, child := range t.children {
		if name == child.Name() {
			return child, nil
		}
	}

	return nil, fmt.Errorf("%w: unable to locate child %s", errParameterError, name)
}

// ClearChildren detaches all children from the current node.
func (t *treeNode) ClearChildren() {
	t.children = nil
}

// DeepCopy creates a new node and then copies and adds all linked
// children.
func (t *treeNode) DeepCopy() treeNode {
	tt := treeNode{
		name:   t.name,
		action: t.action,
	}

	for _, child := range t.children {
		tt.children = append(tt.children, child.DeepCopy())
	}

	return tt
}

// GetMergeAction retrieves the merge action from a node.
func (t *treeNode) GetMergeAction() mergeAction {
	return t.action
}

// SetMergeAction sets the node's action and recursively propagates
// to all the linked children.
func (t *treeNode) SetMergeAction(action mergeAction) {
	t.action = action

	for _, child := range t.children {
		child.SetMergeAction(action)
	}
}

// SetMergePatch associate a merge patch with a node.
func (t *treeNode) SetMergePatch(patch []byte) {
	t.patch = patch
}

// deepEqual is a helper function to test the equality of the resource
// name, and all descendants.  The top level DeepEqual function is
// defined per type, as they need to be able to see the resource
// specification.
func (t *treeNode) deepEqual(other TreeNode) (bool, error) {
	oc := other.GetChildren()

	if t.children.Len() != oc.Len() {
		return false, nil
	}

	for i, child := range t.children {
		ok, err := child.DeepEqual(oc[i])
		if err != nil {
			return false, err
		}

		if !ok {
			return false, nil
		}
	}

	return true, nil
}

// actionDescription provides a textual description of what is required to
// reconcile from the current state to a requested one.
func (t *treeNode) actionDescription() string {
	if t.action == "" {
		return ""
	}

	color := ansi.Black

	switch t.action {
	case mergeActionCreate:
		color = ansi.Green
	case mergeActionDelete:
		color = ansi.Red
	case mergeActionUpdate:
		color = ansi.Yellow
	}

	output := " " + ansi.Color(color) + string(t.action) + ansi.Reset()

	if t.action == mergeActionUpdate && t.patch != nil {
		output += " (merge patch: " + string(t.patch) + ")"
	}

	return output
}

// DeadCode removes any children marked as deleted, then recursively does
// the same for the remainder.
func (t *treeNode) DeadCode() {
	t.children = t.children.DeadCode()

	for _, child := range t.children {
		child.DeadCode()
	}
}

// rootNode is a sentinel type just to provide the required interface.
type rootNode struct {
	treeNode
}

// Describe returns a verbose description of the resource.
func (n *rootNode) Describe() string {
	return ""
}

// DeepCopy does exactly that, clones everything in a tree.
func (n *rootNode) DeepCopy() TreeNode {
	t := &rootNode{
		treeNode: n.treeNode.DeepCopy(),
	}

	return t
}

// SemanticEqual checks for semantic equality of resources i.e. do
// specifications match up.  If false, then the byte array is a
// JSON merge patch that can be used to communicate what has changed.
func (n *rootNode) SemanticEqual(other TreeNode) (bool, []byte, error) {
	if _, ok := other.(*rootNode); !ok {
		return false, nil, nil
	}

	return true, nil, nil
}

// DeepEqual checks for actual equality of resources i.e. do the
// resources have the same specification, name and descendants.
func (n *rootNode) DeepEqual(other TreeNode) (bool, error) {
	ok, _, err := n.SemanticEqual(other)
	if err != nil {
		return false, err
	}

	ook, err := n.deepEqual(other)
	if err != nil {
		return false, err
	}

	return ok && ook, nil
}

// GenerateResourceName creates a unique resource name for
// concrete resource types.
func (n *rootNode) GenerateResourceName() {
}

// ResourceName returns the nodes's resource Kubernetes name.
func (n *rootNode) ResourceName() string {
	return ""
}

// Link looks at a node's abstract children, and creates type
// specific linkage in the resource.  The labels are only relevant
// for buckets, as they are label selected.
func (n *rootNode) Link(_ labels.Set) error {
	return nil
}

func (n *rootNode) Resource() runtime.Object {
	return nil
}

// bucketNode represents a Couchbase bucket type.
type bucketNode struct {
	treeNode

	resource *couchbasev2.CouchbaseBucket
}

// Describe returns a verbose description of the resource.
func (n *bucketNode) Describe() string {
	return "(bucket)" + n.actionDescription()
}

// DeepCopy does exactly that, clones everything in a tree.
func (n *bucketNode) DeepCopy() TreeNode {
	t := &bucketNode{
		treeNode: n.treeNode.DeepCopy(),
		resource: n.resource.DeepCopy(),
	}

	return t
}

// SemanticEqual checks for semantic equality of resources i.e. do
// specifications match up.  If false, then the byte array is a
// JSON merge patch that can be used to communicate what has changed.
func (n *bucketNode) SemanticEqual(other TreeNode) (bool, []byte, error) {
	// Check types match, we can't be converting from a bucket to an ephemeral.
	o, ok := other.(*bucketNode)
	if !ok {
		return false, nil, nil
	}

	// Check for specification equality.  This ignores names and linked children.
	return diffResources(n.resource.Spec, o.resource.Spec)
}

// DeepEqual checks for actual equality of resources i.e. do the
// resources have the same specification, name and descendants.
func (n *bucketNode) DeepEqual(other TreeNode) (bool, error) {
	ok, _, err := n.SemanticEqual(other)
	if err != nil {
		return false, err
	}

	ook, err := n.deepEqual(other)
	if err != nil {
		return false, err
	}

	return ok && ook, nil
}

// GenerateResourceName creates a unique resource name for
// concrete resource types.
func (n *bucketNode) GenerateResourceName() {
	n.resource.Name = defaultSaveRestoreNamer.GenerateBucketName(nil)
}

// ResourceName returns the nodes's resource Kubernetes name.
func (n *bucketNode) ResourceName() string {
	return n.resource.Name
}

// Link looks at a node's abstract children, and creates type
// specific linkage in the resource.  The labels are only relevant
// for buckets, as they are label selected.
func (n *bucketNode) Link(l labels.Set) error {
	selector := &couchbasev2.ScopeSelector{
		Managed: true,
	}

	for _, child := range n.GetChildren() {
		switch child.(type) {
		case *scopeNode:
			r := couchbasev2.ScopeLocalObjectReference{
				Kind: couchbasev2.CouchbaseScopeKindScope,
				Name: couchbasev2.ScopeOrCollectionName(child.ResourceName()),
			}

			selector.Resources = append(selector.Resources, r)
		case *scopeGroupNode:
			r := couchbasev2.ScopeLocalObjectReference{
				Kind: couchbasev2.CouchbaseScopeKindScopeGroup,
				Name: couchbasev2.ScopeOrCollectionName(child.ResourceName()),
			}

			selector.Resources = append(selector.Resources, r)
		default:
			return fmt.Errorf("%w: unexpected child type", errInternalError)
		}
	}

	n.resource.Labels = l
	n.resource.Spec.Name = couchbasev2.BucketName(n.name)
	n.resource.Spec.Scopes = selector

	return nil
}

// Resource extracts the resource from a node.
func (n *bucketNode) Resource() runtime.Object {
	return n.resource
}

// ephemeralBucketNode represents a Couchbase ephemeral bucket type.
type ephemeralBucketNode struct {
	treeNode

	resource *couchbasev2.CouchbaseEphemeralBucket
}

// Describe returns a verbose description of the resource.
func (n *ephemeralBucketNode) Describe() string {
	return "(ephemeral bucket)" + n.actionDescription()
}

// DeepCopy does exactly that, clones everything in a tree.
func (n *ephemeralBucketNode) DeepCopy() TreeNode {
	t := &ephemeralBucketNode{
		treeNode: n.treeNode.DeepCopy(),
		resource: n.resource.DeepCopy(),
	}

	return t
}

// SemanticEqual checks for semantic equality of resources i.e. do
// specifications match up.  If false, then the byte array is a
// JSON merge patch that can be used to communicate what has changed.
func (n *ephemeralBucketNode) SemanticEqual(other TreeNode) (bool, []byte, error) {
	// Check types match, we can't be converting from a bucket to an ephemeral.
	o, ok := other.(*ephemeralBucketNode)
	if !ok {
		return false, nil, nil
	}

	// Check for specification equality.  This ignores names and linked children.
	return diffResources(n.resource.Spec, o.resource.Spec)
}

// DeepEqual checks for actual equality of resources i.e. do the
// resources have the same specification, name and descendants.
func (n *ephemeralBucketNode) DeepEqual(other TreeNode) (bool, error) {
	ok, _, err := n.SemanticEqual(other)
	if err != nil {
		return false, err
	}

	ook, err := n.deepEqual(other)
	if err != nil {
		return false, err
	}

	return ok && ook, nil
}

// GenerateResourceName creates a unique resource name for
// concrete resource types.
func (n *ephemeralBucketNode) GenerateResourceName() {
	n.resource.Name = defaultSaveRestoreNamer.GenerateEphemeralBucketName(nil)
}

// ResourceName returns the nodes's resource Kubernetes name.
func (n *ephemeralBucketNode) ResourceName() string {
	return n.resource.Name
}

// Link looks at a node's abstract children, and creates type
// specific linkage in the resource.  The labels are only relevant
// for buckets, as they are label selected.
func (n *ephemeralBucketNode) Link(l labels.Set) error {
	selector := &couchbasev2.ScopeSelector{
		Managed: true,
	}

	for _, child := range n.GetChildren() {
		switch child.(type) {
		case *scopeNode:
			r := couchbasev2.ScopeLocalObjectReference{
				Kind: couchbasev2.CouchbaseScopeKindScope,
				Name: couchbasev2.ScopeOrCollectionName(child.ResourceName()),
			}

			selector.Resources = append(selector.Resources, r)
		case *scopeGroupNode:
			r := couchbasev2.ScopeLocalObjectReference{
				Kind: couchbasev2.CouchbaseScopeKindScopeGroup,
				Name: couchbasev2.ScopeOrCollectionName(child.ResourceName()),
			}

			selector.Resources = append(selector.Resources, r)
		default:
			return fmt.Errorf("%w: unexpected child type", errInternalError)
		}
	}

	n.resource.Labels = l
	n.resource.Spec.Name = couchbasev2.BucketName(n.name)
	n.resource.Spec.Scopes = selector

	return nil
}

// Resource extracts the resource from a node.
func (n *ephemeralBucketNode) Resource() runtime.Object {
	return n.resource
}

// memcachedBucketNode represents a Couchbase memcached bucket type.
type memcachedBucketNode struct {
	treeNode

	resource *couchbasev2.CouchbaseMemcachedBucket
}

// Describe returns a verbose description of the resource.
func (n *memcachedBucketNode) Describe() string {
	return "(memcached bucket)" + n.actionDescription()
}

// DeepCopy does exactly that, clones everything in a tree.
func (n *memcachedBucketNode) DeepCopy() TreeNode {
	t := &memcachedBucketNode{
		treeNode: n.treeNode.DeepCopy(),
		resource: n.resource.DeepCopy(),
	}

	return t
}

// SemanticEqual checks for semantic equality of resources i.e. do
// specifications match up.  If false, then the byte array is a
// JSON merge patch that can be used to communicate what has changed.
func (n *memcachedBucketNode) SemanticEqual(other TreeNode) (bool, []byte, error) {
	// Check types match, we can't be converting from a bucket to an memcached.
	o, ok := other.(*memcachedBucketNode)
	if !ok {
		return false, nil, nil
	}

	// Check for specification equality.  This ignores names and linked children.
	return diffResources(n.resource.Spec, o.resource.Spec)
}

// DeepEqual checks for actual equality of resources i.e. do the
// resources have the same specification, name and descendants.
func (n *memcachedBucketNode) DeepEqual(other TreeNode) (bool, error) {
	ok, _, err := n.SemanticEqual(other)
	if err != nil {
		return false, err
	}

	ook, err := n.deepEqual(other)
	if err != nil {
		return false, err
	}

	return ok && ook, nil
}

// GenerateResourceName creates a unique resource name for
// concrete resource types.
func (n *memcachedBucketNode) GenerateResourceName() {
	n.resource.Name = defaultSaveRestoreNamer.GenerateMemcachedBucketName(nil)
}

// ResourceName returns the nodes's resource Kubernetes name.
func (n *memcachedBucketNode) ResourceName() string {
	return n.resource.Name
}

// Link looks at a node's abstract children, and creates type
// specific linkage in the resource.  The labels are only relevant
// for buckets, as they are label selected.
func (n *memcachedBucketNode) Link(l labels.Set) error {
	n.resource.Labels = l
	n.resource.Spec.Name = couchbasev2.BucketName(n.name)

	return nil
}

// Resource extracts the resource from a node.
func (n *memcachedBucketNode) Resource() runtime.Object {
	return n.resource
}

// scopeNode represents a Couchbase scope type.
type scopeNode struct {
	treeNode

	resource *couchbasev2.CouchbaseScope
}

// Describe returns a verbose description of the resource.
func (n *scopeNode) Describe() string {
	return "(scope)" + n.actionDescription()
}

// DeepCopy does exactly that, clones everything in a tree.
func (n *scopeNode) DeepCopy() TreeNode {
	t := &scopeNode{
		treeNode: n.treeNode.DeepCopy(),
		resource: n.resource.DeepCopy(),
	}

	return t
}

// SemanticEqual checks for semantic equality of resources i.e. do
// specifications match up.  If false, then the byte array is a
// JSON merge patch that can be used to communicate what has changed.
func (n *scopeNode) SemanticEqual(other TreeNode) (bool, []byte, error) {
	// Check types match.
	o, ok := other.(*scopeNode)
	if !ok {
		return false, nil, nil
	}

	// Check for specification equality.  This ignores names and linked children.
	return diffResources(n.resource.Spec, o.resource.Spec)
}

// DeepEqual checks for actual equality of resources i.e. do the
// resources have the same specification, name and descendants.
func (n *scopeNode) DeepEqual(other TreeNode) (bool, error) {
	ok, _, err := n.SemanticEqual(other)
	if err != nil {
		return false, err
	}

	ook, err := n.deepEqual(other)
	if err != nil {
		return false, err
	}

	return ok && ook, nil
}

// GenerateResourceName creates a unique resource name for
// concrete resource types.
func (n *scopeNode) GenerateResourceName() {
	n.resource.Name = defaultSaveRestoreNamer.GenerateScopeName(nil, nil)
}

// ResourceName returns the nodes's resource Kubernetes name.
func (n *scopeNode) ResourceName() string {
	return n.resource.Name
}

// Link looks at a node's abstract children, and creates type
// specific linkage in the resource.  The labels are only relevant
// for buckets, as they are label selected.
func (n *scopeNode) Link(_ labels.Set) error {
	selector := &couchbasev2.CollectionSelector{
		Managed: true,
	}

	for _, child := range n.GetChildren() {
		switch child.(type) {
		case *collectionNode:
			r := couchbasev2.CollectionLocalObjectReference{
				Kind: couchbasev2.CouchbaseCollectionKindCollection,
				Name: couchbasev2.ScopeOrCollectionName(child.ResourceName()),
			}

			selector.Resources = append(selector.Resources, r)
		case *collectionGroupNode:
			r := couchbasev2.CollectionLocalObjectReference{
				Kind: couchbasev2.CouchbaseCollectionKindCollectionGroup,
				Name: couchbasev2.ScopeOrCollectionName(child.ResourceName()),
			}

			selector.Resources = append(selector.Resources, r)
		default:
			return fmt.Errorf("%w: unexpected child type", errInternalError)
		}
	}

	n.resource.Spec.Name = couchbasev2.ScopeOrCollectionName(n.name)
	n.resource.Spec.Collections = selector

	return nil
}

// Resource extracts the resource from a node.
func (n *scopeNode) Resource() runtime.Object {
	return n.resource
}

// scopeGroupNode represents a Couchbase scope type.
type scopeGroupNode struct {
	treeNode

	resource *couchbasev2.CouchbaseScopeGroup
}

// Describe returns a verbose description of the resource.
func (n *scopeGroupNode) Describe() string {
	return "(scope group)" + n.actionDescription()
}

// DeepCopy does exactly that, clones everything in a tree.
func (n *scopeGroupNode) DeepCopy() TreeNode {
	t := &scopeGroupNode{
		treeNode: n.treeNode.DeepCopy(),
		resource: n.resource.DeepCopy(),
	}

	return t
}

// SemanticEqual checks for semantic equality of resources i.e. do
// specifications match up.  If false, then the byte array is a
// JSON merge patch that can be used to communicate what has changed.
func (n *scopeGroupNode) SemanticEqual(other TreeNode) (bool, []byte, error) {
	// Check types match.
	o, ok := other.(*scopeGroupNode)
	if !ok {
		return false, nil, nil
	}

	// Check for specification equality.  This ignores names and linked children.
	return diffResources(n.resource.Spec, o.resource.Spec)
}

// DeepEqual checks for actual equality of resources i.e. do the
// resources have the same specification, name and descendants.
func (n *scopeGroupNode) DeepEqual(other TreeNode) (bool, error) {
	ok, _, err := n.SemanticEqual(other)
	if err != nil {
		return false, err
	}

	ook, err := n.deepEqual(other)
	if err != nil {
		return false, err
	}

	return ok && ook, nil
}

// GenerateResourceName creates a unique resource name for
// concrete resource types.
func (n *scopeGroupNode) GenerateResourceName() {
	n.resource.Name = defaultSaveRestoreNamer.generateScopeGroupName()
}

// ResourceName returns the nodes's resource Kubernetes name.
func (n *scopeGroupNode) ResourceName() string {
	return n.resource.Name
}

// Link looks at a node's abstract children, and creates type
// specific linkage in the resource.  The labels are only relevant
// for buckets, as they are label selected.
func (n *scopeGroupNode) Link(_ labels.Set) error {
	selector := &couchbasev2.CollectionSelector{
		Managed: true,
	}

	for _, child := range n.GetChildren() {
		switch child.(type) {
		case *collectionNode:
			r := couchbasev2.CollectionLocalObjectReference{
				Kind: couchbasev2.CouchbaseCollectionKindCollection,
				Name: couchbasev2.ScopeOrCollectionName(child.ResourceName()),
			}

			selector.Resources = append(selector.Resources, r)
		case *collectionGroupNode:
			r := couchbasev2.CollectionLocalObjectReference{
				Kind: couchbasev2.CouchbaseCollectionKindCollectionGroup,
				Name: couchbasev2.ScopeOrCollectionName(child.ResourceName()),
			}

			selector.Resources = append(selector.Resources, r)
		default:
			return fmt.Errorf("%w: unexpected child type", errInternalError)
		}
	}

	n.resource.Spec.Collections = selector

	return nil
}

// Resource extracts the resource from a node.
func (n *scopeGroupNode) Resource() runtime.Object {
	return n.resource
}

// collectionNode represents a Couchbase collection.
type collectionNode struct {
	treeNode

	resource *couchbasev2.CouchbaseCollection
}

// Describe returns a verbose description of the resource.
func (n *collectionNode) Describe() string {
	return "(collection)" + n.actionDescription()
}

// DeepCopy does exactly that, clones everything in a tree.
func (n *collectionNode) DeepCopy() TreeNode {
	t := &collectionNode{
		treeNode: n.treeNode.DeepCopy(),
		resource: n.resource.DeepCopy(),
	}

	return t
}

// SemanticEqual checks for semantic equality of resources i.e. do
// specifications match up.  If false, then the byte array is a
// JSON merge patch that can be used to communicate what has changed.
func (n *collectionNode) SemanticEqual(other TreeNode) (bool, []byte, error) {
	// Check types match.
	o, ok := other.(*collectionNode)
	if !ok {
		return false, nil, nil
	}

	// Check for specification equality.  This ignores names and linked children.
	return diffResources(n.resource.Spec, o.resource.Spec)
}

// DeepEqual checks for actual equality of resources i.e. do the
// resources have the same specification, name and descendants.
func (n *collectionNode) DeepEqual(other TreeNode) (bool, error) {
	ok, _, err := n.SemanticEqual(other)
	if err != nil {
		return false, err
	}

	ook, err := n.deepEqual(other)
	if err != nil {
		return false, err
	}

	return ok && ook, nil
}

// GenerateResourceName creates a unique resource name for
// concrete resource types.
func (n *collectionNode) GenerateResourceName() {
	n.resource.Name = defaultSaveRestoreNamer.GenerateCollectionName(nil, nil, nil)
}

// ResourceName returns the nodes's resource Kubernetes name.
func (n *collectionNode) ResourceName() string {
	return n.resource.Name
}

// Link looks at a node's abstract children, and creates type
// specific linkage in the resource.  The labels are only relevant
// for buckets, as they are label selected.
func (n *collectionNode) Link(_ labels.Set) error {
	n.resource.Spec.Name = couchbasev2.ScopeOrCollectionName(n.name)

	return nil
}

// Resource extracts the resource from a node.
func (n *collectionNode) Resource() runtime.Object {
	return n.resource
}

// collectionGroupNode represents a Couchbase collection group.
type collectionGroupNode struct {
	treeNode

	resource *couchbasev2.CouchbaseCollectionGroup
}

// Describe returns a verbose description of the resource.
func (n *collectionGroupNode) Describe() string {
	return "(collection group)" + n.actionDescription()
}

// DeepCopy does exactly that, clones everything in a tree.
func (n *collectionGroupNode) DeepCopy() TreeNode {
	t := &collectionGroupNode{
		treeNode: n.treeNode.DeepCopy(),
		resource: n.resource.DeepCopy(),
	}

	return t
}

// SemanticEqual checks for semantic equality of resources i.e. do
// specifications match up.  If false, then the byte array is a
// JSON merge patch that can be used to communicate what has changed.
func (n *collectionGroupNode) SemanticEqual(other TreeNode) (bool, []byte, error) {
	// Check types match.
	o, ok := other.(*collectionGroupNode)
	if !ok {
		return false, nil, nil
	}

	// Check for specification equality.  This ignores names and linked children.
	return diffResources(n.resource.Spec, o.resource.Spec)
}

// DeepEqual checks for actual equality of resources i.e. do the
// resources have the same specification, name and descendants.
func (n *collectionGroupNode) DeepEqual(other TreeNode) (bool, error) {
	ok, _, err := n.SemanticEqual(other)
	if err != nil {
		return false, err
	}

	ook, err := n.deepEqual(other)
	if err != nil {
		return false, err
	}

	return ok && ook, nil
}

// GenerateResourceName creates a unique resource name for
// concrete resource types.
func (n *collectionGroupNode) GenerateResourceName() {
	n.resource.Name = defaultSaveRestoreNamer.generateCollectionGroupName()
}

// ResourceName returns the nodes's resource Kubernetes name.
func (n *collectionGroupNode) ResourceName() string {
	return n.resource.Name
}

// Link looks at a node's abstract children, and creates type
// specific linkage in the resource.  The labels are only relevant
// for buckets, as they are label selected.
func (n *collectionGroupNode) Link(_ labels.Set) error {
	return nil
}

// Resource extracts the resource from a node.
func (n *collectionGroupNode) Resource() runtime.Object {
	return n.resource
}

const (
	// lastChild is a marker that indicates this is the last child in a list.
	lastChild = iota

	// middleChild is a marker that indicates this is a middle (or first) child,
	// not the last one, in any case!
	middleChild
)

// Dump prints out the tree from an arbitrary node.
func Dump(n TreeNode, history ...int) {
	// Dump formatting is dependent on two variables, whether the history
	// for this level records a last child or not, and whether this is the last
	// history element.  It's a simple shift and add algorithm base on 0 and 1.
	format := []string{
		"└── ", // last child in history, last element, points to a data item.
		"    ", // last child in history, not the last element, has already been pointed to.
		"├── ", // not last child, but last element, so it needs pointing to and also has siblings.
		"│   ", // not last child, not last element, will be printed later.
	}

	output := "  "

	for i, h := range history {
		offt := middleChild

		if i+1 == len(history) {
			offt = lastChild
		}

		index := (h << 1) + offt

		output += format[index]
	}

	// Do the actual printing of the node.
	description := n.Describe()
	if description != "" {
		description = " " + description
	}

	log.Info(output + ansi.Color(ansi.Blue) + n.Name() + ansi.Reset() + description)

	// Process children, doing a lookahead to see if there are any further
	// siblings and adding that data onto the history.
	for i, c := range n.GetChildren() {
		hl := len(history)

		h := make([]int, hl+1)
		copy(h, history)

		if i+1 == len(n.GetChildren()) {
			h[hl] = lastChild
		} else {
			h[hl] = middleChild
		}

		Dump(c, h...)
	}
}

// BreadthFirstSearch returns a list of nodes in BFS order.
func BreadthFirstSearch(t TreeNode) TreeNodeList {
	queue := TreeNodeList{t}

	for i := 0; i < len(queue); i++ {
		queue = append(queue, queue[i].GetChildren()...)
	}

	return queue
}

// loadRestoreObjects reads a save file, breaks the YAML down into individual resources
// and then parses them into concrete types, returning them as abstract objects.
func (o *restoreOptions) loadRestoreObjects() ([]runtime.Object, error) {
	save, err := ioutil.ReadFile(o.filePath)
	if err != nil {
		return nil, err
	}

	var objects []runtime.Object

	for _, part := range strings.Split(string(save), "\n---\n") {
		// Tommie's favorite code :D
		u := &unstructured.Unstructured{}

		if err := yaml.Unmarshal([]byte(part), u); err != nil {
			return nil, err
		}

		objectKind := u.GetObjectKind()

		object, err := scheme.Scheme.New(objectKind.GroupVersionKind())
		if err != nil {
			return nil, err
		}

		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, object); err != nil {
			return nil, err
		}

		objects = append(objects, object)
	}

	return objects, nil
}

// objectToLevel takes an abstract object, then returns the level in the hierarchy it
// would be inserted at.
func objectToLevel(o runtime.Object) (int, error) {
	switch o.(type) {
	case *couchbasev2.CouchbaseBucket, *couchbasev2.CouchbaseEphemeralBucket, *couchbasev2.CouchbaseMemcachedBucket:
		return hierarchyFullTree, nil
	case *couchbasev2.CouchbaseScope:
		return hierarchyBucket, nil
	case *couchbasev2.CouchbaseCollection:
		return hierarchyScope, nil
	}

	return hierarchyMax, fmt.Errorf("%w: unhandled object kind %v", errInternalError, o.GetObjectKind())
}

// resourceMapping maps from resource name to Kubernetes object.
type resourceMapping map[string]runtime.Object

// levelResourceMapping has levels, representing buckets, scopes and collections,
// each of which is a map from name to the underlying Kubernetes object.
type levelResourceMapping [hierarchyMax]resourceMapping

// recurseBuild takes a map of resources discovered at various levels, and the current level
// we are processing.  It also takes a root node and a current object under interrogation.
// The object is translated into a tree node, that is attached to the current root.  All of
// that object's children are then read out of the next level up, and are recursively processed
// relative to the new root node we've just created.  tl;dr it builds a tree from resources
// based on their parent-child relationships.  When nodes are constructed, the resources are
// stripped of all their instance-specific stuff, like names and references, this yields
// resources that can be easily checked to "semantic equivalence".
func recurseBuild(levels levelResourceMapping, root TreeNode, o runtime.Object, level int) error {
	var node TreeNode

	var children []string

	switch t := o.(type) {
	case *couchbasev2.CouchbaseBucket:
		if t.Spec.Scopes.Selector != nil {
			return fmt.Errorf("%w: unexpected selector in resource", errParameterError)
		}

		name := t.GetCouchbaseName()

		r := t.DeepCopy()
		r.Spec.Name = ""
		r.Spec.Scopes = nil

		node = &bucketNode{
			treeNode: newTreeNode(name),
			resource: r,
		}

		for _, child := range t.Spec.Scopes.Resources {
			if child.Kind != couchbasev2.CouchbaseScopeKindScope {
				return fmt.Errorf("%w: unexpected resource kind", errParameterError)
			}

			children = append(children, string(child.Name))
		}
	case *couchbasev2.CouchbaseEphemeralBucket:
		if t.Spec.Scopes.Selector != nil {
			return fmt.Errorf("%w: unexpected selector in resource", errParameterError)
		}

		name := t.GetCouchbaseName()

		r := t.DeepCopy()
		r.Spec.Name = ""
		r.Spec.Scopes = nil

		node = &ephemeralBucketNode{
			treeNode: newTreeNode(name),
			resource: r,
		}

		for _, child := range t.Spec.Scopes.Resources {
			if child.Kind != couchbasev2.CouchbaseScopeKindScope {
				return fmt.Errorf("%w: unexpected resource kind", errParameterError)
			}

			children = append(children, string(child.Name))
		}

	case *couchbasev2.CouchbaseMemcachedBucket:
		name := t.GetCouchbaseName()

		r := t.DeepCopy()
		r.Spec.Name = ""

		node = &memcachedBucketNode{
			treeNode: newTreeNode(name),
			resource: r,
		}
	case *couchbasev2.CouchbaseScope:
		if t.Spec.Collections.Selector != nil {
			return fmt.Errorf("%w: unexpected selector in resource", errParameterError)
		}

		name := t.CouchbaseName()

		r := t.DeepCopy()
		r.Spec.Name = ""
		r.Spec.Collections = nil

		node = &scopeNode{
			treeNode: newTreeNode(name),
			resource: r,
		}

		for _, child := range t.Spec.Collections.Resources {
			if child.Kind != couchbasev2.CouchbaseCollectionKindCollection {
				return fmt.Errorf("%w: unexpected resource kind", errParameterError)
			}

			children = append(children, string(child.Name))
		}

	case *couchbasev2.CouchbaseCollection:
		name := t.CouchbaseName()

		r := t.DeepCopy()
		r.Spec.Name = ""

		node = &collectionNode{
			treeNode: newTreeNode(name),
			resource: r,
		}
	default:
		return fmt.Errorf("%w: unhandled object kind %v", errInternalError, o.GetObjectKind())
	}

	root.AddChild(node)

	for _, child := range children {
		childObject, ok := levels[level+1][child]
		if !ok {
			return fmt.Errorf("%w: inconsistency, child %s missing from level %d", errInternalError, child, level)
		}

		if err := recurseBuild(levels, node, childObject, level+1); err != nil {
			return err
		}
	}

	return nil
}

// build takes a bunch of abstract resources and orders them into buckets based
// on their level in the hierarchy, it then does some sanity checking based on
// the expected 'level', e.g. does the most significant object type tally with
// the path provided.  Finally the tree is built.
func build(objects []runtime.Object, path pathVar, save bool) (TreeNode, error) {
	// We're going to sort the objects into buckets of the same kind.
	// i.e. buckets, scopes and collections.  These are stored as hashes
	// to make relationships easier to lookup.
	var levels levelResourceMapping

	// As we go along, find the apparent level the save was collected at.
	// The definition of a level is defined by a pathVar.
	level := hierarchyMax

	// When not a save (i.e. from a live cluster), force the level to be
	// a full tree.  This allows the builder to successfully validate a
	// virgin cluster with no data topology defined.
	if !save {
		level = hierarchyFullTree
	}

	for _, o := range objects {
		objectLevel, err := objectToLevel(o)
		if err != nil {
			return nil, err
		}

		// Set the apparent level to that of the most significant object,
		// e.g. buckets take precedence.
		level = intMin(level, objectLevel)

		// Add the object to the level's bucket.
		if levels[objectLevel] == nil {
			levels[objectLevel] = resourceMapping{}
		}

		levels[objectLevel][o.(metav1.Object).GetName()] = o
	}

	// Check the level is set, if not, then the implication is nothing was
	// actually in the save data.
	if level == hierarchyMax {
		return nil, fmt.Errorf("%w: save contains no resources", errParameterError)
	}

	// Check the level of the save matches the level of the restore path.
	if level != path.level {
		return nil, fmt.Errorf("%w: save level %d does not match patch level %d", errParameterError, level, path.level)
	}

	// Now we have a base level, we are going to use that as the base level
	// to start building up our data topology tree...
	root := &rootNode{
		treeNode{
			name: path.path,
		},
	}

	for _, o := range levels[level] {
		if err := recurseBuild(levels, root, o, level); err != nil {
			return nil, err
		}
	}

	return root, nil
}

// splice takes the requested tree and splices it into the current tree at the
// location as defined by the path.  This yield a full tree when only a partial
// save is provided, which can then be merged with the current full tree.
func splice(current, requested TreeNode, path pathVar) (TreeNode, error) {
	// Copy the current tree, this will form the basis of our spliced tree.
	spliced := current.DeepCopy()

	// Starting that the root, descend through the tree along the path
	// to determine where to perform the splice.
	spliceRoot := spliced

	for _, element := range path.elements {
		child, err := spliceRoot.GetChild(element)
		if err != nil {
			return nil, err
		}

		spliceRoot = child
	}

	// Clear the children from the splice root, and deep copy the requested
	// children into their place.
	spliceRoot.ClearChildren()

	for _, child := range requested.GetChildren() {
		spliceRoot.AddChild(child.DeepCopy())
	}

	return spliced, nil
}

// merge takes two full trees, and merges them together.  Nodes that are equal are
// marked as retained, those that aren't are marked for update.  Those that exist only
// in the requested tree are marked for creation, and those in the current tree only
// are marked based up the merge strategy -- either they will be retained or deleted.
func (o *restoreOptions) merge(current, requested TreeNode) (TreeNode, error) {
	// Check the current nodes for equality.  If they are same we will
	// mark the node as retained, needing no updates, otherwise we will
	// mark it as requiring an update.
	ok, patch, err := current.SemanticEqual(requested)
	if err != nil {
		return nil, err
	}

	action := mergeActionRetain

	if !ok {
		action = mergeActionUpdate
	}

	// Copy the requested node (that has precedence as it contains the
	// updated specification), and set its merge action.
	// TODO: a shallow copy will remove the need to deep copy all the children
	// then remove them again instantly.
	n := requested.DeepCopy()
	n.ClearChildren()
	n.SetMergeAction(action)
	n.SetMergePatch(patch)

	// Now the fun bit...  Children are ordered lexically by name for a reason
	// and this is it.  We essentially do a "merge-sort" merge algorithm here,
	// but discarding duplicates.  Keep a pointer to each list and increment as
	// we "pop" items off them.  If nodes have the same name, then we recursively
	// merge down, and that will de-duplicate, selecting the requested version.
	// If nodes have different names, we select the one with the smallest name,
	// and pop that off (thus maintaining ordering).  The list we pop the small
	// node off determines the action we need to perform.
	cc := current.GetChildren()
	rc := requested.GetChildren()

	ccp := 0
	rcp := 0

	for ccp < len(cc) && rcp < len(rc) {
		switch strings.Compare(cc[ccp].Name(), rc[rcp].Name()) {
		case 0: // equal
			child, err := o.merge(cc[ccp], rc[rcp])
			if err != nil {
				return nil, err
			}

			n.AddChild(child)

			ccp++
			rcp++
		case 1: // current > requested, add requested.
			child := rc[rcp].DeepCopy()
			child.SetMergeAction(mergeActionCreate)

			n.AddChild(child)

			rcp++
		case -1: // current < requested, add current.
			child := cc[ccp].DeepCopy()
			child.SetMergeAction(o.strategy.mergeAction())

			n.AddChild(child)

			ccp++
		}
	}

	// Obviously, once one of the lists is exhausted in the above loop, then
	// we cannot compare any more, so need to mop up any leftovers.
	for _, c := range cc[ccp:] {
		child := c.DeepCopy()
		child.SetMergeAction(o.strategy.mergeAction())

		n.AddChild(child)
	}

	for _, c := range rc[rcp:] {
		child := c.DeepCopy()
		child.SetMergeAction(mergeActionCreate)

		n.AddChild(child)
	}

	return n, nil
}

// compact does a reverse breadth first traversal of the tree.  If we spot any
// collections or scopes that can be merged into a group, then do so to improve
// performance.
func (o *restoreOptions) compact(merged TreeNode) (TreeNode, error) {
	// Get copy our merge tree, removing anything that needs deleting,
	// we only care about things that need preserving, updating or creating
	// at this point.
	compacted := merged.DeepCopy()
	compacted.DeadCode()

	// Determine the BFS ordering of the nodes, in reverse so we start with
	// the leaves, and iterate over it...
	for _, node := range BreadthFirstSearch(compacted).Reverse() {
		children := node.GetChildren()

		// If no children or an only child, then ignore, there is no compression yet.
		// TODO: resource sharing will reduce API overhead.
		if children.Len() < 2 {
			continue
		}

		// If all the kids are equal, then we may be able to compress them.
		// TODO: there may be groups of children that can be compressed.
		ok, err := children.Mergeable()
		if err != nil {
			return nil, err
		}

		if !ok {
			continue
		}

		// Select the first child, this is going to be our source of truth
		// for resource configuration, and any grandchildren.
		firstChild := children[0]

		// TODO: full path would be useful here.
		log.V(cli.Debug).Info("Merging children under node", node.Name())

		// Gather the names of all the children, and cast them into the common type
		// across all group types.
		var names []couchbasev2.ScopeOrCollectionName

		for _, child := range children {
			names = append(names, couchbasev2.ScopeOrCollectionName(child.Name()))
		}

		name := strings.Join(couchbasev2.ScopeOrCollectionNameList(names).StringSlice(), ", ")

		// Based on the child type, create a group node, copying over any
		// resource type specific configuration.
		var child TreeNode

		switch node.(type) {
		case *scopeNode:
			collection, ok := firstChild.(*collectionNode)
			if !ok {
				return nil, fmt.Errorf("%w: unexpected child resource type", errInternalError)
			}

			child = &collectionGroupNode{
				treeNode: newTreeNode(name),
				resource: &couchbasev2.CouchbaseCollectionGroup{
					TypeMeta: metav1.TypeMeta{
						APIVersion: couchbasev2.Group,
						Kind:       "CouchbaseCollectionGroup",
					},
					Spec: couchbasev2.CouchbaseCollectionGroupSpec{
						Names: names,
						CouchbaseCollectionSpecCommon: couchbasev2.CouchbaseCollectionSpecCommon{
							MaxTTL: collection.resource.Spec.MaxTTL,
						},
					},
				},
			}
		case *bucketNode, *ephemeralBucketNode:
			child = &scopeGroupNode{
				treeNode: newTreeNode(name),
				resource: &couchbasev2.CouchbaseScopeGroup{
					TypeMeta: metav1.TypeMeta{
						APIVersion: couchbasev2.Group,
						Kind:       "CouchbaseScopeGroup",
					},
					Spec: couchbasev2.CouchbaseScopeGroupSpec{
						Names: names,
					},
				},
			}
		default:
			return nil, fmt.Errorf("%w: unexpected resource type", errInternalError)
		}

		// Copy any grandchildren from the first child to the new child.
		child.AddChildren(firstChild.GetChildren())

		// Usurp all children with the new child.
		node.ClearChildren()
		node.AddChild(child)
	}

	return compacted, nil
}

// gatherDataTopologyResources looks at the cluster resource, and collects any bucket resources
// linked to it, and then recursively gathers descendants.
func gatherDataTopologyResources(clients *clients, cluster *couchbasev2.CouchbaseCluster) ([]runtime.Object, error) {
	if !cluster.Spec.Buckets.Managed {
		return nil, nil
	}

	selector, err := cluster.GetBucketLabelSelector()
	if err != nil {
		return nil, err
	}

	buckets, err := clients.couchbaseClient.CouchbaseV2().CouchbaseBuckets(clients.namespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}

	ephemeralBuckets, err := clients.couchbaseClient.CouchbaseV2().CouchbaseEphemeralBuckets(clients.namespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}

	memcachedBuckets, err := clients.couchbaseClient.CouchbaseV2().CouchbaseMemcachedBuckets(clients.namespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}

	var resources []runtime.Object

	for _, bucket := range buckets.Items {
		b := bucket
		resources = append(resources, &b)
	}

	for _, bucket := range ephemeralBuckets.Items {
		b := bucket
		resources = append(resources, &b)
	}

	for _, bucket := range memcachedBuckets.Items {
		b := bucket
		resources = append(resources, &b)
	}

	for _, bucket := range buckets.Items {
		r, err := gatherScopeResources(clients, bucket.Spec.Scopes)
		if err != nil {
			return nil, err
		}

		resources = append(resources, r...)
	}

	for _, bucket := range ephemeralBuckets.Items {
		r, err := gatherScopeResources(clients, bucket.Spec.Scopes)
		if err != nil {
			return nil, err
		}

		resources = append(resources, r...)
	}

	return resources, nil
}

// gatherScopeResources collects scope resources under the control of a scope selector, then
// recursively gathers descendants.
// nolint:gocognit
func gatherScopeResources(clients *clients, scopeSelector *couchbasev2.ScopeSelector) ([]runtime.Object, error) {
	if scopeSelector == nil || !scopeSelector.Managed {
		return nil, nil
	}

	var scopes []couchbasev2.CouchbaseScope

	var scopeGroups []couchbasev2.CouchbaseScopeGroup

	for _, resource := range scopeSelector.Resources {
		switch resource.Kind {
		case couchbasev2.CouchbaseScopeKindScope:
			scope, err := clients.couchbaseClient.CouchbaseV2().CouchbaseScopes(clients.namespace).Get(context.Background(), string(resource.Name), metav1.GetOptions{})
			if err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}

				return nil, err
			}

			// GET does not fill this in.
			scope.APIVersion = couchbasev2.Group
			scope.Kind = couchbasev2.ScopeCRDResourceKind

			scopes = append(scopes, *scope)
		case couchbasev2.CouchbaseScopeKindScopeGroup:
			scopeGroup, err := clients.couchbaseClient.CouchbaseV2().CouchbaseScopeGroups(clients.namespace).Get(context.Background(), string(resource.Name), metav1.GetOptions{})
			if err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}

				return nil, err
			}

			// GET does not fill this in.
			scopeGroup.APIVersion = couchbasev2.Group
			scopeGroup.Kind = couchbasev2.ScopeGroupCRDResourceKind

			scopeGroups = append(scopeGroups, *scopeGroup)
		default:
			return nil, fmt.Errorf("%w: invalid kind", errInternalError)
		}
	}

	if scopeSelector.Selector != nil {
		selectedScopes, err := clients.couchbaseClient.CouchbaseV2().CouchbaseScopes(clients.namespace).List(context.Background(), metav1.ListOptions{LabelSelector: scopeSelector.Selector.String()})
		if err != nil {
			return nil, err
		}

		selectedScopeGroups, err := clients.couchbaseClient.CouchbaseV2().CouchbaseScopeGroups(clients.namespace).List(context.Background(), metav1.ListOptions{LabelSelector: scopeSelector.Selector.String()})
		if err != nil {
			return nil, err
		}

		scopes = append(scopes, selectedScopes.Items...)
		scopeGroups = append(scopeGroups, selectedScopeGroups.Items...)
	}

	var resources []runtime.Object

	for _, scope := range scopes {
		t := scope
		resources = append(resources, &t)
	}

	for _, scopeGroup := range scopeGroups {
		t := scopeGroup
		resources = append(resources, &t)
	}

	for _, scope := range scopes {
		r, err := gatherCollectionResources(clients, scope.Spec.Collections)
		if err != nil {
			return nil, err
		}

		resources = append(resources, r...)
	}

	for _, scopeGroup := range scopeGroups {
		r, err := gatherCollectionResources(clients, scopeGroup.Spec.Collections)
		if err != nil {
			return nil, err
		}

		resources = append(resources, r...)
	}

	return resources, nil
}

// gatherCollectionResources gathers any collection resources under the control of a selector.
func gatherCollectionResources(clients *clients, collectionSelector *couchbasev2.CollectionSelector) ([]runtime.Object, error) {
	if collectionSelector == nil || !collectionSelector.Managed {
		return nil, nil
	}

	var collections []couchbasev2.CouchbaseCollection

	var collectionGroups []couchbasev2.CouchbaseCollectionGroup

	for _, resource := range collectionSelector.Resources {
		switch resource.Kind {
		case couchbasev2.CouchbaseCollectionKindCollection:
			collection, err := clients.couchbaseClient.CouchbaseV2().CouchbaseCollections(clients.namespace).Get(context.Background(), string(resource.Name), metav1.GetOptions{})
			if err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}

				return nil, err
			}

			// GET does not fill this in.
			collection.APIVersion = couchbasev2.Group
			collection.Kind = couchbasev2.CollectionCRDResourceKind

			collections = append(collections, *collection)
		case couchbasev2.CouchbaseCollectionKindCollectionGroup:
			collectionGroup, err := clients.couchbaseClient.CouchbaseV2().CouchbaseCollectionGroups(clients.namespace).Get(context.Background(), string(resource.Name), metav1.GetOptions{})
			if err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}

				return nil, err
			}

			// GET does not fill this in.
			collectionGroup.APIVersion = couchbasev2.Group
			collectionGroup.Kind = couchbasev2.CollectionGroupCRDResourceKind

			collectionGroups = append(collectionGroups, *collectionGroup)
		default:
			return nil, fmt.Errorf("%w: invalid kind", errInternalError)
		}
	}

	if collectionSelector.Selector != nil {
		selectedCollections, err := clients.couchbaseClient.CouchbaseV2().CouchbaseCollections(clients.namespace).List(context.Background(), metav1.ListOptions{LabelSelector: collectionSelector.Selector.String()})
		if err != nil {
			return nil, err
		}

		selectedCollectionGroups, err := clients.couchbaseClient.CouchbaseV2().CouchbaseCollectionGroups(clients.namespace).List(context.Background(), metav1.ListOptions{LabelSelector: collectionSelector.Selector.String()})
		if err != nil {
			return nil, err
		}

		collections = append(collections, selectedCollections.Items...)
		collectionGroups = append(collectionGroups, selectedCollectionGroups.Items...)
	}

	var resources []runtime.Object

	for _, collection := range collections {
		t := collection
		resources = append(resources, &t)
	}

	for _, collectionGroup := range collectionGroups {
		t := collectionGroup
		resources = append(resources, &t)
	}

	return resources, nil
}

// link takes the abstract topology tree, names all the resources and then links them
// together with resource references.
func (o *restoreOptions) link(cluster string, current TreeNode) (labels.Set, []runtime.Object, error) {
	root := current.DeepCopy()

	l := labels.Set{
		"cluster":    cluster,
		"generation": uuid.New().String(),
	}

	order := BreadthFirstSearch(root)

	for _, node := range order {
		node.GenerateResourceName()
	}

	for _, node := range order {
		if err := node.Link(l); err != nil {
			return nil, nil, err
		}
	}

	var resources []runtime.Object

	for _, node := range order {
		resource := node.Resource()
		if resource == nil {
			continue
		}

		resources = append(resources, resource)
	}

	return l, resources, nil
}

// pivotRoot is an operation that creates new resources, then atomically switches the
// cluster over to use them.  The key word here is atomic, so if an error is raised
// at any point, we can do a rollback.
func pivotRoot(clients *clients, cluster *couchbasev2.CouchbaseCluster, l labels.Set, resources []runtime.Object) error {
	for _, resource := range resources {
		o, err := runtime.DefaultUnstructuredConverter.ToUnstructured(resource)
		if err != nil {
			return err
		}

		object := &unstructured.Unstructured{
			Object: o,
		}

		gvk := object.GroupVersionKind()

		mapping, err := clients.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return err
		}

		if _, err := clients.dynamicClient.Resource(mapping.Resource).Namespace(clients.namespace).Create(context.Background(), object, metav1.CreateOptions{}); err != nil {
			return err
		}

		log.Infof("%s/%s created", mapping.Resource.Resource, object.GetName())
	}

	cluster.Spec.Buckets.Managed = true
	cluster.Spec.Buckets.Selector = &metav1.LabelSelector{
		MatchLabels: l,
	}

	if _, err := clients.couchbaseClient.CouchbaseV2().CouchbaseClusters(clients.namespace).Update(context.Background(), cluster, metav1.UpdateOptions{}); err != nil {
		return err
	}

	log.Infof("couchbasecluster/%s updated", cluster.Name)

	return nil
}

// cleanup is primarily used to take pity on users and clean up any existing
// resources left lying around -- of which there many be thousands.  It's also
// used to rollback any failed pivots.
func cleanup(clients *clients, resources []runtime.Object) error {
	// Because resources can be referenced by multiple sources, we old need to delete
	// them once.
	// TODO: can make the collection algorithm more intelligent, but that's a lot
	// more complexity.
	seen := map[string]interface{}{}

	for _, resource := range resources {
		o, err := runtime.DefaultUnstructuredConverter.ToUnstructured(resource)
		if err != nil {
			return err
		}

		object := &unstructured.Unstructured{
			Object: o,
		}

		// Deduplicate deletions...
		id := object.GetKind() + "/" + object.GetName()

		if _, ok := seen[id]; ok {
			log.V(cli.Debug).Info("Seen", id, " so skipping")

			continue
		}

		seen[id] = nil

		gvk := object.GroupVersionKind()

		mapping, err := clients.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return err
		}

		if err := clients.dynamicClient.Resource(mapping.Resource).Namespace(clients.namespace).Delete(context.Background(), object.GetName(), metav1.DeleteOptions{}); err != nil {
			log.Info(err)
			continue
		}

		log.Infof("%s/%s deleted", mapping.Resource.Resource, object.GetName())
	}

	return nil
}
