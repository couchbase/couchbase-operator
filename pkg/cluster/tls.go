package cluster

import (
	"context"
	"crypto/x509"
	"fmt"
	"reflect"
	"strings"
	"time"

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
	if err := couchbaseutil.GetClusterCACert(oldcacert).InPlaintext().On(c.api, member); err != nil {
		return err
	}

	if !reflect.DeepEqual(cacert, oldcacert) {
		log.Info("Reloading CA certificate", "cluster", c.namespacedName(), "name", member.Name)

		if err := couchbaseutil.SetClusterCACert(cacert).InPlaintext().On(c.api, member); err != nil {
			return err
		}
	}

	return nil
}

// reloadChain does an insecure reload of the TLS certificates and keys.
func (c *Cluster) reloadChain(member couchbaseutil.Member) error {
	return couchbaseutil.ReloadNodeCert().InPlaintext().On(c.api, member)
}

// reloadChainAndVerify reloads the certificate chain for a member when necessary,
// waiting until the certificate is presented by the server.
func (c *Cluster) reloadChainAndVerify(member couchbaseutil.Member, cacert, clientCert, clientKey []byte, cert *x509.Certificate) error {
	log.Info("Reloading certificate chain", "cluster", c.namespacedName(), "name", member.Name)

	// Wait for the certificate data to be updated. NS server has a few quirks (as per usual... sigh).
	// Reloading the chain will sometimes not work and need to be repeatedly prodded until it decides
	// to obey our command.  It will also take a few tries to verify.
	callback := func() error {
		if err := c.reloadChain(member); err != nil {
			return err
		}

		if !tlsValid(member, cacert, clientCert, clientKey, cert) {
			return fmt.Errorf("%w: certificate chain not served", errors.ErrCouchbaseServerError)
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
		err = fmt.Errorf("%w: unable to get operator secret %s", errors.ErrResourceRequired, c.cluster.Spec.Networking.TLS.Static.OperatorSecret)
		return
	}

	serverSecret, found := c.k8s.Secrets.Get(c.cluster.Spec.Networking.TLS.Static.ServerSecret)
	if !found {
		err = fmt.Errorf("%w: unable to get server secret %s", errors.ErrResourceRequired, c.cluster.Spec.Networking.TLS.Static.ServerSecret)
		return
	}

	// Ensure that the secrets are correctly formatted.
	var ok bool

	ca, ok = operatorSecret.Data[tlsOperatorSecretCACert]
	if !ok {
		err = fmt.Errorf("%w: operator secret missing ca.crt", errors.ErrResourceAttributeRequired)
		return
	}

	key, ok = serverSecret.Data["pkey.key"]
	if !ok {
		err = fmt.Errorf("%w: server secret missing pkey.key", errors.ErrResourceAttributeRequired)
		return
	}

	chain, ok = serverSecret.Data["chain.pem"]
	if !ok {
		err = fmt.Errorf("%w: server secret missing chain.pem", errors.ErrResourceAttributeRequired)
		return
	}

	return
}

// getTLSClientData returns the PEM files required for client authentication.
func (c *Cluster) getTLSClientData() (chain []byte, key []byte, err error) {
	// Load the TLS data from kubernetes.
	operatorSecret, found := c.k8s.Secrets.Get(c.cluster.Spec.Networking.TLS.Static.OperatorSecret)
	if !found {
		err = fmt.Errorf("%w: unable to get operator secret %s", errors.ErrResourceRequired, c.cluster.Spec.Networking.TLS.Static.OperatorSecret)
		return
	}

	var ok bool

	chain, ok = operatorSecret.Data[tlsOperatorSecretCert]
	if !ok {
		err = fmt.Errorf("%w: operator secret missing %s", errors.ErrResourceAttributeRequired, tlsOperatorSecretCert)
		return
	}

	key, ok = operatorSecret.Data[tlsOperatorSecretKey]
	if !ok {
		err = fmt.Errorf("%w: operator secret missing %s", errors.ErrResourceAttributeRequired, tlsOperatorSecretKey)
		return
	}

	return
}

// reconcileMemberTLS reconciles both the CA and certificate chain on Couchbase server.
// This is done in plain text due to races involving required mTLS.
func (c *Cluster) reconcileMemberTLS(member couchbaseutil.Member, ca, cert, key []byte, leaf *x509.Certificate) (bool, error) {
	// Try connect to the target node, if it doesn't respond we assume it's
	// deleted or the admin service has gone down and needs a reconcile to fix it.
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()

	if err := netutil.WaitForHostPort(ctx, member.GetHostPort()); err != nil {
		return false, nil
	}

	// By default we use the most up to date certificates, as the whole CA may
	// have been rotated so we need those client certs to perform the TLS handshake,
	// when mandatory mTLS is enabled. If however we are disabling mTLS entirely but
	// not rotating, then we need to use the existing client cert or the check will
	// fail.
	if tls := c.api.GetTLS(); cert == nil && tls != nil && tls.ClientAuth != nil {
		cert = tls.ClientAuth.Cert
		key = tls.ClientAuth.Key
	}

	if tlsValid(member, ca, cert, key, leaf) {
		return false, nil
	}

	// Reload the CA certificate if necessary.
	if err := c.reloadCA(member, ca); err != nil {
		return false, err
	}

	// If the pods doesn't have TLS enabled then ignore it.
	pod, found := c.k8s.Pods.Get(member.Name())
	if !found {
		return false, nil
	}

	if _, ok := pod.Annotations[constants.PodTLSAnnotation]; !ok {
		return false, nil
	}

	// Reload the server certificate chain.
	if err := c.reloadChainAndVerify(member, ca, cert, key, leaf); err != nil {
		return false, err
	}

	// Indicate something happened for raising events.
	return true, nil
}

// reconcileClientAuthentication reconciles client ceritifcate policy.  Note this must be
// done over plaintext or the logic would become next to impossible to comprehend.
func (c *Cluster) reconcileClientAuthentication(members couchbaseutil.MemberSet) error {
	// Reconcile client ceritifcate policy. Defaults to disable (implied by nil policy).
	settings := &couchbaseutil.ClientCertAuth{
		State:    "disable",
		Prefixes: []couchbaseutil.ClientCertAuthPrefix{}, // *sigh* it must be specified and not null
	}

	if c.cluster.Spec.Networking.TLS.ClientCertificatePolicy != nil {
		settings.State = string(*c.cluster.Spec.Networking.TLS.ClientCertificatePolicy)
		for _, path := range c.cluster.Spec.Networking.TLS.ClientCertificatePaths {
			settings.Prefixes = append(settings.Prefixes, couchbaseutil.ClientCertAuthPrefix{
				Path:      path.Path,
				Prefix:    path.Prefix,
				Delimiter: path.Delimiter,
			})
		}
	}

	// GetClientCertAuth/SetClientCertAuth allow reconciliation of TLS settings.
	// The ordering constraints when using mTLS make the process very messy, so
	// this is always done over HTTP.
	existingSettings := &couchbaseutil.ClientCertAuth{}
	if err := couchbaseutil.GetClientCertAuth(existingSettings).InPlaintext().On(c.api, members); err != nil {
		return err
	}

	if !reflect.DeepEqual(existingSettings, settings) {
		if err := couchbaseutil.SetClientCertAuth(settings).InPlaintext().On(c.api, members); err != nil {
			return err
		}

		// These settings are ACCEPTED and take some time to apply, so wait until they are
		// live, lest we get non-determinism.
		callback := func() error {
			currentSettings := &couchbaseutil.ClientCertAuth{}
			if err := couchbaseutil.GetClientCertAuth(currentSettings).InPlaintext().On(c.api, members); err != nil {
				return err
			}

			if !reflect.DeepEqual(currentSettings, settings) {
				return fmt.Errorf("%w: client TLS not reconciled", errors.ErrCouchbaseServerError)
			}

			return nil
		}

		ctx, cancel := context.WithTimeout(c.ctx, time.Minute)
		defer cancel()

		if err := retryutil.RetryOnErr(ctx, time.Second, callback); err != nil {
			return err
		}

		c.raiseEvent(k8sutil.ClusterSettingsEditedEvent("client authentication", c.cluster))
	}

	return nil
}

// reconcileTLS performs any certificate rotations that are necessary.
// We perform all of the rotations in plain text as this would become a
// nightmare with required mTLS.  We always ensure TLS client settings
// are enforced on exit from this function as we don't ever want to
// leak sensitive information.
func (c *Cluster) reconcileTLS(members couchbaseutil.MemberSet) error {
	// Nothing to do.
	if members.Empty() {
		return nil
	}

	// Insecure cluster, ignore.
	if !c.cluster.IsTLSEnabled() {
		return nil
	}

	// Load server TLS data from kubernetes and verify.
	cacert, chain, key, err := c.getTLSData()
	if err != nil {
		c.raiseEventCached(k8sutil.TLSInvalidEvent(c.cluster))
		return err
	}

	subjectAltNames := util_x509.MandatorySANs(c.cluster.Name, c.cluster.Namespace)

	if c.cluster.Spec.Networking.DNS != nil {
		subjectAltNames = append(subjectAltNames, "*."+c.cluster.Spec.Networking.DNS.Domain)
	}

	if errs := util_x509.Verify(cacert, chain, key, x509.ExtKeyUsageServerAuth, subjectAltNames); len(errs) != 0 {
		c.raiseEventCached(k8sutil.TLSInvalidEvent(c.cluster))

		errStrings := []string{}

		for _, err := range errs {
			errStrings = append(errStrings, err.Error())
		}

		errString := strings.Join(errStrings, ", ")

		return fmt.Errorf("%w: %s", errors.ErrTLSInvalid, errString)
	}

	// Create a new client TLS configuration.
	tls := &couchbaseutil.TLSAuth{
		CACert: cacert,
	}

	// If client authentication is specified load and verify those certificates.
	var clientCert []byte

	var clientKey []byte

	if c.cluster.Spec.Networking.TLS.ClientCertificatePolicy != nil {
		// Load client TLS data from kubernetes and verify.
		clientCert, clientKey, err = c.getTLSClientData()
		if err != nil {
			c.raiseEventCached(k8sutil.ClientTLSInvalidEvent(c.cluster))
			return err
		}

		if errs := util_x509.Verify(cacert, clientCert, clientKey, x509.ExtKeyUsageClientAuth, nil); len(errs) != 0 {
			c.raiseEventCached(k8sutil.ClientTLSInvalidEvent(c.cluster))

			errStrings := []string{}

			for _, err := range errs {
				errStrings = append(errStrings, err.Error())
			}

			errString := strings.Join(errStrings, ", ")

			return fmt.Errorf("%w: %s", errors.ErrTLSInvalid, errString)
		}

		// Update the TLS client configuration.
		tls.ClientAuth = &couchbaseutil.TLSClientAuth{
			Cert: clientCert,
			Key:  clientKey,
		}
	}

	// Parse the certificate chain.
	chainPem := util_x509.DecodePEM(chain)

	cert, err := x509.ParseCertificate(chainPem[0].Bytes)
	if err != nil {
		return err
	}

	// Quiesce persistent connections, NS server doesn't quite work if some are
	// still open.
	c.api.CloseIdleConnections()

	// Update the CA and any server certificate chains that require it.
	changed := false

	for _, member := range members {
		change, err := c.reconcileMemberTLS(member, cacert, clientCert, clientKey, cert)
		if err != nil {
			return err
		}

		changed = changed || change
	}

	// Finally if we did anything raise an event.
	if changed {
		c.raiseEvent(k8sutil.TLSUpdatedEvent(c.cluster))
	}

	// Update the client if necessary.  This must be done after member reconciliation as
	// we need a reference to the old TLS client certs using this process.  They are not
	// interrogated when no longer required as per the client code above.
	if !reflect.DeepEqual(tls, c.api.GetTLS()) {
		c.api.SetTLS(tls)
		log.Info("Reloading TLS client configuration")
		c.raiseEvent(k8sutil.ClientTLSUpdatedEvent(c.cluster))
	}

	// Reconcile client ceritifcate policy.
	if err := c.reconcileClientAuthentication(members); err != nil {
		return err
	}

	return nil
}
