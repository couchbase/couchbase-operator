/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cluster

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// refreshTLSShadowSecret does what it says, it keeps a shadow version of the TLS
// secret up to date.  Why you ask?  Well CBS is crap and requires the files be
// called chain.pem and pkey.key, whereas our users require it to be whatever they
// want it to be, so we need to change the names for them.
func (c *Cluster) refreshTLSShadowSecret() error {
	if !c.cluster.IsTLSShadowed() {
		return nil
	}

	// Grab the user provided secret.
	_, cert, key, err := c.getTLSDataStandard()
	if err != nil {
		return err
	}

	name := k8sutil.ShadowTLSSecretName(c.cluster)

	requestedShadowSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: k8sutil.LabelsForCluster(c.cluster),
			OwnerReferences: []metav1.OwnerReference{
				c.cluster.AsOwner(),
			},
		},
		Data: map[string][]byte{
			"chain.pem": cert,
			"pkey.key":  key,
		},
	}

	// Pa's special sauce!  Support PKCS#8 keys, because we can.
	block, _ := pem.Decode(key)
	if block == nil {
		return fmt.Errorf("%w: private key not in PEM format", errors.NewStackTracedError(errors.ErrPrivateKeyInvalid))
	}

	switch block.Type {
	case "RSA PRIVATE KEY":
		// PKCS#1, in the right format.
		break
	case "PRIVATE KEY":
		// PKCS#8, way more modern and widely used, but needs conversion.
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return errors.NewStackTracedError(err)
		}

		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return fmt.Errorf("%w: private key not RSA", errors.NewStackTracedError(errors.ErrPrivateKeyInvalid))
		}

		block := &pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(rsaKey),
		}

		buf := &bytes.Buffer{}
		if err := pem.Encode(buf, block); err != nil {
			return errors.NewStackTracedError(err)
		}

		requestedShadowSecret.Data["pkey.key"] = buf.Bytes()
	default:
		return fmt.Errorf("%w: private key in unhandled format %s", errors.NewStackTracedError(errors.ErrPrivateKeyInvalid), block.Type)
	}

	// Look for the shadow secret, if it exists update it, otherwise create it.
	currentShadowSecret, ok := c.k8s.Secrets.Get(name)
	if !ok {
		if _, err := c.k8s.KubeClient.CoreV1().Secrets(c.cluster.Namespace).Create(context.Background(), requestedShadowSecret, metav1.CreateOptions{}); err != nil {
			return errors.NewStackTracedError(err)
		}

		return nil
	}

	if reflect.DeepEqual(requestedShadowSecret.Data, currentShadowSecret.Data) {
		return nil
	}

	updatedShadowSecret := currentShadowSecret.DeepCopy()
	updatedShadowSecret.Data = requestedShadowSecret.Data

	if _, err := c.k8s.KubeClient.CoreV1().Secrets(c.cluster.Namespace).Update(context.Background(), updatedShadowSecret, metav1.UpdateOptions{}); err != nil {
		return errors.NewStackTracedError(err)
	}

	return nil
}

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

	if err := retryutil.RetryFor(extendedRetryPeriod, callback); err != nil {
		return err
	}

	return nil
}

// getTLSDataStandard get TLS server configuration using standard data layout.
func (c *Cluster) getTLSDataStandard() ([]byte, []byte, []byte, error) {
	if c.cluster.Spec.Networking.TLS == nil {
		return nil, nil, nil, fmt.Errorf("%w: TLS not defined", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	if c.cluster.Spec.Networking.TLS.SecretSource == nil {
		return nil, nil, nil, fmt.Errorf("%w: TLS source not defined", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	secret, ok := c.k8s.Secrets.Get(c.cluster.Spec.Networking.TLS.SecretSource.ServerSecretName)
	if !ok {
		return nil, nil, nil, fmt.Errorf("%w: unable to get TLS secret %s", errors.NewStackTracedError(errors.ErrResourceRequired), c.cluster.Spec.Networking.TLS.SecretSource.ServerSecretName)
	}

	ca, ok := secret.Data["ca.crt"]
	if !ok {
		return nil, nil, nil, fmt.Errorf("%w: TLS secret missing ca.crt", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	cert, ok := secret.Data["tls.crt"]
	if !ok {
		return nil, nil, nil, fmt.Errorf("%w: TLS secret missing tls.crt", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	key, ok := secret.Data["tls.key"]
	if !ok {
		return nil, nil, nil, fmt.Errorf("%w: TLS secret missing tls.key", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	return ca, cert, key, nil
}

// getTLSDataLegacy gets TLS server configuration using made-up, proprietary layout.
func (c *Cluster) getTLSDataLegacy() ([]byte, []byte, []byte, error) {
	if c.cluster.Spec.Networking.TLS == nil {
		return nil, nil, nil, fmt.Errorf("%w: TLS not defined", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	if c.cluster.Spec.Networking.TLS.Static == nil {
		return nil, nil, nil, fmt.Errorf("%w: static TLS not defined", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	// Load the TLS data from kubernetes.
	operatorSecret, found := c.k8s.Secrets.Get(c.cluster.Spec.Networking.TLS.Static.OperatorSecret)
	if !found {
		return nil, nil, nil, fmt.Errorf("%w: unable to get operator secret %s", errors.NewStackTracedError(errors.ErrResourceRequired), c.cluster.Spec.Networking.TLS.Static.OperatorSecret)
	}

	serverSecret, found := c.k8s.Secrets.Get(c.cluster.Spec.Networking.TLS.Static.ServerSecret)
	if !found {
		return nil, nil, nil, fmt.Errorf("%w: unable to get server secret %s", errors.NewStackTracedError(errors.ErrResourceRequired), c.cluster.Spec.Networking.TLS.Static.ServerSecret)
	}

	// Ensure that the secrets are correctly formatted.
	ca, ok := operatorSecret.Data["ca.crt"]
	if !ok {
		return nil, nil, nil, fmt.Errorf("%w: operator secret missing ca.crt", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	key, ok := serverSecret.Data["pkey.key"]
	if !ok {
		return nil, nil, nil, fmt.Errorf("%w: server secret missing pkey.key", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	chain, ok := serverSecret.Data["chain.pem"]
	if !ok {
		return nil, nil, nil, fmt.Errorf("%w: server secret missing chain.pem", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	return ca, chain, key, nil
}

// getTLSData gets the TLS data from kubernetes and performs some error checking.
func (c *Cluster) getTLSData() ([]byte, []byte, []byte, error) {
	if c.cluster.IsTLSShadowed() {
		return c.getTLSDataStandard()
	}

	return c.getTLSDataLegacy()
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

	if errs := util_x509.Verify(cacert, chain, key, x509.ExtKeyUsageServerAuth, subjectAltNames, !c.cluster.IsTLSShadowed()); len(errs) != 0 {
		errStrings := []string{}

		for _, err := range errs {
			errStrings = append(errStrings, err.Error())
		}

		errString := strings.Join(errStrings, ", ")

		return nil, nil, fmt.Errorf("%w: %s", errors.NewStackTracedError(errors.ErrTLSInvalid), errString)
	}

	return cacert, chain, nil
}

// getTLSClientDataStandard get TLS client configuration using standard data layout.
func (c *Cluster) getTLSClientDataStandard() ([]byte, []byte, error) {
	if c.cluster.Spec.Networking.TLS == nil {
		return nil, nil, fmt.Errorf("%w: TLS not defined", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	if c.cluster.Spec.Networking.TLS.SecretSource == nil {
		return nil, nil, fmt.Errorf("%w: TLS source not defined", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	secret, ok := c.k8s.Secrets.Get(c.cluster.Spec.Networking.TLS.SecretSource.ClientSecretName)
	if !ok {
		return nil, nil, fmt.Errorf("%w: unable to get TLS secret %s", errors.NewStackTracedError(errors.ErrResourceRequired), c.cluster.Spec.Networking.TLS.SecretSource.ClientSecretName)
	}

	cert, ok := secret.Data["tls.crt"]
	if !ok {
		return nil, nil, fmt.Errorf("%w: TLS secret missing tls.crt", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	key, ok := secret.Data["tls.key"]
	if !ok {
		return nil, nil, fmt.Errorf("%w: TLS secret missing tls.key", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	return cert, key, nil
}

// getTLSClientDataLegacy gets TLS client configuration using made-up, proprietary layout.
func (c *Cluster) getTLSClientDataLegacy() ([]byte, []byte, error) {
	if c.cluster.Spec.Networking.TLS == nil {
		return nil, nil, fmt.Errorf("%w: TLS not defined", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	if c.cluster.Spec.Networking.TLS.Static == nil {
		return nil, nil, fmt.Errorf("%w: static TLS not defined", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	// Load the TLS data from kubernetes.
	operatorSecret, found := c.k8s.Secrets.Get(c.cluster.Spec.Networking.TLS.Static.OperatorSecret)
	if !found {
		return nil, nil, fmt.Errorf("%w: unable to get operator secret %s", errors.NewStackTracedError(errors.ErrResourceRequired), c.cluster.Spec.Networking.TLS.Static.OperatorSecret)
	}

	chain, ok := operatorSecret.Data[tlsOperatorSecretCert]
	if !ok {
		return nil, nil, fmt.Errorf("%w: operator secret missing %s", errors.NewStackTracedError(errors.ErrResourceAttributeRequired), tlsOperatorSecretCert)
	}

	key, ok := operatorSecret.Data[tlsOperatorSecretKey]
	if !ok {
		return nil, nil, fmt.Errorf("%w: operator secret missing %s", errors.NewStackTracedError(errors.ErrResourceAttributeRequired), tlsOperatorSecretKey)
	}

	return chain, key, nil
}

// getTLSClientData returns the PEM files required for client authentication.
func (c *Cluster) getTLSClientData() ([]byte, []byte, error) {
	if c.cluster.IsTLSShadowed() {
		return c.getTLSClientDataStandard()
	}

	return c.getTLSClientDataLegacy()
}

func (c *Cluster) getVerifiedTLSClientData(cacert []byte) (chain []byte, key []byte, err error) {
	clientCert, clientKey, err := c.getTLSClientData()
	if err != nil {
		return nil, nil, err
	}

	if errs := util_x509.Verify(cacert, clientCert, clientKey, x509.ExtKeyUsageClientAuth, nil, !c.cluster.IsTLSShadowed()); len(errs) != 0 {
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

	if err := retryutil.RetryFor(time.Minute, callback); err != nil {
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

// reconcileNodeToNodeGetUpdatableMembers checks each member and returns a set of those
// whose N2N settings don't match the requested on/off state.
func (c *Cluster) reconcileNodeToNodeGetUpdatableMembers(requestedEncryption bool) (couchbaseutil.MemberSet, error) {
	updatableMembers := couchbaseutil.NewMemberSet()

	for _, m := range c.members {
		s := &couchbaseutil.NodeNetworkConfiguration{}
		if err := c.getNodeNetworkConfiguration(m, s); err != nil {
			return nil, err
		}

		if (s.NodeEncryption == couchbaseutil.On) != requestedEncryption {
			updatableMembers.Add(m)
		}
	}

	return updatableMembers, nil
}

// reconcileNodeToNodeSetControlPlaneOnly changes the cluster N2N configuration to control plane only,
// which is necessary for certain things to succeed e.g. you cannot just disable entryption, you need
// to gradually reduce security before switching off wth the per-node configuration.
func (c *Cluster) reconcileNodeToNodeSetControlPlaneOnly(requestedEncryption bool) error {
	if requestedEncryption {
		return nil
	}

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

	return nil
}

// reconcileNodeToNodeUpdateMembers turns N2N excryption on/off across all nodes in the cluster.
func (c *Cluster) reconcileNodeToNodeUpdateMembers(requestedEncryption bool, updatableMembers couchbaseutil.MemberSet) error {
	if updatableMembers.Empty() {
		return nil
	}

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

	return nil
}

// reconcileNodeToNode turns node-to-node encryption on/off.
func (c *Cluster) reconcileNodeToNode(requestedEncryption bool) error {
	if !c.supportsNodeToNode() {
		return nil
	}

	// See if any nodes are in the wrong state.
	updatableMembers, err := c.reconcileNodeToNodeGetUpdatableMembers(requestedEncryption)
	if err != nil {
		// This is a soft error, caused by various external conditions.  Once topology is
		// sorted out it will start working again.
		log.Info("failed to get node network configuration", "cluster", c.namespacedName(), "error", err)
		return nil
	}

	// If we are disabling encryption then we need to set the mode to control plane only first...
	if err := c.reconcileNodeToNodeSetControlPlaneOnly(requestedEncryption); err != nil {
		return err
	}

	// Modify encryption settings for each node.
	if err := c.reconcileNodeToNodeUpdateMembers(requestedEncryption, updatableMembers); err != nil {
		return err
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

// updateSecuritySettings updates network security settings other than node-to-node.
func (c *Cluster) updateSecuritySettings() error {
	securitySettings := &couchbaseutil.SecuritySettings{}
	if err := couchbaseutil.GetSecuritySettings(securitySettings).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	requestedSecuritySettings := &couchbaseutil.SecuritySettings{
		DisableUIOverHTTP:      c.cluster.Spec.Networking.DisableUIOverHTTP,
		DisableUIOverHTTPS:     c.cluster.Spec.Networking.DisableUIOverHTTPS,
		TLSMinVersion:          couchbaseutil.TLS12,
		HonorCipherOrder:       true, // This is plain stupid, I'm hard coding it for the good of humanity.
		ClusterEncryptionLevel: securitySettings.ClusterEncryptionLevel,
	}

	if c.cluster.Spec.Networking.TLS != nil {
		var tlsVersion couchbaseutil.TLSVersion

		switch c.cluster.Spec.Networking.TLS.TLSMinimumVersion {
		case couchbasev2.TLS10:
			tlsVersion = couchbaseutil.TLS10
		case couchbasev2.TLS11:
			tlsVersion = couchbaseutil.TLS11
		case couchbasev2.TLS12:
			tlsVersion = couchbaseutil.TLS12
		}

		requestedSecuritySettings.TLSMinVersion = tlsVersion
		requestedSecuritySettings.CipherSuites = c.cluster.Spec.Networking.TLS.CipherSuites
	}

	// As per usual, normalize nil/empty arrays...
	if len(securitySettings.CipherSuites) == 0 {
		securitySettings.CipherSuites = nil
	}

	if len(requestedSecuritySettings.CipherSuites) == 0 {
		requestedSecuritySettings.CipherSuites = nil
	}

	if reflect.DeepEqual(securitySettings, requestedSecuritySettings) {
		return nil
	}

	if err := couchbaseutil.SetSecuritySettings(requestedSecuritySettings).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	c.raiseEvent(k8sutil.SecuritySettingsUpdatedEvent(c.cluster, k8sutil.SecuritySettingUpdated))

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

	// Security settings are updated independently of node-to-node due to ordering
	// constraints of the latter.
	if err := c.updateSecuritySettings(); err != nil {
		return err
	}

	return nil
}

// supportsNodeToNode tells us whether the current version supports N2N encryption.
func (c *Cluster) supportsNodeToNode() bool {
	tag, err := k8sutil.CouchbaseVersion(c.cluster.Spec.Image)
	if err != nil {
		return false
	}

	version, err := couchbaseutil.NewVersion(tag)
	if err != nil {
		return false
	}

	return version.GreaterEqualString("6.5.1")
}

// nodeToNodeEnabled tells us whether N2N encyption is enabled.
func (c *Cluster) nodeToNodeEnabled() bool {
	return c.cluster.Spec.Networking.TLS != nil && c.cluster.Spec.Networking.TLS.NodeToNodeEncryption != nil
}
