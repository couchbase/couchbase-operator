/*
Copyright 2020-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

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

	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/pkg/util/urlencoding"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("api")

var (
	ErrTypeError   = fmt.Errorf("unsupported type")
	ErrMemberError = fmt.Errorf("member error")
)

// Certificate and key used by TLS client authentication.
type TLSClientAuth struct {
	// PEM encoded certificate
	Cert []byte

	// PEM encoded private key
	Key []byte
}

// TLS Authentication parameters.
type TLSAuth struct {
	// PEM encoded CA certificate
	CACert []byte

	// RootCAs allows a pool of CAs to be used to verify when
	// we have multiple specified, and have no idea which to use.
	RootCAs [][]byte

	// Optional client authentication
	ClientAuth *TLSClientAuth
}

// Client is a structure which encapsulates HTTP API access to a
// Client cluster.
type Client struct {
	// username is used in basic HTTP authorization
	username string

	// password is used in basic HTTP authorization
	password string

	// tls, if set, specifies the client certificate chain and private keys
	// for mutual verification.  It also contains at least a CA certificate
	// to authenticate the server is trustworthy
	tls *TLSAuth

	// Client is a persistent connection pool to be used by all endpoints
	// associated with this connection context.  It will become invalid
	// if any parameters used in the TLS handshake, or HTTP UUID check
	// are updated.
	// We export it to allow for mocking.
	Client HTTPClient

	// ctx is the context used to cancel requests.
	ctx context.Context

	// cluster is the cluster name for logging.
	cluster string
}

// New creates a new Client HTTP(S) API client and initializes the
// HTTP connection pool.
func New(ctx context.Context, cluster, username, password string) *Client {
	c := &Client{
		ctx:      ctx,
		cluster:  cluster,
		username: username,
		password: password,
	}

	c.makeClient()

	return c
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
	CloseIdleConnections()
}

func (c *Client) SetPassword(password string) {
	c.password = password
}

// SetTLS updates the client TLS settings.  Creates a new client object to
// flush existing persistent connections.
func (c *Client) SetTLS(tls *TLSAuth) {
	c.tls = tls
	c.makeClient()
}

// GetTLS returns the TLS configuration in use by the c.
func (c *Client) GetTLS() *TLSAuth {
	return c.tls
}

// CloseIdleConnections forces all TCP sessions with Client to terminate, as
// it will sometime prioritize keeping a client happy rather than being in the
// correct state.
func (c *Client) CloseIdleConnections() {
	c.Client.CloseIdleConnections()
}

// RequestMethod is a common HTTP call.
type RequestMethod func(*Client, *Request, string) error

// Request is a one time request against the Client API.
type Request struct {
	// method is the API routine to use, analogous to GET, POST etc.
	method RequestMethod

	// host is the function used to extract the HTTP host URL from a
	// member.  If nil this defaults to ClientURL.
	Host func(Member) string

	// path is the HTTP path to call.
	Path string

	// body is the request body.
	Body []byte

	// result is the unmarshalled response body.
	Result interface{}

	// timeout is the duration to allow retries for.
	timeout time.Duration

	// err records any errors during the request build process.
	err error

	// Authenticate sends the admin username/password.
	Authenticate bool
}

// NewRequest is a terse way of creating an API request.
func NewRequest(method RequestMethod, path string, body []byte, result interface{}) *Request {
	return &Request{
		method:       method,
		Path:         path,
		Body:         body,
		Result:       result,
		Authenticate: true,
	}
}

// NewRequestError is how to return an error in a builder chain.  It will be picked
// up by Do and reported then.
func NewRequestError(err error) *Request {
	return &Request{
		err: err,
	}
}

// InPlaintext forces the API requests to be in plaintext.  This is used
// for initial calls to server before TLS is configured, and therefore
// we inhibit the passing of admin credentials to avoid a leak.  If you ever
// need to change this, think long and hard about your life.
func (r *Request) InPlaintext() *Request {
	r.Host = Member.GetHostURLPlaintext
	r.Authenticate = false

	return r
}

// RetryFor repeats the API call for a certain amount of time until it
// succeeds.
func (r *Request) RetryFor(timeout time.Duration) *Request {
	r.timeout = timeout
	return r
}

// On executes the API call request against the given hosts.
// The targets may be a member set, a member or an explicit string.
func (r *Request) On(client *Client, target interface{}) error {
	// Something bad happened during request building, most likely
	// marshalling data.
	if r.err != nil {
		return r.err
	}

	// Set a default address resolution function if none is specified.
	if r.Host == nil {
		r.Host = Member.GetHostURL
	}

	// Translate the specified targets into a list of hosts.
	hosts := []string{}

	switch t := target.(type) {
	case MemberSet:
		for _, member := range t {
			hosts = append(hosts, r.Host(member))
		}
	case Member:
		hosts = append(hosts, r.Host(t))
	case string:
		hosts = append(hosts, t)
	default:
		return fmt.Errorf("%w: unknown target type %v", errors.NewStackTracedError(ErrTypeError), t)
	}

	// Generate a closure to iterate over the hosts trying the call.
	callMembers := func(c *Client, r *Request) error {
		lastError := errors.NewStackTracedError(ErrMemberError)

		for _, host := range hosts {
			if err := r.method(c, r, host); err != nil {
				lastError = err
				continue
			}

			return nil
		}

		return lastError
	}

	call := callMembers

	// Optionally generate another closure to retry the call for a specified
	// amount of time.
	if r.timeout != 0 {
		retryCallMembers := func(c *Client, r *Request) error {
			callback := func() error {
				return callMembers(c, r)
			}

			return retryutil.RetryFor(r.timeout, callback)
		}

		call = retryCallMembers
	}

	return call(client, r)
}

// AddNode adds a new node to an existing Client cluster.
func AddNode(hostname, username, password string, services fmt.Stringer) *Request {
	data := url.Values{}
	data.Set("hostname", hostname)
	data.Set("user", username)
	data.Set("password", password)
	data.Set("services", services.String())

	return NewRequest((*Client).Post, "/controller/addNode", []byte(data.Encode()), nil)
}

// CancelAddNode stops Client server from from attempting to add a node to
// the cluster.
func CancelAddNode(otpNode OTPNode) *Request {
	data := url.Values{}
	data.Set("otpNode", string(otpNode))

	return NewRequest((*Client).Post, "/controller/ejectNode", []byte(data.Encode()), nil)
}

// GetPoolsDefault returns the default cluster pool information.
// This is the dumping ground for most cluster information.
func GetPoolsDefault(clusterInfo *ClusterInfo) *Request {
	return NewRequest((*Client).Get, "/pools/default", nil, clusterInfo)
}

// SetPoolsDefault sets cluster wide configuration, and probably needs a proper
// name rather than going off what server says.
func SetPoolsDefault(defaults *PoolsDefaults) *Request {
	data, err := urlencoding.Marshal(defaults)
	if err != nil {
		return NewRequestError(err)
	}

	return NewRequest((*Client).Post, "/pools/default", data, nil)
}

// SetServices initializes the node with the set of services we expect it to be
// runnning.
func SetServices(services ServiceList) *Request {
	data := url.Values{}
	data.Set("services", services.String())

	return NewRequest((*Client).Post, "/node/controller/setupServices", []byte(data.Encode()), nil)
}

// SetWebSettings actually sets a username and password.
func SetWebSettings(username, password string, port int) *Request {
	data := url.Values{}
	data.Set("username", username)
	data.Set("password", password)
	data.Set("port", strconv.Itoa(port))

	return NewRequest((*Client).Post, "/settings/web", []byte(data.Encode()), nil)
}

// GetPools returns information about all cluster pools, this is
// just information about Client server and the cluster in general.
func GetPools(info *PoolsInfo) *Request {
	return NewRequest((*Client).Get, "/pools", nil, info)
}

// ListTasks lists all asynchronous tasks on the server.
func ListTasks(tasks *TaskList) *Request {
	return NewRequest((*Client).Get, "/pools/default/tasks", nil, tasks)
}

// SetHostname sets the hostname of a node.
func SetHostname(hostname string) *Request {
	data := url.Values{}
	data.Set("hostname", hostname)

	return NewRequest((*Client).Post, "/node/controller/rename", []byte(data.Encode()), nil)
}

// SetStoragePaths sets the storage paths of a node.
func SetStoragePaths(dataPath, indexPath string, analyticsPaths []string) *Request {
	data := url.Values{}
	data.Set("path", dataPath)
	data.Set("index_path", indexPath)

	for _, path := range analyticsPaths {
		data.Add("cbas_path", path)
	}

	return NewRequest((*Client).Post, "/nodes/self/controller/settings", []byte(data.Encode()), nil)
}

// Rebalance starts a rebalance on a node.
func Rebalance(known, eject OTPNodeList) *Request {
	data := url.Values{}
	data.Set("ejectedNodes", strings.Join(eject.StringSlice(), ","))
	data.Set("knownNodes", strings.Join(known.StringSlice(), ","))

	return NewRequest((*Client).Post, "/controller/rebalance", []byte(data.Encode()), nil)
}

// StopRebalance attempts to stop an in-progress rebalance.
func StopRebalance() *Request {
	data := url.Values{}
	data.Set("allowUnsafe", "true")

	return NewRequest((*Client).Post, "/controller/stopRebalance", []byte(data.Encode()), nil)
}

// CreateBucket creates a new bucket.
func CreateBucket(bucket *Bucket) *Request {
	params := bucket.FormEncode(false)

	return NewRequest((*Client).Post, "/pools/default/buckets", params, nil)
}

// DeleteBucket deletes an existing bucket.
func DeleteBucket(name string) *Request {
	return NewRequest((*Client).Delete, "/pools/default/buckets/"+name, nil, nil)
}

// UpdateBucket updates an existing bucket.
func UpdateBucket(bucket *Bucket) *Request {
	params := bucket.FormEncode(true)

	return NewRequest((*Client).Post, "/pools/default/buckets/"+bucket.BucketName, params, nil)
}

// ListBuckets lists all buckets on the system.
func ListBuckets(buckets *BucketList) *Request {
	return NewRequest((*Client).Get, "/pools/default/buckets/", nil, buckets)
}

// ListBucketStatuses lists all bucket statuses on the system.
func ListBucketStatuses(buckets *BucketStatusList) *Request {
	return NewRequest((*Client).Get, "/pools/default/buckets/", nil, buckets)
}

// SetAutoFailoverSettings sets auto failover settings.
func SetAutoFailoverSettings(settings *AutoFailoverSettings) *Request {
	data, err := urlencoding.Marshal(settings)
	if err != nil {
		return NewRequestError(err)
	}

	return NewRequest((*Client).Post, "/settings/autoFailover", data, nil)
}

// GetAutoFailoverSettings gets auto failover settings.
func GetAutoFailoverSettings(settings *AutoFailoverSettings) *Request {
	return NewRequest((*Client).Get, "/settings/autoFailover", nil, settings)
}

// GetIndexSettings gets index settings.
func GetIndexSettings(settings *IndexSettings) *Request {
	return NewRequest((*Client).Get, "/settings/indexes", nil, settings)
}

// SetIndexSettings sets index settings.
func SetIndexSettings(settings *IndexSettings) *Request {
	data, err := urlencoding.Marshal(settings)
	if err != nil {
		return NewRequestError(err)
	}

	return NewRequest((*Client).Post, "/settings/indexes", data, nil)
}

// GetAuditSettings retrieves the audit settings, shockingly.
func GetAuditSettings(settings *AuditSettings) *Request {
	return NewRequest((*Client).Get, "/settings/audit", nil, settings)
}

// SetAuditSettings sets audit settings.
func SetAuditSettings(settings AuditSettings) *Request {
	// Audit REST API is a PITA so we use a custom marshalling method to cope with it.
	// It requires the use of an empty key, not an empty array for the disabled settings if you want to disable all ids/users.
	// This fails, the default JSON encoding for an empty array without omit empty (which just removes the key entirely):
	// curl ... settings/audit \
	// 	-d auditdEnabled=true \
	// 	-d rotateSize=1024 \
	// 	-d disabled="[]" | jq
	// {
	// 	"errors": {
	// 		"disabled": "All event id's must be integers"
	// 	}
	// }
	// This is correct:
	// 	curl ... settings/audit \
	// 		-d auditdEnabled=true \
	// 		-d rotateSize=1024 \
	// 		-d disabled= | jq
	// Similarly the arrays have to be non-JSON ones and CSVs so no [] delimiters
	// For users it is even worse, the POST request requires a CSV string but the GET response provides an array of structs:
	// curl ... POST ... \
	// -d disabledUsers=@eventing/local
	// curl ... GET ...
	// {..."disabledUsers":[{"name":"@eventing","domain":"local"}],...}
	data := url.Values{}
	data.Set("auditdEnabled", BoolAsStr(settings.Enabled))
	data.Set("logPath", settings.LogPath)
	data.Set("rotateInterval", IntToStr(settings.RotateInterval))
	data.Set("rotateSize", IntToStr(settings.RotateSize))

	// Deal with the non-standard CSV nonsense required by the audit REST API
	eventsCSV := ""
	for _, i := range settings.DisabledEvents {
		if eventsCSV != "" {
			eventsCSV += ","
		}

		eventsCSV += strconv.Itoa(i)
	}

	data.Set("disabled", eventsCSV)

	// And now the custom struct array to a CSV
	usersCSV := ""
	for _, u := range settings.DisabledUsers {
		if usersCSV != "" {
			usersCSV += ","
		}

		usersCSV += u.Name + "/" + u.Domain
	}

	data.Set("disabledUsers", usersCSV)

	return NewRequest((*Client).Post, "/settings/audit", []byte(data.Encode()), nil)
}

// GetNodesSelf returns information about the called node.
func GetNodesSelf(node *NodeInfo) *Request {
	return NewRequest((*Client).Get, "/nodes/self", nil, node)
}

// GetClusterCACert gets the cluster CA certificate.
func GetClusterCACert(certificate *[]byte) *Request {
	return NewRequest((*Client).Get, "/pools/default/certificate", nil, certificate)
}

// SetClusterCACert sets the cluster CA certificate.
func SetClusterCACert(certificate []byte) *Request {
	return NewRequest((*Client).PostNoContentType, "/controller/uploadClusterCA", certificate, nil)
}

// LoadCAs loads all CAs in the inbox (7.1+ only).
func LoadCAs() *Request {
	return NewRequest((*Client).PostNoContentType, "/node/controller/loadTrustedCAs", []byte{}, nil)
}

// ListCAs lists all installed CAs (7.1+ only).
func ListCAs(ca *TrustedCAList) *Request {
	return NewRequest((*Client).Get, "/pools/default/trustedCAs", nil, ca)
}

// DeleteCA removes the requested CA from the cluster (7.1+ only).
func DeleteCA(id int) *Request {
	return NewRequest((*Client).Delete, fmt.Sprintf("/pools/default/trustedCAs/%d", id), nil, nil)
}

// ReloadNodeCert causes server to reload the server certificate chain and key from disk.
func ReloadNodeCert(settings *PrivateKeyPassphraseSettings) *Request {
	data := []byte{}

	if settings != nil {
		var err error

		data, err = json.Marshal(settings)
		if err != nil {
			return NewRequestError(err)
		}
	}

	return NewRequest((*Client).PostJSON, "/node/controller/reloadCertificate", data, nil)
}

// GetClientCertAuth gets client certificate authentication settings.
func GetClientCertAuth(cAuth *ClientCertAuth) *Request {
	return NewRequest((*Client).Get, "/settings/clientCertAuth", nil, cAuth)
}

// SetClientCertAuth sets client certificate authentication settings.
func SetClientCertAuth(settings *ClientCertAuth) *Request {
	data, err := json.Marshal(settings)
	if err != nil {
		return NewRequestError(err)
	}

	return NewRequest((*Client).PostJSON, "/settings/clientCertAuth", data, nil)
}

// GetSettingsStats gets some settings to do with stats.
func GetSettingsStats(stats *SettingsStats) *Request {
	return NewRequest((*Client).Get, "/settings/stats", nil, stats)
}

// SetSettingsStats sets some settings to do with stats.
func SetSettingsStats(stats *SettingsStats) *Request {
	data, err := urlencoding.Marshal(stats)
	if err != nil {
		return NewRequestError(err)
	}

	return NewRequest((*Client).Post, "/settings/stats", data, nil)
}

// GetNodeServices lists service ports for all nodes in the system.
func GetNodeServices(nodeServices *NodeServices) *Request {
	return NewRequest((*Client).Get, "/pools/default/nodeServices", nil, nodeServices)
}

// SetAlternateAddressesExternal gets the alternate addresess for this node.
func SetAlternateAddressesExternal(addresses *AlternateAddressesExternal) *Request {
	data, err := urlencoding.Marshal(addresses)
	if err != nil {
		return NewRequestError(err)
	}

	return NewRequest((*Client).Put, "/node/controller/setupAlternateAddresses/external", data, nil)
}

// DeleteAlternateAddressesExternal deletes all alternate addresses for this node.
func DeleteAlternateAddressesExternal() *Request {
	return NewRequest((*Client).Delete, "/node/controller/setupAlternateAddresses/external", nil, nil)
}

// ListServerGroups lists all server groups on the system.
func ListServerGroups(serverGroups *ServerGroups) *Request {
	return NewRequest((*Client).Get, "/pools/default/serverGroups", nil, serverGroups)
}

// CreateServerGroup creates a new server group.
func CreateServerGroup(name string) *Request {
	data := url.Values{}
	data.Set("name", name)

	return NewRequest((*Client).Post, "/pools/default/serverGroups", []byte(data.Encode()), nil)
}

// UpdateServerGroups updates all server groups on the system.
func UpdateServerGroups(revision string, groups *ServerGroupsUpdate) *Request {
	data, err := json.Marshal(groups)
	if err != nil {
		return NewRequestError(err)
	}

	return NewRequest((*Client).PutJSON, fmt.Sprintf("/pools/default/serverGroups?rev=%s", revision), data, nil)
}

// SetRecoveryType sets the recovery type for a specific node.
func SetRecoveryType(otpNode OTPNode, recoveryType RecoveryType) *Request {
	data := url.Values{}
	data.Set("otpNode", string(otpNode))
	data.Set("recoveryType", string(recoveryType))

	return NewRequest((*Client).Post, "/controller/setRecoveryType", []byte(data.Encode()), nil)
}

// GetAutoCompactionSettings gets the auto compaction settings for the cluster.
func GetAutoCompactionSettings(r *AutoCompactionSettings) *Request {
	return NewRequest((*Client).Get, "/settings/autoCompaction", nil, r)
}

// SetAutoCompactionSettings sets the auto compaction settings for the cluster.
func SetAutoCompactionSettings(r *AutoCompactionSettings) *Request {
	data, err := urlencoding.Marshal(r)
	if err != nil {
		return NewRequestError(err)
	}

	return NewRequest((*Client).Post, "/controller/setAutoCompaction", data, nil)
}

// ListRemoteClusters lists all XDCR remote clusters.
func ListRemoteClusters(r *RemoteClusters) *Request {
	return NewRequest((*Client).Get, "/pools/default/remoteClusters", nil, r)
}

// CreateRemoteCluster creates a new XDCR remote cluster.
func CreateRemoteCluster(r *RemoteCluster) *Request {
	data, err := urlencoding.Marshal(r)
	if err != nil {
		return NewRequestError(err)
	}

	return NewRequest((*Client).Post, "/pools/default/remoteClusters", data, nil)
}

// UpdateRemoteCluster updates an existing XDCR remote cluster.
func UpdateRemoteCluster(r *RemoteCluster) *Request {
	data, err := urlencoding.Marshal(r)
	if err != nil {
		return NewRequestError(err)
	}

	return NewRequest((*Client).Post, fmt.Sprintf("/pools/default/remoteClusters/%s", r.Name), data, nil)
}

// DeleteRemoteCluster deletes an XDCR remote cluster.
func DeleteRemoteCluster(r *RemoteCluster) *Request {
	return NewRequest((*Client).Delete, fmt.Sprintf("/pools/default/remoteClusters/%s", r.Name), nil, nil)
}

// GetReplicationSettings helps manage the utter horror show that is XDCR
// replications.
func GetReplicationSettings(s *ReplicationSettings, uuid, from, to string) *Request {
	return NewRequest((*Client).Get, "/settings/replications/"+url.PathEscape(uuid+"/"+from+"/"+to), nil, s)
}

// CreateReplication creates an XDCR replication between clusters.
func CreateReplication(r *Replication) *Request {
	data, err := urlencoding.Marshal(r)
	if err != nil {
		return NewRequestError(err)
	}

	return NewRequest((*Client).Post, "/controller/createReplication", data, nil)
}

// UpdateReplication updates the parts of an XDCR replication that can be updated.
func UpdateReplication(r *Replication, uuid, from, to string) *Request {
	data, err := urlencoding.Marshal(r)
	if err != nil {
		return NewRequestError(err)
	}

	return NewRequest((*Client).Post, "/settings/replications/"+url.PathEscape(uuid+"/"+from+"/"+to), data, nil)
}

// DeleteReplication deletes an existing XDCR replication between clusters.
func DeleteReplication(uuid, from, to string) *Request {
	return NewRequest((*Client).Delete, "/controller/cancelXDCR/"+url.PathEscape(uuid+"/"+from+"/"+to), nil, nil)
}

// ListUsers lists all users on the cluster.
func ListUsers(users *UserList) *Request {
	return NewRequest((*Client).Get, "/settings/rbac/users", nil, users)
}

// CreateUser creates a new user.
func CreateUser(user *User) *Request {
	params := user.FormEncode()

	return NewRequest((*Client).Put, fmt.Sprintf("/settings/rbac/users/%v/%v", user.Domain, user.ID), params, nil)
}

// DeleteUser deletes a user.
func DeleteUser(user *User) *Request {
	return NewRequest((*Client).Delete, fmt.Sprintf("/settings/rbac/users/%v/%v", user.Domain, user.ID), nil, nil)
}

// GetUser gets a named user.
func GetUser(id string, domain AuthDomain, user *User) *Request {
	return NewRequest((*Client).Get, fmt.Sprintf("/settings/rbac/users/%v/%v", domain, id), nil, user)
}

// ListGroups lists all groups.
func ListGroups(groups *GroupList) *Request {
	return NewRequest((*Client).Get, "/settings/rbac/groups", nil, &groups)
}

// CreateGroup creates a new group.
func CreateGroup(group *Group) *Request {
	roles := RolesToStr(group.Roles)

	data := url.Values{}
	data.Set("roles", strings.Join(roles, ","))
	data.Set("description", group.Description)
	data.Set("ldap_group_ref", group.LDAPGroupRef)

	return NewRequest((*Client).Put, fmt.Sprintf("/settings/rbac/groups/%v", group.ID), []byte(data.Encode()), nil)
}

// DeleteGroup deletes a group.
func DeleteGroup(group *Group) *Request {
	return NewRequest((*Client).Delete, fmt.Sprintf("/settings/rbac/groups/%v", group.ID), nil, nil)
}

// GetGroup gets a named group.
func GetGroup(id string, group *Group) *Request {
	return NewRequest((*Client).Get, fmt.Sprintf("/settings/rbac/groups/%v", id), nil, group)
}

// GetLDAPSettings gets the LDAP settings for the cluster.
func GetLDAPSettings(settings *LDAPSettings) *Request {
	return NewRequest((*Client).Get, "/settings/ldap", nil, settings)
}

// SetLDAPSettings sets the LDAP settings for the cluster.
func SetLDAPSettings(settings *LDAPSettings) *Request {
	params, err := settings.FormEncode()
	if err != nil {
		return NewRequestError(err)
	}

	return NewRequest((*Client).Post, "/settings/ldap", params, nil)
}

// GetLDAPConnectivityStatus gets the connectivity status with the LDAP service.
func GetLDAPConnectivityStatus(status *LDAPStatus) *Request {
	data := url.Values{}

	return NewRequest((*Client).Post, "/settings/ldap/validate/connectivity", []byte(data.Encode()), status)
}

// GetSecuritySettings gets the cluster security settings.
func GetSecuritySettings(s *SecuritySettings) *Request {
	return NewRequest((*Client).Get, "/settings/security", nil, s)
}

// SetSecuritySettings sets the cluster security settings.
func SetSecuritySettings(s *SecuritySettings) *Request {
	// Because, this API accepts cipherSuites in the form ["suite_1", "suite_2"], with "".
	cipherSuites := []string{}
	for _, cs := range s.CipherSuites {
		cipherSuites = append(cipherSuites, fmt.Sprintf(`"%s"`, cs))
	}

	s.CipherSuites = cipherSuites

	data, err := urlencoding.Marshal(s)
	if err != nil {
		return NewRequestError(err)
	}

	return NewRequest((*Client).Post, "/settings/security", data, nil)
}

// SetNodeNetworkConfiguration sets the network configuration settings for a node.
func SetNodeNetworkConfiguration(s *NodeNetworkConfiguration) *Request {
	data, err := urlencoding.Marshal(s)
	if err != nil {
		return NewRequestError(err)
	}

	return NewRequest((*Client).Post, "/node/controller/setupNetConfig", data, nil)
}

// EnableExternalListener enables a listener (probably an API port, for a specific protocol for a node).
func EnableExternalListener(s *ListenerConfiguration) *Request {
	data, err := urlencoding.Marshal(s)
	if err != nil {
		return NewRequestError(err)
	}

	return NewRequest((*Client).Post, "/node/controller/enableExternalListener", data, nil)
}

// EnableExternalListener disables a listener (probably an API port, for a specific protocol for a node).
func DisableExternalListener(s *ListenerConfiguration) *Request {
	data, err := urlencoding.Marshal(s)
	if err != nil {
		return NewRequestError(err)
	}

	return NewRequest((*Client).Post, "/node/controller/disableExternalListener", data, nil)
}

func GetRunningTasks(runningTasks *RunningTasks) *Request {
	return NewRequest((*Client).Get, "/pools/default/tasks", nil, runningTasks)
}

// GracefulFailover will start a graceful failover of a set of OTPNodes.
// NOTE: You will have to poll the tasks API to watch the status of the failover.
func GracefulFailover(otpNodeList OTPNodeList) *Request {
	data := url.Values{}
	for _, otpNode := range otpNodeList {
		data.Add("otpNode", string(otpNode))
	}

	return NewRequest((*Client).Post, "/controller/startGracefulFailover", []byte(data.Encode()), nil)
}

// Failover forces a down node to go into the failed state so the operator can start to recover it.
// THIS IS VERY DANGEROUS AND CAN DESTROY A USER'S DATASET.  ONLY USE IF YOU KNOW WHAT YOU ARE
// DOING.  AND I REALLY REALLY MEAN IT.
func Failover(otpNodes OTPNodeList, unsafe bool) *Request {
	data := url.Values{}

	for _, otpNode := range otpNodes {
		data.Add("otpNode", string(otpNode))
	}

	if unsafe {
		data.Add("allowUnsafe", "true")
	}

	return NewRequest((*Client).Post, "/controller/failOver", []byte(data.Encode()), nil)
}

// ChangePassword changes the password for the current user, in our case, admin.
func ChangePassword(password string) *Request {
	data := url.Values{}
	data.Add("password", password)

	return NewRequest((*Client).Post, "/controller/changePassword", []byte(data.Encode()), nil)
}

// GetQuerySettings returns settings for the query service.
func GetQuerySettings(settings *QuerySettings) *Request {
	return NewRequest((*Client).Get, "/settings/querySettings", nil, settings)
}

// SetQuerySettings sets settings for the query service.
func SetQuerySettings(settings *QuerySettings) *Request {
	data, err := urlencoding.Marshal(settings)
	if err != nil {
		return NewRequestError(err)
	}

	return NewRequest((*Client).Post, "/settings/querySettings", data, nil)
}

// GetMemcachedGlobalSettings returns stuff like reader/writer threads.
func GetMemcachedGlobalSettings(settings *MemcachedGlobals) *Request {
	return NewRequest((*Client).Get, "/pools/default/settings/memcached/global", nil, settings)
}

// SetMemcachedGlobalSettings sets stuff like reader/writer threads.
func SetMemcachedGlobalSettings(settings *MemcachedGlobals) *Request {
	data, err := urlencoding.Marshal(settings)
	if err != nil {
		return NewRequestError(err)
	}

	return NewRequest((*Client).Post, "/pools/default/settings/memcached/global", data, nil)
}

// ListScopes is a misnomer as this returns literally everything, there is no method
// to list collections.
func ListScopes(bucket string, scopes *ScopeList) *Request {
	return NewRequest((*Client).Get, fmt.Sprintf("/pools/default/buckets/%s/scopes", bucket), nil, scopes)
}

func CreateScope(bucket, scope string) *Request {
	data := url.Values{}
	data.Add("name", scope)

	return NewRequest((*Client).Post, fmt.Sprintf("/pools/default/buckets/%s/scopes", bucket), []byte(data.Encode()), nil)
}

func DeleteScope(bucket, scope string) *Request {
	return NewRequest((*Client).Delete, fmt.Sprintf("/pools/default/buckets/%s/scopes/%s", bucket, scope), nil, nil)
}

func CreateCollection(bucket, scope string, collection Collection) *Request {
	data, err := urlencoding.Marshal(collection)
	if err != nil {
		return NewRequestError(err)
	}

	return NewRequest((*Client).Post, fmt.Sprintf("/pools/default/buckets/%s/scopes/%s/collections", bucket, scope), data, nil)
}

func PatchCollection(bucket, scope string, collection Collection) *Request {
	// only mutation field is mutable
	data, err := urlencoding.Marshal(CollectionPatchRequest{History: collection.History})
	if err != nil {
		return NewRequestError(err)
	}

	return NewRequest((*Client).Patch, fmt.Sprintf("/pools/default/buckets/%s/scopes/%s/collections/%s", bucket, scope, collection.Name), data, nil)
}

func DeleteCollection(bucket, scope, collection string) *Request {
	return NewRequest((*Client).Delete, fmt.Sprintf("/pools/default/buckets/%s/scopes/%s/collections/%s", bucket, scope, collection), nil, nil)
}

// GetStatsRangeAvgMetrics returns stats range metrics for couchbase services.
func GetStatsRangeAvgMetrics(service string, metrics *StatsRangeMetrics) *Request {
	metricName := ""

	switch service {
	case "index":
		metricName = "/index_total_disk_size"
	case "data":
		metricName = "/couch_docs_actual_disk_size"
	case "views":
		metricName = "/couch_views_actual_disk_size"
	}

	return NewRequest((*Client).Get, "/pools/default/stats/range"+metricName+"/avg?start=-10&step=10", nil, metrics)
}

func GetTerseClusterInfo(clusterInfo *TerseClusterInfo) *Request {
	return NewRequest((*Client).Get, "/pools/default/terseClusterInfo", nil, clusterInfo)
}
