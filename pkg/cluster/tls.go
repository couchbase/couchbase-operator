package cluster

import (
	"context"
	"crypto/x509"
	"fmt"
	"reflect"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/cluster/persistence"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/netutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	util_x509 "github.com/couchbase/couchbase-operator/pkg/util/x509"
)

// tlsValid checks the members TLS is valid for the CA and the certificate leaf matches.
func tlsValid(member couchbaseutil.Member, ca, clientCert, clientKey []byte, cert *x509.Certificate) bool {
	serverChain, err := netutil.GetTLSState(member.GetHostPortTLS(), ca, clientCert, clientKey)
	if err == nil && serverChain[0].Equal(cert) {
		return true
	}

	return false
}

// reloadCA insecurely reloads the cluster CA certificate.
func (c *Cluster) reloadCA(member couchbaseutil.Member, cacert []byte) error {
	oldcacert := []byte{}
	if err := couchbaseutil.GetClusterCACert(oldcacert).On(c.api, member); err != nil {
		return err
	}

	if !reflect.DeepEqual(cacert, oldcacert) {
		log.Info("Reloading CA certificate", "cluster", c.namespacedName(), "name", member.Name())

		// If node to node is enabled, then server will refuse to rotate TLS, for good reason,
		// so force disable it when performing TLS updates.
		if err := c.disableNodeToNode(); err != nil {
			return err
		}

		if err := couchbaseutil.SetClusterCACert(cacert).On(c.api, member); err != nil {
			return err
		}
	}

	return nil
}

// reloadChain does an insecure reload of the TLS certificates and keys.
func (c *Cluster) reloadChain(member couchbaseutil.Member) error {
	return couchbaseutil.ReloadNodeCert().On(c.api, member)
}

// reloadChainAndVerify reloads the certificate chain for a member when necessary,
// waiting until the certificate is presented by the server.
func (c *Cluster) reloadChainAndVerify(member couchbaseutil.Member, cacert, clientCert, clientKey []byte, cert *x509.Certificate) error {
	log.Info("Reloading certificate chain", "cluster", c.namespacedName(), "name", member.Name())

	// Wait for the certificate data to be updated. NS server has a few quirks (as per usual... sigh).
	// We need to keep retrying until the secret mount is updated by kubelet, then this will fail
	// due to a dirty shutdown of TLS.  So prioritize the end result over the retry or we will
	// get stuck.
	callback := func() error {
		if tlsValid(member, cacert, clientCert, clientKey, cert) {
			return nil
		}

		if err := c.reloadChain(member); err != nil {
			return err
		}

		if !tlsValid(member, cacert, clientCert, clientKey, cert) {
			return fmt.Errorf("%w: certificate chain not served", errors.NewStackTracedError(errors.ErrCouchbaseServerError))
		}

		return nil
	}

	ctx, cancel := context.WithTimeout(c.ctx, extendedRetryPeriod)
	defer cancel()

	if err := retryutil.RetryOnErr(ctx, 5*time.Second, callback); err != nil {
		return err
	}

	return nil
}

// getTLSData gets the TLS data from kubernetes and performs some error checking.
func (c *Cluster) getTLSData() (ca []byte, chain []byte, key []byte, err error) {
	// Load the TLS data from kubernetes.
	operatorSecret, found := c.k8s.Secrets.Get(c.cluster.Spec.Networking.TLS.Static.OperatorSecret)
	if !found {
		err = fmt.Errorf("%w: unable to get operator secret %s", errors.NewStackTracedError(errors.ErrResourceRequired), c.cluster.Spec.Networking.TLS.Static.OperatorSecret)
		return
	}

	serverSecret, found := c.k8s.Secrets.Get(c.cluster.Spec.Networking.TLS.Static.ServerSecret)
	if !found {
		err = fmt.Errorf("%w: unable to get server secret %s", errors.NewStackTracedError(errors.ErrResourceRequired), c.cluster.Spec.Networking.TLS.Static.ServerSecret)
		return
	}

	// Ensure that the secrets are correctly formatted.
	var ok bool

	ca, ok = operatorSecret.Data[tlsOperatorSecretCACert]
	if !ok {
		err = fmt.Errorf("%w: operator secret missing ca.crt", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
		return
	}

	key, ok = serverSecret.Data["pkey.key"]
	if !ok {
		err = fmt.Errorf("%w: server secret missing pkey.key", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
		return
	}

	chain, ok = serverSecret.Data["chain.pem"]
	if !ok {
		err = fmt.Errorf("%w: server secret missing chain.pem", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
		return
	}

	return
}

// getVerifiedTLSData is an extended version of getTLSData that performs certificate
// verification of tainted input.
func (c *Cluster) getVerifiedTLSData() (ca, chain []byte, err error) {
	// Load server TLS data from kubernetes and verify.
	cacert, chain, key, err := c.getTLSData()
	if err != nil {
		return nil, nil, err
	}

	subjectAltNames := util_x509.MandatorySANs(c.cluster.Name, c.cluster.Namespace)

	if c.cluster.Spec.Networking.DNS != nil {
		subjectAltNames = append(subjectAltNames, "*."+c.cluster.Spec.Networking.DNS.Domain)
	}

	if errs := util_x509.Verify(cacert, chain, key, x509.ExtKeyUsageServerAuth, subjectAltNames); len(errs) != 0 {
		errStrings := []string{}

		for _, err := range errs {
			errStrings = append(errStrings, err.Error())
		}

		errString := strings.Join(errStrings, ", ")

		return nil, nil, fmt.Errorf("%w: %s", errors.NewStackTracedError(errors.ErrTLSInvalid), errString)
	}

	return cacert, chain, nil
}

// getTLSClientData returns the PEM files required for client authentication.
func (c *Cluster) getTLSClientData() (chain []byte, key []byte, err error) {
	// Load the TLS data from kubernetes.
	operatorSecret, found := c.k8s.Secrets.Get(c.cluster.Spec.Networking.TLS.Static.OperatorSecret)
	if !found {
		err = fmt.Errorf("%w: unable to get operator secret %s", errors.NewStackTracedError(errors.ErrResourceRequired), c.cluster.Spec.Networking.TLS.Static.OperatorSecret)
		return
	}

	var ok bool

	chain, ok = operatorSecret.Data[tlsOperatorSecretCert]
	if !ok {
		err = fmt.Errorf("%w: operator secret missing %s", errors.NewStackTracedError(errors.ErrResourceAttributeRequired), tlsOperatorSecretCert)
		return
	}

	key, ok = operatorSecret.Data[tlsOperatorSecretKey]
	if !ok {
		err = fmt.Errorf("%w: operator secret missing %s", errors.NewStackTracedError(errors.ErrResourceAttributeRequired), tlsOperatorSecretKey)
		return
	}

	return
}

func (c *Cluster) getVerifiedTLSClientData(cacert []byte) (chain []byte, key []byte, err error) {
	clientCert, clientKey, err := c.getTLSClientData()
	if err != nil {
		return nil, nil, err
	}

	if errs := util_x509.Verify(cacert, clientCert, clientKey, x509.ExtKeyUsageClientAuth, nil); len(errs) != 0 {
		errStrings := []string{}

		for _, err := range errs {
			errStrings = append(errStrings, err.Error())
		}

		errString := strings.Join(errStrings, ", ")

		return nil, nil, fmt.Errorf("%w: %s", errors.NewStackTracedError(errors.ErrTLSInvalid), errString)
	}

	return clientCert, clientKey, nil
}

// reconcileMemberTLS reconciles both the CA and certificate chain on Couchbase server.
// This is done in plain text due to races involving required mTLS.
func (c *Cluster) reconcileMemberTLS(member couchbaseutil.Member, ca, cert, key []byte, leaf *x509.Certificate) error {
	// Try connect to the target node, if it doesn't respond we assume it's
	// deleted or the admin service has gone down and needs a reconcile to fix it.
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()

	if err := netutil.WaitForHostPort(ctx, member.GetHostPort()); err != nil {
		return nil
	}

	if tlsValid(member, ca, cert, key, leaf) {
		return nil
	}

	// Reload the CA certificate if necessary.
	if err := c.reloadCA(member, ca); err != nil {
		return err
	}

	// If the pods doesn't have TLS enabled then ignore it.
	pod, found := c.k8s.Pods.Get(member.Name())
	if !found {
		return nil
	}

	if _, ok := pod.Annotations[constants.PodTLSAnnotation]; !ok {
		return nil
	}

	// Reload the server certificate chain.
	if err := c.reloadChainAndVerify(member, ca, cert, key, leaf); err != nil {
		return err
	}

	c.raiseEvent(k8sutil.TLSUpdatedEvent(c.cluster, member.Name()))

	return nil
}

// updateClientCertAuthSettings is the main call that enables, disables and updated mTLS.
// It is also responsible for abstracting away any dodgy behaviour at the API level.
func (c *Cluster) updateClientCertAuthSettings(settings *couchbaseutil.ClientCertAuth) error {
	// The API is broken and expects something, even an empty list.
	if settings.Prefixes == nil {
		settings.Prefixes = []couchbaseutil.ClientCertAuthPrefix{}
	}

	// These settings are ACCEPTED and take some time to apply, so wait until they are
	// live, lest we get non-determinism.
	callback := func() error {
		// Repeat this call, if we are turning on mTLS then the API will do an
		// unclean termination, and we may get an EOF.
		if err := couchbaseutil.SetClientCertAuth(settings).On(c.api, c.members); err != nil {
			return err
		}

		currentSettings := &couchbaseutil.ClientCertAuth{}
		if err := couchbaseutil.GetClientCertAuth(currentSettings).On(c.api, c.members); err != nil {
			return err
		}

		if !reflect.DeepEqual(currentSettings, settings) {
			return fmt.Errorf("%w: client TLS not reconciled", errors.NewStackTracedError(errors.ErrCouchbaseServerError))
		}

		return nil
	}

	ctx, cancel := context.WithTimeout(c.ctx, time.Minute)
	defer cancel()

	if err := retryutil.RetryOnErr(ctx, time.Second, callback); err != nil {
		return err
	}

	c.raiseEvent(k8sutil.ClusterSettingsEditedEvent("client authentication", c.cluster))

	return nil
}

// enableTLS dynamically enables the use of TLS by the Operator.  This must be
// done before creating any TLS enabled nodes, or communication will fail.
func (c *Cluster) enableTLS() error {
	// If TLS is not enabled, then ignore this.
	if !c.cluster.IsTLSEnabled() {
		return nil
	}

	// If the client is populated, then TLS was previously set (persisted),
	// it's a new cluster, or it's being upgraded to from <2.1.  What this
	// doesn't capture is the fact that someone could have added TLS while
	// the operator is down, and cluster upgrade will fail.  We can fix this
	// in 2.2 once we don't have to worry about the upgrade path.
	clientTLS := c.api.GetTLS()
	if clientTLS != nil {
		return nil
	}

	log.Info("Enabling TLS", "cluster", c.namespacedName())

	ca, _, err := c.getVerifiedTLSData()
	if err != nil {
		c.raiseEventCached(k8sutil.TLSInvalidEvent(c.cluster))
		return err
	}

	// Reload the CA certificate if necessary.  This must happen before new
	// nodes are added as server does what the hell it wants to and just copies
	// over what is the current CA to the new node, irrespective of what we
	// pre-populate it with.
	for _, member := range c.members {
		if err := c.reloadCA(member, ca); err != nil {
			return err
		}
	}

	clientTLS = &couchbaseutil.TLSAuth{
		CACert: ca,
	}

	c.api.SetTLS(clientTLS)

	if err := c.state.Insert(persistence.CACertificate, string(ca)); err != nil {
		return err
	}

	c.raiseEvent(k8sutil.ClientTLSUpdatedEvent(c.cluster, k8sutil.ClientTLSUpdateReasonCreateCA))

	return nil
}

// updateTLS is responsible for modifying (rotating) any existing TLS certificates that Couchbase
// is serving.  Once complete, it optionally starts using a new CA.
func (c *Cluster) updateTLS() error {
	if !c.cluster.IsTLSEnabled() {
		return nil
	}

	// Load server TLS data from kubernetes and verify.
	cacert, chain, err := c.getVerifiedTLSData()
	if err != nil {
		c.raiseEventCached(k8sutil.TLSInvalidEvent(c.cluster))

		return err
	}

	// If client authentication is specified load and verify those certificates.
	var clientCert []byte

	var clientKey []byte

	if c.cluster.IsMutualTLSEnabled() {
		clientCert, clientKey, err = c.getVerifiedTLSClientData(cacert)
		if err != nil {
			c.raiseEventCached(k8sutil.ClientTLSInvalidEvent(c.cluster))

			return err
		}
	}

	// Parse the certificate chain.
	chainPem := util_x509.DecodePEM(chain)

	cert, err := x509.ParseCertificate(chainPem[0].Bytes)
	if err != nil {
		return errors.NewStackTracedError(err)
	}

	// Quiesce persistent connections, NS server doesn't quite work if some are
	// still open.
	c.api.CloseIdleConnections()

	// Update the CA and any server certificate chains that require it.
	for _, member := range c.members {
		if err := c.reconcileMemberTLS(member, cacert, clientCert, clientKey, cert); err != nil {
			return err
		}
	}

	clientTLS := c.api.GetTLS()

	newClientTLS := *clientTLS
	newClientTLS.CACert = cacert

	if !reflect.DeepEqual(clientTLS, &newClientTLS) {
		log.Info("Reloading client CA certificate", "cluster", c.namespacedName())

		c.api.SetTLS(&newClientTLS)

		if err := c.state.Update(persistence.CACertificate, string(cacert)); err != nil {
			return err
		}

		c.raiseEvent(k8sutil.ClientTLSUpdatedEvent(c.cluster, k8sutil.ClientTLSUpdateReasonUpdateCA))
	}

	return nil
}

// disableTLS turns off client support for TLS, this must be done after all TLS enabled
// Couchbase nodes have been removed from the cluster.
func (c *Cluster) disableTLS() error {
	if c.cluster.IsTLSEnabled() {
		return nil
	}

	clientTLS := c.api.GetTLS()

	if clientTLS == nil {
		return nil
	}

	log.Info("Disabling TLS", "cluster", c.namespacedName())

	c.api.SetTLS(nil)

	if err := c.state.Delete(persistence.CACertificate); err != nil {
		return err
	}

	c.raiseEvent(k8sutil.ClientTLSUpdatedEvent(c.cluster, k8sutil.ClientTLSUpdateReasonDeleteCA))

	return nil
}

// enableMutualTLS is responsible for enabling mTLS at runtime.  This loads the client certs
// into the API client then enables mTLS on the server side.
func (c *Cluster) enableMutualTLS() error {
	if !c.cluster.IsMutualTLSEnabled() {
		return nil
	}

	// If the client is already populated then ignore this.
	// It will be populated for:
	// * New cluster created with mTLS
	// * Existing cluster upgraded to 2.1 (sourced from the secret)
	// * Existing cluster is required (sourced from persistence)
	// This leaves any cluster that was upgraded to mTLS at runtime.
	clientTLS := c.api.GetTLS()
	if clientTLS.ClientAuth == nil {
		log.Info("Loading client certificate", "cluster", c.namespacedName())

		cert, key, err := c.getTLSClientData()
		if err != nil {
			return err
		}

		clientTLS.ClientAuth = &couchbaseutil.TLSClientAuth{
			Cert: cert,
			Key:  key,
		}

		c.api.SetTLS(clientTLS)

		if err := c.state.Insert(persistence.ClientCertificate, string(cert)); err != nil {
			return err
		}

		if err := c.state.Insert(persistence.ClientKey, string(key)); err != nil {
			return err
		}

		c.raiseEvent(k8sutil.ClientTLSUpdatedEvent(c.cluster, k8sutil.ClientTLSUpdateReasonCreateClientAuth))
	}

	// Get the current encryption settings
	existingSettings := &couchbaseutil.ClientCertAuth{}
	if err := couchbaseutil.GetClientCertAuth(existingSettings).On(c.api, c.members); err != nil {
		return err
	}

	if existingSettings.State != "disable" {
		return nil
	}

	log.Info("Enabling mTLS", "cluster", c.namespacedName())

	// Reconcile client ceritifcate policy. Defaults to disable (implied by nil policy).
	settings := &couchbaseutil.ClientCertAuth{
		State: string(*c.cluster.Spec.Networking.TLS.ClientCertificatePolicy),
	}

	for _, path := range c.cluster.Spec.Networking.TLS.ClientCertificatePaths {
		prefix := couchbaseutil.ClientCertAuthPrefix{
			Path:      path.Path,
			Prefix:    path.Prefix,
			Delimiter: path.Delimiter,
		}

		settings.Prefixes = append(settings.Prefixes, prefix)
	}

	if err := c.updateClientCertAuthSettings(settings); err != nil {
		return err
	}

	return nil
}

// updateMutualTLS ensures that client certificates are correctly installed before updating
// the server side configuration.  Client certificates must be loaded first to handle rotation
// of the entire PKI so subsequent calls work.  Updating the client settings must occur
// immediately after the server certifcates and CA have been rotated with no intervening calls,
// otherwise they will fail.
func (c *Cluster) updateMutualTLS() error {
	if !c.cluster.IsMutualTLSEnabled() {
		return nil
	}

	// If not enabled yet, then ignore this update.
	clientTLS := c.api.GetTLS()
	if clientTLS.ClientAuth == nil {
		return nil
	}

	// Verified already by cert updates, so we don't need to mess with getting the CA.
	// Note of caution, if you've changed the secret data in the mean time...
	cert, key, err := c.getTLSClientData()
	if err != nil {
		c.raiseEventCached(k8sutil.ClientTLSInvalidEvent(c.cluster))

		return err
	}

	// Pass by reference, caution!
	newClientTLS := *clientTLS
	newClientTLS.ClientAuth = &couchbaseutil.TLSClientAuth{
		Cert: cert,
		Key:  key,
	}

	if !reflect.DeepEqual(clientTLS, &newClientTLS) {
		log.Info("Reloading client certificate", "cluster", c.namespacedName())

		c.api.SetTLS(&newClientTLS)

		if err := c.state.Update(persistence.ClientCertificate, string(cert)); err != nil {
			return err
		}

		if err := c.state.Update(persistence.ClientKey, string(key)); err != nil {
			return err
		}

		c.raiseEvent(k8sutil.ClientTLSUpdatedEvent(c.cluster, k8sutil.ClientTLSUpdateReasonUpdateClientAuth))
	}

	// Get the current encryption settings
	existingSettings := &couchbaseutil.ClientCertAuth{}
	if err := couchbaseutil.GetClientCertAuth(existingSettings).On(c.api, c.members); err != nil {
		return err
	}

	// Reconcile client ceritifcate policy. Defaults to disable (implied by nil policy).
	settings := &couchbaseutil.ClientCertAuth{
		State: string(*c.cluster.Spec.Networking.TLS.ClientCertificatePolicy),
	}

	for _, path := range c.cluster.Spec.Networking.TLS.ClientCertificatePaths {
		prefix := couchbaseutil.ClientCertAuthPrefix{
			Path:      path.Path,
			Prefix:    path.Prefix,
			Delimiter: path.Delimiter,
		}

		settings.Prefixes = append(settings.Prefixes, prefix)
	}

	if !reflect.DeepEqual(existingSettings, settings) {
		log.Info("Updating mTLS", "cluster", c.namespacedName())

		if err := c.updateClientCertAuthSettings(settings); err != nil {
			return err
		}
	}

	return nil
}

// disableMutualTLS turns off mTLS and removes the configuration from the client and storage.
func (c *Cluster) disableMutualTLS() error {
	if c.cluster.IsMutualTLSEnabled() {
		return nil
	}

	// Get the current encryption settings
	existingSettings := &couchbaseutil.ClientCertAuth{}
	if err := couchbaseutil.GetClientCertAuth(existingSettings).On(c.api, c.members); err != nil {
		return err
	}

	if existingSettings.State == "disable" {
		return nil
	}

	log.Info("Disabling mTLS", "cluster", c.namespacedName())

	// Disable the feature.
	settings := &couchbaseutil.ClientCertAuth{
		State: "disable",
	}

	if err := c.updateClientCertAuthSettings(settings); err != nil {
		return err
	}

	// Remove the certificates from the client, it's perhaps unnecessary,
	// however we can catch weird behaviour by being strict about it.
	clientTLS := c.api.GetTLS()
	clientTLS.ClientAuth = nil
	c.api.SetTLS(clientTLS)

	if err := c.state.Delete(persistence.ClientCertificate); err != nil {
		return err
	}

	if err := c.state.Delete(persistence.ClientKey); err != nil {
		return err
	}

	c.raiseEvent(k8sutil.ClientTLSUpdatedEvent(c.cluster, k8sutil.ClientTLSUpdateReasonDeleteClientAuth))

	return nil
}

// disableNodeToNode forces node-to-node encryption to off.
func (c *Cluster) disableNodeToNode() error {
	return c.reconcileNodeToNode(false)
}

// updateNodeToNode modifies node-to-node encryption as per the specification.
func (c *Cluster) updateNodeToNode() error {
	return c.reconcileNodeToNode(c.nodeToNodeEnabled())
}

// reconcileNodeToNode turns node-to-node encryption on/off.
func (c *Cluster) reconcileNodeToNode(requestedEncryption bool) error {
	if !c.supportsNodeToNode() {
		return nil
	}

	// See if any nodes are in the wrong state.
	updatableMembers := couchbaseutil.NewMemberSet()

	for name, m := range c.members {
		s := &couchbaseutil.NodeNetworkConfiguration{}
		if err := c.getNodeNetworkConfiguration(m, s); err != nil {
			// As the message says, this is "fine"
			log.Info("failed to get node network configuration", "cluster", c.namespacedName(), "pod", name, "error", err)
			return nil
		}

		if (s.NodeEncryption == couchbaseutil.On) != requestedEncryption {
			updatableMembers.Add(m)
		}
	}

	// If we are disabling encryption then we need to set the mode to control plane only first...
	if !requestedEncryption {
		securitySettings := &couchbaseutil.SecuritySettings{}
		if err := couchbaseutil.GetSecuritySettings(securitySettings).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		// Only update if the current setting is not null (which defaults to..) or not control plane.
		if securitySettings.ClusterEncryptionLevel != "" && securitySettings.ClusterEncryptionLevel != couchbaseutil.ClusterEncryptionControl {
			requestedSecuritySettings := *securitySettings
			requestedSecuritySettings.ClusterEncryptionLevel = couchbaseutil.ClusterEncryptionControl

			if err := couchbaseutil.SetSecuritySettings(&requestedSecuritySettings).On(c.api, c.readyMembers()); err != nil {
				return err
			}

			c.raiseEvent(k8sutil.SecuritySettingsUpdatedEvent(c.cluster, k8sutil.SecuritySettingUpdatedN2NEncryptionModeModified))
		}
	}

	// Modify encryption settings for each node.
	if !updatableMembers.Empty() {
		// For some reasons you need to disable failover because server is
		// incapable of doing this itself...
		failoverSettings := &couchbaseutil.AutoFailoverSettings{}
		if err := couchbaseutil.GetAutoFailoverSettings(failoverSettings).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		failoverWasEnabled := failoverSettings.Enabled

		if failoverWasEnabled {
			failoverSettings.Enabled = false

			if err := couchbaseutil.SetAutoFailoverSettings(failoverSettings).On(c.api, c.readyMembers()); err != nil {
				return err
			}
		}

		// Booleans obviously don't exist in serverland...
		encryptionEnabledString := couchbaseutil.Off
		encryptionDisabledString := couchbaseutil.On

		if requestedEncryption {
			encryptionEnabledString = couchbaseutil.On
			encryptionDisabledString = couchbaseutil.Off
		}

		networkSettings := &couchbaseutil.NodeNetworkConfiguration{
			AddressFamily:  couchbaseutil.AddressFamilyIPV4,
			NodeEncryption: encryptionEnabledString,
		}

		antiNetworkSettings := &couchbaseutil.NodeNetworkConfiguration{
			AddressFamily:  couchbaseutil.AddressFamilyIPV4,
			NodeEncryption: encryptionDisabledString,
		}

		// Update one API per node...
		for _, m := range updatableMembers {
			// The auto-failover settings may take a while to take effect, so retry
			// this call a few times.
			if err := couchbaseutil.EnableExternalListener(networkSettings).RetryFor(time.Minute).On(c.api, m); err != nil {
				return err
			}
		}

		// Update another API per node, with exactly the same configuration...
		for _, m := range updatableMembers {
			if err := couchbaseutil.SetNodeNetworkConfiguration(networkSettings).On(c.api, m); err != nil {
				return err
			}
		}

		// And another API per node...
		// The prior command does seem to trigger a network restart and may cause the
		// following calls to fail, so retry.
		for i := range updatableMembers {
			m := updatableMembers[i]

			if err := couchbaseutil.DisableExternalListener(antiNetworkSettings).RetryFor(time.Minute).On(c.api, m); err != nil {
				return err
			}
		}

		// Reenable auto failover
		if failoverWasEnabled {
			failoverSettings.Enabled = true

			if err := couchbaseutil.SetAutoFailoverSettings(failoverSettings).On(c.api, c.readyMembers()); err != nil {
				return err
			}
		}

		c.raiseEvent(k8sutil.SecuritySettingsUpdatedEvent(c.cluster, k8sutil.SecuritySettingUpdatedN2NEncryptionModified))
	}

	// Encryption is not enabled, ignore any further settings.
	if !requestedEncryption {
		return nil
	}

	securitySettings := &couchbaseutil.SecuritySettings{}
	if err := couchbaseutil.GetSecuritySettings(securitySettings).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	requestedSecuritySettings := *securitySettings

	switch *c.cluster.Spec.Networking.TLS.NodeToNodeEncryption {
	case couchbasev2.NodeToNodeControlPlaneOnly:
		requestedSecuritySettings.ClusterEncryptionLevel = couchbaseutil.ClusterEncryptionControl
	case couchbasev2.NodeToNodeAll:
		requestedSecuritySettings.ClusterEncryptionLevel = couchbaseutil.ClusterEncryptionAll
	default:
		return fmt.Errorf("%w: illegal cluster encryption level '%s'", errors.NewStackTracedError(errors.ErrConfigurationInvalid), *c.cluster.Spec.Networking.TLS.NodeToNodeEncryption)
	}

	// Nothing has changed, ignore.
	if reflect.DeepEqual(securitySettings, &requestedSecuritySettings) {
		return nil
	}

	if err := couchbaseutil.SetSecuritySettings(&requestedSecuritySettings).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	c.raiseEvent(k8sutil.SecuritySettingsUpdatedEvent(c.cluster, k8sutil.SecuritySettingUpdatedN2NEncryptionModeModified))

	return nil
}

// reconcileTLSPreTopology handles TLS reconciliation before the topology changes.
func (c *Cluster) reconcileTLSPreTopologyChange() error {
	// If the cluster is upgrading, then don't interfere with TLS until the process
	// is complete.  All topology changes should be considered "atomic" or the logic
	// for this just gets mind bending.
	if _, err := c.state.Get(persistence.Upgrading); err == nil {
		return nil
	}

	// When disabling TLS, this causes an "upgrade" to remove the TLS volume
	// from the pods, and doesn't install certificates, leading to the client
	// potentially failing to connect to the new pods.  If the user intends to
	// disable mTLS, do it before changing the topology.  Recovering nodes should
	// sync configuration eventually, so we expect a few errors while this happens.
	if err := c.disableMutualTLS(); err != nil {
		return err
	}

	// When enabling TLS ensure the client is updated with the CA before adding in,
	// and communicating with, any new nodes.
	if err := c.enableTLS(); err != nil {
		return err
	}

	return nil
}

// reconcileTLS performs any certificate rotations that are necessary.
// We perform all of the rotations in plain text as this would become a
// nightmare with required mTLS.  We always ensure TLS client settings
// are enforced on exit from this function as we don't ever want to
// leak sensitive information.
func (c *Cluster) reconcileTLSPostTopologyChange() error {
	// If the cluster is upgrading, then don't interfere with TLS until the process
	// is complete.  All topology changes should be considered "atomic" or the logic
	// for this just gets mind bending.
	if _, err := c.state.Get(persistence.Upgrading); err == nil {
		return nil
	}

	// If TLS is off, but we have client configuration, then remove the CA.  By this point
	// all nodes should be plaintext.  mTLS and client certificates were flushed from the
	// system prior to tolopy changes.
	if err := c.disableTLS(); err != nil {
		return err
	}

	// Member TLS must be updated before balancing in new nodes, Server does what it
	// wants, in this case it will overwrite our TLS with the existing (auto-generated)
	// TLS on addition, we must take control before this happens.
	if err := c.updateTLS(); err != nil {
		return err
	}

	// If mTLS is enabled and we're already using it, then rotate client certs as
	// appropriate, directly after reloading the cluster CA and certs without any
	// intervening API calls.
	if err := c.updateMutualTLS(); err != nil {
		return err
	}

	// If mTLS is enabled but we don't know about it, then enable it directly after
	// rotation so we load client certs before making any further API calls.  Do this after
	// rotation as it will not spot client updates otherwise and fail reconciling the settings.
	if err := c.enableMutualTLS(); err != nil {
		return err
	}

	// If node-to-node encryption is enabled, only turn it on once the cluster is fully
	// TLS enabled, server will refuse to upload a new CA when enabled.
	if err := c.updateNodeToNode(); err != nil {
		return err
	}

	return nil
}
