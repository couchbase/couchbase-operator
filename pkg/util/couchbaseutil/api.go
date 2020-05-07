package couchbaseutil

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/urlencoding"
)

// Certificate and key used by TLS client authentication
type TLSClientAuth struct {
	// PEM encoded certificate
	Cert []byte
	// PEM encoded private key
	Key []byte
}

// TLS Authentication parameters
type TLSAuth struct {
	// PEM encoded CA certificate
	CACert []byte
	// Optional client authentication
	ClientAuth *TLSClientAuth
}

// Client certificate authentication prefixes, used to extract the user name
// All fields must be specified, so no "omitempty" tags.
type ClientCertAuthPrefix struct {
	Path      string `json:"path"`
	Prefix    string `json:"prefix"`
	Delimiter string `json:"delimiter"`
}

// Client certificate authentication settings
// All fields must be specified, so no "omitempty" tags.
type ClientCertAuth struct {
	// Must be 'disable', 'enable', 'mandatory'
	State string `json:"state"`
	// Maximum of 10
	Prefixes []ClientCertAuthPrefix `json:"prefixes"`
}

// UserAgent defines the HTTP User-Agent header string
type UserAgent struct {
	// Name is the unique name of the client e.g. couchbase-operator
	Name string
	// Version is the release version of the client
	Version string
	// UUID is a unique identifier of the client to differentiate it from
	// other clients of the same Name e.g. a FQDN.  This field is optional
	UUID string
}

// Couchbase is a structure which encapsulates HTTP API access to a
// Couchbase cluster
type Couchbase struct {
	// endpoints is a list of URIs to try when performing and operation
	endpoints []string
	// username is used in basic HTTP authorization
	username string
	// password is used in basic HTTP authorization
	password string
	// uuid, when set, is used to verify that endpoints we are connecting
	// to are part of the specified cluster and will return reliable
	// responses
	uuid string
	// tls, if set, specifies the client certificate chain and private keys
	// for mutual verification.  It also contains at least a CA certificate
	// to authenticate the server is trustworthy
	tls *TLSAuth
	// client is a persistent connection pool to be used by all endpoints
	// associated with this connection context.  It will become invalid
	// if any parameters used in the TLS handshake, or HTTP UUID check
	// are updated.
	client *http.Client
	// userAgent is sent to the server for all API requests and allows us
	// to uniquely identify the client e.g. differentiates from other go
	// tools or even instances of couchbase-operator
	userAgent *UserAgent
}

// New creates a new Couchbase HTTP(S) API client and initializes the
// HTTP connection pool.
func New(username, password string) *Couchbase {
	c := &Couchbase{
		endpoints: []string{},
		username:  username,
		password:  password,
	}

	c.makeClient()

	return c
}

// SetEndpoints sets the current working set of endpoints to try API requests
// against in the event of failure.  The endpoint host and port will be used
// to lookup a persistent connection in the http.Client.
func (c *Couchbase) SetEndpoints(endpoints []string) {
	c.endpoints = endpoints
}

// SetUUID updates the cluster UUID to check new connections against.  Creates
// a new client object to flush existing persistent connections.
func (c *Couchbase) SetUUID(uuid string) {
	c.uuid = uuid
	c.makeClient()
}

// SetTLS updates the client TLS settings.  Creates a new client object to
// flush existing persistent connections.
func (c *Couchbase) SetTLS(tls *TLSAuth) {
	c.tls = tls
	c.makeClient()
}

// GetTLS returns the TLS configuration in use by the client.
func (c *Couchbase) GetTLS() *TLSAuth {
	return c.tls
}

// SetUserAgent sets the User-Agent header to be sent in subsequent HTTP
// requests.
func (c *Couchbase) SetUserAgent(userAgent *UserAgent) {
	c.userAgent = userAgent
}

// CloseIdleConnections forces all TCP sessions with Couchbase to terminate, as
// it will sometime prioritize keeping a client happy rather than being in the
// correct state.
func (c *Couchbase) CloseIdleConnections() {
	c.client.CloseIdleConnections()
}

// RebalanceProgressEntry is the type communicated to clients periodically
// over a channel.
type RebalanceStatusEntry struct {
	// Status is the status of a rebalance.
	Status RebalanceStatus
	// Progress is how far the rebalance has progressed, only valid when
	// the status is RebalanceStatusRunning.
	Progress float64
}

// RebalanceProgress is a type used to monitor rebalance status.
type RebalanceProgress interface {
	// Status is a channel that is periodically updated with the rebalance
	// status and progress.  This channel is closed once the rebalance task
	// is no longer running or an error was detected.
	Status() <-chan *RebalanceStatusEntry
	// Error is used to check the error status when the Status channel has
	// been closed.
	Error() error
	// Cancel allows the client to stop the go routine before a rebalance has
	// completed.
	Cancel()
}

// rebalanceProgressImpl implements the RebalanceProgress interface.
type rebalanceProgressImpl struct {
	// statusChan is the main channel for communicating status to the client.
	statusChan chan *RebalanceStatusEntry
	// err is used to return any error encountered
	err error
	// context is a context used to cancel the rebalance progress
	context context.Context
	// cancel is used to cancel the rebalance progress
	cancel context.CancelFunc
}

// NewRebalanceProgress creates a new RebalanceProgress object and starts a go routine to
// periodically poll for updates.
func (c *Couchbase) NewRebalanceProgress() RebalanceProgress {
	progress := &rebalanceProgressImpl{
		statusChan: make(chan *RebalanceStatusEntry),
	}

	progress.context, progress.cancel = context.WithCancel(context.Background())

	go func() {
	RoutineRunloop:
		for {
			task, err := c.getRebalanceTask()
			if err != nil {
				progress.err = err

				close(progress.statusChan)

				break RoutineRunloop
			}

			status := getRebalanceStatus(task)

			// If the task is no longer running then terminate the routine.
			if status == RebalanceStatusNotRunning {
				close(progress.statusChan)

				break RoutineRunloop
			}

			// Otherwise return the status to the client
			progress.statusChan <- &RebalanceStatusEntry{
				Status:   status,
				Progress: task.Progress,
			}

			// Wait for a period of time or for the client to close the
			// progress.  Do this in the loop tail to maintain compatibility
			// with the old code.
			select {
			case <-time.After(4 * time.Second):
			case <-progress.context.Done():
				break RoutineRunloop
			}
		}
	}()

	return progress
}

// Status returns the RebalanceProgress status channel.
func (r *rebalanceProgressImpl) Status() <-chan *RebalanceStatusEntry {
	return r.statusChan
}

// Error returns the RebalanceProgress error channel.
func (r *rebalanceProgressImpl) Error() error {
	return r.err
}

// Cancel terminates the RebalanceProgress routine.
func (r *rebalanceProgressImpl) Cancel() {
	r.cancel()
}

// getRebalanceStatus transforms a task status into our simplified rebalance status.
func getRebalanceStatus(task *Task) RebalanceStatus {
	// We treat stale or timed out tasks as unknown, no status
	// is treated as not running.
	status := RebalanceStatus(task.Status)

	switch {
	case task.Stale || task.Timeout:
		status = RebalanceStatusUnknown
	case status == RebalanceStatusNone:
		status = RebalanceStatusNotRunning
	}

	return status
}

// getRebalanceTask polls the Couchbase API for tasks and returns the one associated
// with a rebalance.
func (c *Couchbase) getRebalanceTask() (*Task, error) {
	tasks, err := c.getTasks()
	if err != nil {
		return nil, err
	}

	for _, task := range tasks {
		if task.Type == "rebalance" {
			return task, nil
		}
	}

	return nil, fmt.Errorf("no rebalance task detected")
}

// getXDCRTasks returns tasks associated with XDCR replications.
func (c *Couchbase) getXDCRTasks() (xdcrTasks []*Task, err error) {
	tasks, err := c.getTasks()
	if err != nil {
		return
	}

	for _, task := range tasks {
		if task.Type == "xdcr" {
			xdcrTasks = append(xdcrTasks, task)
		}
	}

	return
}

// getOTPNode translates the hostname (that everyone expects) into an OTP node (whatever
// the hell that is and should not be exposed).
func (c *Couchbase) getOTPNode(hostname string) (string, error) {
	cluster, err := c.ClusterInfo()
	if err != nil {
		return "", err
	}

	for _, node := range cluster.Nodes {
		if node.HostName == hostname {
			return node.OTPNode, nil
		}
	}

	return "", fmt.Errorf("unable hostname %s to translate to OTP node", hostname)
}

func (c *Couchbase) AddNode(hostname, username, password string, services fmt.Stringer) error {
	data := url.Values{}
	data.Set("hostname", hostname)
	data.Set("user", username)
	data.Set("password", password)
	data.Set("services", services.String())

	headers := c.defaultHeaders()
	headers.Set("Content-Type", ContentTypeURLEncoded)

	return c.doPost("/controller/addNode", []byte(data.Encode()), nil, headers)
}

func (c *Couchbase) CancelAddNode(hostname string) error {
	otpNode, err := c.getOTPNode(hostname)
	if err != nil {
		return err
	}

	data := url.Values{}
	data.Set("otpNode", otpNode)

	headers := c.defaultHeaders()
	headers.Set("Content-Type", ContentTypeURLEncoded)

	return c.doPost("/controller/ejectNode", []byte(data.Encode()), nil, headers)
}

func (c *Couchbase) CancelAddBackNode(hostname string) error {
	otpNode, err := c.getOTPNode(hostname)
	if err != nil {
		return err
	}

	data := url.Values{}
	data.Set("otpNode", otpNode)

	headers := c.defaultHeaders()
	headers.Set("Content-Type", ContentTypeURLEncoded)

	return c.doPost("/controller/reFailOver", []byte(data.Encode()), nil, headers)
}

func (c *Couchbase) ClusterInfo() (*ClusterInfo, error) {
	clusterInfo := &ClusterInfo{}

	err := c.doGet("/pools/default", clusterInfo, c.defaultHeaders())
	if err != nil {
		return nil, err
	}

	return clusterInfo, nil
}

func (c *Couchbase) SetPoolsDefault(defaults *PoolsDefaults) error {
	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	data, err := urlencoding.Marshal(defaults)
	if err != nil {
		return err
	}

	return c.doPost("/pools/default", data, nil, headers)
}

func (c *Couchbase) setServices(services ServiceList) error {
	data := url.Values{}
	data.Set("services", services.String())

	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	return c.doPost("/node/controller/setupServices", []byte(data.Encode()), nil, headers)
}

func (c *Couchbase) setWebSettings(username, password string, port int) error {
	data := url.Values{}
	data.Set("username", username)
	data.Set("password", password)
	data.Set("port", strconv.Itoa(port))

	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	return c.doPost("/settings/web", []byte(data.Encode()), nil, headers)
}

func (c *Couchbase) ClusterInitialize(username, password string, defaults *PoolsDefaults, port int, services []ServiceName, mode IndexStorageMode) error {
	if err := c.SetPoolsDefault(defaults); err != nil {
		return err
	}

	settings, err := c.GetIndexSettings()
	if err != nil {
		return err
	}

	settings.StorageMode = mode

	if err := c.SetIndexSettings(settings); err != nil {
		return err
	}

	if err := c.setServices(services); err != nil {
		return err
	}

	if err := c.setWebSettings(username, password, port); err != nil {
		return err
	}

	return nil
}

func (c *Couchbase) getPools() (*PoolsInfo, error) {
	info := &PoolsInfo{}

	err := c.doGet("/pools", info, c.defaultHeaders())
	if err != nil {
		return nil, err
	}

	return info, nil
}

func (c *Couchbase) getTasks() ([]*Task, error) {
	tasks := []*Task{}

	err := c.doGet("/pools/default/tasks", &tasks, c.defaultHeaders())
	if err != nil {
		return nil, err
	}

	return tasks, nil
}

func (c *Couchbase) ClusterUUID() (string, error) {
	info, err := c.getPools()
	if err != nil {
		return "", err
	}

	if uuid, ok := info.UUID.(string); ok {
		return uuid, nil
	}

	return "", nil
}

func (c *Couchbase) IsEnterprise() (bool, error) {
	info, err := c.getPools()
	if err != nil {
		return false, err
	}

	return info.Enterprise, nil
}

func (c *Couchbase) setHostname(hostname string) error {
	data := url.Values{}
	data.Set("hostname", hostname)

	headers := c.defaultHeaders()
	headers.Set("Content-Type", ContentTypeURLEncoded)

	return c.doPost("/node/controller/rename", []byte(data.Encode()), nil, headers)
}

func (c *Couchbase) setStoragePaths(dataPath, indexPath string, analyticsPaths []string) error {
	data := url.Values{}
	data.Set("path", dataPath)
	data.Set("index_path", indexPath)

	if len(analyticsPaths) > 0 {
		data.Set("cbas_path", analyticsPaths[0])

		for _, path := range analyticsPaths[1:] {
			data.Add("cbas_path", path)
		}
	}

	headers := c.defaultHeaders()
	headers.Set("Content-Type", ContentTypeURLEncoded)

	return c.doPost("/nodes/self/controller/settings", []byte(data.Encode()), nil, headers)
}

func (c *Couchbase) NodeInitialize(hostname, dataPath, indexPath string, analyticsPaths []string) error {
	if err := c.setHostname(hostname); err != nil {
		return err
	}

	if err := c.setStoragePaths(dataPath, indexPath, analyticsPaths); err != nil {
		return err
	}

	return nil
}

func (c *Couchbase) Rebalance(nodesToRemove []string) error {
	cluster, err := c.ClusterInfo()
	if err != nil {
		return err
	}

	all := []string{}
	eject := []string{}

	for _, node := range cluster.Nodes {
		all = append(all, node.OTPNode)

		for _, toRemove := range nodesToRemove {
			if node.HostName == toRemove {
				eject = append(eject, node.OTPNode)
			}
		}
	}

	data := url.Values{}
	data.Set("ejectedNodes", strings.Join(eject, ","))
	data.Set("knownNodes", strings.Join(all, ","))

	headers := c.defaultHeaders()
	headers.Set("Content-Type", ContentTypeURLEncoded)

	return c.doPost("/controller/rebalance", []byte(data.Encode()), nil, headers)
}

// Compare status of rebalance with an expected status.
func (c *Couchbase) CompareRebalanceStatus(expectedStatus RebalanceStatus) (bool, error) {
	task, err := c.getRebalanceTask()
	if err != nil {
		return false, err
	}

	return getRebalanceStatus(task) == expectedStatus, nil
}

func (c *Couchbase) StopRebalance() error {
	data := url.Values{}
	data.Set("allowUnsafe", "true")

	headers := c.defaultHeaders()
	headers.Set("Content-Type", ContentTypeURLEncoded)

	return c.doPost("/controller/stopRebalance", []byte(data.Encode()), nil, headers)
}

func (c *Couchbase) Failover(nodesToRemove []string) error {
	cluster, err := c.ClusterInfo()
	if err != nil {
		return err
	}

	otpNodes := []string{}

	for _, nodeToRemove := range nodesToRemove {
		for _, node := range cluster.Nodes {
			if node.HostName == nodeToRemove {
				otpNodes = append(otpNodes, node.OTPNode)
				break
			}
		}
	}

	if len(otpNodes) != len(nodesToRemove) {
		return fmt.Errorf("unable to find nodes to failover")
	}

	data := url.Values{}

	for _, otpNode := range otpNodes {
		data.Add("otpNode", otpNode)
	}

	headers := c.defaultHeaders()
	headers.Set("Content-Type", ContentTypeURLEncoded)

	return c.doPost("/controller/failOver", []byte(data.Encode()), nil, headers)
}

func (c *Couchbase) CreateBucket(bucket *Bucket) error {
	params := bucket.FormEncode()

	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	return c.doPost("/pools/default/buckets", params, nil, headers)
}

func (c *Couchbase) DeleteBucket(name string) error {
	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	path := "/pools/default/buckets/" + name

	return c.doDelete(path, headers)
}

func (c *Couchbase) EditBucket(bucket *Bucket) error {
	// bucket params cannot include conflict resolution field
	// during edit.
	bucket.ConflictResolution = ""

	params := bucket.FormEncode()

	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	return c.doPost("/pools/default/buckets/"+bucket.BucketName, params, nil, headers)
}

func (c *Couchbase) getBucketStatus(name string) (*BucketStatus, error) {
	status := &BucketStatus{}
	path := "/pools/default/buckets/" + name

	if err := c.doGet(path, status, c.defaultHeaders()); err != nil {
		return nil, err
	}

	return status, nil
}

// Determine wether bucket is ready based on status resolving
// to healthy across all nodes.
func (c *Couchbase) BucketReady(name string) (bool, error) {
	status, err := c.getBucketStatus(name)
	if err != nil {
		return false, err
	}

	// check bucket health on all nodes
	if len(status.Nodes) == 0 {
		return false, NewErrorBucketNotReady(name, "creation pending")
	}

	for _, node := range status.Nodes {
		if node.Status != "healthy" {
			return false, NewErrorBucketNotReady(name, node.Status)
		}
	}

	return true, nil
}

func (c *Couchbase) GetBucketStatus(name string) (*BucketStatus, error) {
	status, err := c.getBucketStatus(name)
	if err != nil {
		return nil, err
	}

	return status, nil
}

func (c *Couchbase) GetBuckets() ([]Bucket, error) {
	buckets := []Bucket{}
	path := "/pools/default/buckets/"

	err := c.doGet(path, &buckets, c.defaultHeaders())
	if err != nil {
		return nil, err
	}

	return buckets, nil
}

func (c *Couchbase) GetBucket(bucketName string) (*Bucket, error) {
	bucketList, err := c.GetBuckets()
	if err != nil {
		return nil, err
	}

	for i := range bucketList {
		bucket := bucketList[i]

		if bucket.BucketName == bucketName {
			return &bucket, nil
		}
	}

	return nil, fmt.Errorf("no such bucket: %s", bucketName)
}

func (c *Couchbase) SetAutoFailoverSettings(settings *AutoFailoverSettings) error {
	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	data, err := urlencoding.Marshal(settings)
	if err != nil {
		return err
	}

	return c.doPost("/settings/autoFailover", data, nil, headers)
}

func (c *Couchbase) GetAutoFailoverSettings() (*AutoFailoverSettings, error) {
	settings := &AutoFailoverSettings{}

	if err := c.doGet("/settings/autoFailover", settings, c.defaultHeaders()); err != nil {
		return nil, err
	}

	return settings, nil
}

func (c *Couchbase) GetIndexSettings() (*IndexSettings, error) {
	settings := &IndexSettings{}

	err := c.doGet("/settings/indexes", settings, c.defaultHeaders())
	if err != nil {
		return nil, err
	}

	return settings, nil
}

func (c *Couchbase) SetIndexSettings(settings *IndexSettings) error {
	data := url.Values{}
	data.Set("storageMode", string(settings.StorageMode))
	data.Set("indexerThreads", strconv.Itoa(settings.Threads))
	data.Set("memorySnapshotInterval", strconv.Itoa(settings.MemSnapInterval))
	data.Set("stableSnapshotInterval", strconv.Itoa(settings.StableSnapInterval))
	data.Set("maxRollbackPoints", strconv.Itoa(settings.MaxRollbackPoints))
	data.Set("logLevel", string(settings.LogLevel))

	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	return c.doPost("/settings/indexes", []byte(data.Encode()), nil, headers)
}

func (c *Couchbase) getNodeInfo() (*NodeInfo, error) {
	node := &NodeInfo{}

	if err := c.doGet("/nodes/self", node, c.defaultHeaders()); err != nil {
		return nil, err
	}

	return node, nil
}

func (c *Couchbase) UploadClusterCACert(pem []byte) error {
	headers := c.defaultHeaders()
	return c.doPost("/controller/uploadClusterCA", pem, nil, headers)
}

func (c *Couchbase) ReloadNodeCert() error {
	headers := c.defaultHeaders()
	return c.doPost("/node/controller/reloadCertificate", []byte{}, nil, headers)
}

func (c *Couchbase) GetClusterCACert() ([]byte, error) {
	var cert string

	err := c.doGet("/pools/default/certificate", &cert, c.defaultHeaders())

	return []byte(cert), err
}

func (c *Couchbase) GetClientCertAuth() (*ClientCertAuth, error) {
	clientAuth := &ClientCertAuth{}
	if err := c.doGet("/settings/clientCertAuth", clientAuth, c.defaultHeaders()); err != nil {
		return nil, err
	}

	return clientAuth, nil
}

func (c *Couchbase) SetClientCertAuth(settings *ClientCertAuth) error {
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}

	headers := c.defaultHeaders()

	return c.doPost("/settings/clientCertAuth", data, nil, headers)
}

func (c *Couchbase) GetUpdatesEnabled() (bool, error) {
	settingsStats := &SettingsStats{}
	if err := c.doGet("/settings/stats", settingsStats, c.defaultHeaders()); err != nil {
		return false, err
	}

	return settingsStats.SendStats, nil
}

func (c *Couchbase) SetUpdatesEnabled(enabled bool) error {
	data := url.Values{}
	data.Set("sendStats", BoolAsStr(enabled))

	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	return c.doPost("/settings/stats", []byte(data.Encode()), nil, headers)
}

func (c *Couchbase) GetAlternateAddressesExternal() (*AlternateAddressesExternal, error) {
	nodeServices := &NodeServices{}
	if err := c.doGet("/pools/default/nodeServices", nodeServices, c.defaultHeaders()); err != nil {
		return nil, err
	}

	for _, node := range nodeServices.NodesExt {
		if !node.ThisNode {
			continue
		}

		if node.AlternateAddresses == nil {
			return nil, nil
		}

		return node.AlternateAddresses.External, nil
	}

	// The absence of this node is probably due to it not being balanced in yet.
	// /pools/default/nodeServices apparently only shows nodes when the rebalance
	// starts.  Don't raise an error.
	return nil, nil
}

func (c *Couchbase) SetAlternateAddressesExternal(addresses *AlternateAddressesExternal) error {
	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	data, err := urlencoding.Marshal(addresses)
	if err != nil {
		return err
	}

	return c.doPut("/node/controller/setupAlternateAddresses/external", data, headers)
}

func (c *Couchbase) DeleteAlternateAddressesExternal() error {
	headers := c.defaultHeaders()
	return c.doDelete("/node/controller/setupAlternateAddresses/external", headers)
}

func (c *Couchbase) GetServerGroups() (*ServerGroups, error) {
	serverGroups := &ServerGroups{}
	if err := c.doGet("/pools/default/serverGroups", serverGroups, c.defaultHeaders()); err != nil {
		return nil, err
	}

	return serverGroups, nil
}

func (c *Couchbase) CreateServerGroup(name string) error {
	data := url.Values{}
	data.Set("name", name)

	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	return c.doPost("/pools/default/serverGroups", []byte(data.Encode()), nil, headers)
}

func (c *Couchbase) UpdateServerGroups(revision string, groups *ServerGroupsUpdate) error {
	data, err := json.Marshal(groups)
	if err != nil {
		return err
	}

	uri := "/pools/default/serverGroups?rev=" + revision

	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeJSON)

	return c.doPut(uri, data, headers)
}

func (c *Couchbase) SetRecoveryType(hostname string, recoveryType RecoveryType) error {
	otpNode, err := c.getOTPNode(hostname)
	if err != nil {
		return err
	}

	data := url.Values{}
	data.Set("otpNode", otpNode)
	data.Set("recoveryType", string(recoveryType))

	headers := c.defaultHeaders()
	headers.Set("Content-Type", ContentTypeURLEncoded)

	return c.doPost("/controller/setRecoveryType", []byte(data.Encode()), nil, headers)
}

func (c *Couchbase) GetAutoCompactionSettings() (*AutoCompactionSettings, error) {
	r := &AutoCompactionSettings{}
	if err := c.doGet("/settings/autoCompaction", r, c.defaultHeaders()); err != nil {
		return nil, err
	}

	return r, nil
}

func (c *Couchbase) SetAutoCompactionSettings(r *AutoCompactionSettings) error {
	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	data, err := urlencoding.Marshal(r)
	if err != nil {
		return err
	}

	return c.doPost("/controller/setAutoCompaction", data, nil, headers)
}

func (c *Couchbase) ListRemoteClusters() (RemoteClusters, error) {
	r := RemoteClusters{}
	if err := c.doGet("/pools/default/remoteClusters", &r, c.defaultHeaders()); err != nil {
		return nil, err
	}

	// God only knows what this means, but lets assume we discard things
	// that are "deleted".
	filtered := RemoteClusters{}

	for _, cluster := range r {
		if !cluster.Deleted {
			filtered = append(filtered, cluster)
		}
	}

	return filtered, nil
}

func (c *Couchbase) CreateRemoteCluster(r *RemoteCluster) error {
	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	data, err := urlencoding.Marshal(r)
	if err != nil {
		return err
	}

	return c.doPost("/pools/default/remoteClusters", data, nil, headers)
}

func (c *Couchbase) DeleteRemoteCluster(r *RemoteCluster) error {
	return c.doDelete("/pools/default/remoteClusters/"+r.Name, c.defaultHeaders())
}

// getRemoteClusterByUUID helps manage the utter horror show that is XDCR
// replications.
func (c *Couchbase) getRemoteClusterByUUID(uuid string) (*RemoteCluster, error) {
	clusters, err := c.ListRemoteClusters()
	if err != nil {
		return nil, err
	}

	for _, cluster := range clusters {
		if cluster.UUID == uuid {
			return &cluster, nil
		}
	}

	return nil, fmt.Errorf("lookupClusterForUUID: no cluster found for uuid %v", uuid)
}

// getRemoteClusterByName helps manage the utter horror show that is XDCR
// replications.
func (c *Couchbase) getRemoteClusterByName(name string) (*RemoteCluster, error) {
	clusters, err := c.ListRemoteClusters()
	if err != nil {
		return nil, err
	}

	for _, cluster := range clusters {
		if cluster.Name == name {
			return &cluster, nil
		}
	}

	return nil, fmt.Errorf("lookupUUIDForCluster: no cluster found for name %v", name)
}

// getReplicationSettings helps manage the utter horror show that is XDCR
// replications.
func (c *Couchbase) getReplicationSettings(uuid, from, to string) (*ReplicationSettings, error) {
	s := &ReplicationSettings{}
	if err := c.doGet("/settings/replications/"+url.PathEscape(uuid+"/"+from+"/"+to), s, c.defaultHeaders()); err != nil {
		return nil, err
	}

	return s, nil
}

// ListReplications lists all replications in the cluster.  To make the Operator
// code a million times simpler we do a lot of post processing and table joins
// just to recover the same information used to create a replication.
func (c *Couchbase) ListReplications() ([]Replication, error) {
	tasks, err := c.getXDCRTasks()
	if err != nil {
		return nil, err
	}

	replications := []Replication{}

	for _, task := range tasks {
		// Parse the target to recover lost information.
		// Should be in the form /remoteClusters/c4c9af9ad62d8b5f665edac5ffc9c1be/buckets/default
		if task.Target == "" {
			return nil, fmt.Errorf("listReplications: target not populated")
		}

		parts := strings.Split(task.Target, "/")
		if len(parts) != 5 {
			return nil, fmt.Errorf("listReplications: target incorrectly formatted: %v", task.Target)
		}

		uuid := parts[2]
		to := parts[4]

		// Lookup the UUID to recover the cluster name.
		cluster, err := c.getRemoteClusterByUUID(uuid)
		if err != nil {
			return nil, err
		}

		// Lookup the settings to recover the compression type.
		settings, err := c.getReplicationSettings(uuid, task.Source, to)
		if err != nil {
			return nil, err
		}

		// By now your eyeballs will be dry from all the rolling they are doing.
		replications = append(replications, Replication{
			FromBucket:       task.Source,
			ToCluster:        cluster.Name,
			ToBucket:         to,
			Type:             task.ReplicationType,
			ReplicationType:  "continuous",
			CompressionType:  settings.CompressionType,
			FilterExpression: task.FilterExpression,
			PauseRequested:   settings.PauseRequested,
		})
	}

	return replications, nil
}

// CreateReplication creates an XDCR replication between clusters.
func (c *Couchbase) CreateReplication(r *Replication) error {
	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	data, err := urlencoding.Marshal(r)
	if err != nil {
		return err
	}

	return c.doPost("/controller/createReplication", data, nil, headers)
}

// UpdateReplication updates the parts of an XDCR replication that can be updated.
func (c *Couchbase) UpdateReplication(r *Replication) error {
	cluster, err := c.getRemoteClusterByName(r.ToCluster)
	if err != nil {
		return err
	}

	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	data, err := urlencoding.Marshal(r)
	if err != nil {
		return err
	}

	return c.doPost("/settings/replications/"+url.PathEscape(cluster.UUID+"/"+r.FromBucket+"/"+r.ToBucket), data, nil, headers)
}

// DeleteReplication deletes an existing XDCR replication between clusters.
func (c *Couchbase) DeleteReplication(r *Replication) error {
	cluster, err := c.getRemoteClusterByName(r.ToCluster)
	if err != nil {
		return err
	}

	// WHAT IS THIS MADNESS?!??!?!?!?!??!
	return c.doDelete("/controller/cancelXDCR/"+url.PathEscape(cluster.UUID+"/"+r.FromBucket+"/"+r.ToBucket), c.defaultHeaders())
}

func (c *Couchbase) GetUsers() ([]*User, error) {
	users := []*User{}
	path := "/settings/rbac/users"

	if err := c.doGet(path, &users, c.defaultHeaders()); err != nil {
		return nil, err
	}

	return users, nil
}

func (c *Couchbase) CreateUser(user *User) error {
	params := user.FormEncode()

	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	path := strings.Join([]string{"/settings/rbac/users", string(user.Domain), user.ID}, "/")

	return c.doPut(path, params, headers)
}

func (c *Couchbase) DeleteUser(user *User) error {
	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	path := strings.Join([]string{"/settings/rbac/users", string(user.Domain), user.ID}, "/")

	return c.doDelete(path, headers)
}

func (c *Couchbase) GetUser(id string, domain AuthDomain) (*User, error) {
	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	user := &User{}
	path := strings.Join([]string{"/settings/rbac/users", string(domain), id}, "/")

	if err := c.doGet(path, user, c.defaultHeaders()); err != nil {
		return nil, err
	}

	return user, nil
}

func (c *Couchbase) GetGroups() ([]*Group, error) {
	groups := []*Group{}
	path := "/settings/rbac/groups"

	if err := c.doGet(path, &groups, c.defaultHeaders()); err != nil {
		return nil, err
	}

	return groups, nil
}

func (c *Couchbase) CreateGroup(group *Group) error {
	roles := RolesToStr(group.Roles)

	data := url.Values{}
	data.Set("roles", strings.Join(roles, ","))
	data.Set("description", group.Description)
	data.Set("ldap_group_ref", group.LDAPGroupRef)

	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	path := "/settings/rbac/groups/" + group.ID

	return c.doPut(path, []byte(data.Encode()), headers)
}

func (c *Couchbase) DeleteGroup(group *Group) error {
	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	path := "/settings/rbac/groups/" + group.ID

	return c.doDelete(path, headers)
}

func (c *Couchbase) GetGroup(id string) (*Group, error) {
	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	path := "/settings/rbac/groups/" + id
	group := &Group{}

	if err := c.doGet(path, group, c.defaultHeaders()); err != nil {
		return nil, err
	}

	return group, nil
}

func (c *Couchbase) GetLDAPSettings() (*LDAPSettings, error) {
	settings := &LDAPSettings{}

	if err := c.doGet("/settings/ldap", settings, c.defaultHeaders()); err != nil {
		return nil, err
	}

	return settings, nil
}

func (c *Couchbase) SetLDAPSettings(settings *LDAPSettings) error {
	params, err := settings.FormEncode()
	if err != nil {
		return err
	}

	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	return c.doPost("/settings/ldap", params, nil, headers)
}

func (c *Couchbase) GetLDAPConnectivityStatus() (*LDAPStatus, error) {
	data := url.Values{}

	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeJSON)

	status := &LDAPStatus{}

	if err := c.doPost("/settings/ldap/validate/connectivity", []byte(data.Encode()), status, headers); err != nil {
		return nil, err
	}

	return status, nil
}

func (c *Couchbase) GetSecuritySettings() (*SecuritySettings, error) {
	s := &SecuritySettings{}

	if err := c.doGet("/settings/security", s, c.defaultHeaders()); err != nil {
		return nil, err
	}

	return s, nil
}

func (c *Couchbase) SetSecuritySettings(s *SecuritySettings) error {
	data, err := urlencoding.Marshal(s)
	if err != nil {
		return err
	}

	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	return c.doPost("/settings/security", data, nil, headers)
}

func (c *Couchbase) GetNodeNetworkConfiguration() (*NodeNetworkConfiguration, error) {
	// And yet again, the CRUD is completely ignored!
	node, err := c.getNodeInfo()
	if err != nil {
		return nil, err
	}

	onOrOff := Off

	if node.NodeEncryption {
		onOrOff = On
	}

	s := &NodeNetworkConfiguration{
		NodeEncryption: onOrOff,
	}

	return s, nil
}

func (c *Couchbase) SetNodeNetworkConfiguration(s *NodeNetworkConfiguration) error {
	data, err := urlencoding.Marshal(s)
	if err != nil {
		return err
	}

	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	return c.doPost("/node/controller/setupNetConfig", data, nil, headers)
}

func (c *Couchbase) EnableExternalListener(s *NodeNetworkConfiguration) error {
	data, err := urlencoding.Marshal(s)
	if err != nil {
		return err
	}

	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	return c.doPost("/node/controller/enableExternalListener", data, nil, headers)
}

func (c *Couchbase) DisableExternalListener(s *NodeNetworkConfiguration) error {
	data, err := urlencoding.Marshal(s)
	if err != nil {
		return err
	}

	headers := c.defaultHeaders()
	headers.Set(HeaderContentType, ContentTypeURLEncoded)

	return c.doPost("/node/controller/disableExternalListener", data, nil, headers)
}
