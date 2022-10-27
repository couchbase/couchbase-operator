package cluster

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"reflect"
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

// tlsCache allows semi-atomic views of TLS updates.  Essentially weird things
// happen if we read from the main caches and things change as we go through
// the reconcile process.  By having an atomic cache that exists for the duration
// or a reconcile cycle, this reduces race conditions and provides a better UX.
type tlsCache struct {
	// rootCAs is the cluster CA and any additional ones required for
	// client authentication, rotation etc.
	rootCAs [][]byte

	// serverCA is the cluster CA.
	serverCA []byte

	// serverCert is the server certificate/chain.
	serverCert []byte

	// serverKey is the server key.
	serverKey []byte

	// clientCert is the operator client certificate (optional).
	clientCert []byte

	// clientKey is the operator client key (optional).
	clientKey []byte

	// publicPassphraseKey is used to encrypt the passphrase before sending to server
	publicPassphraseKey *rsa.PublicKey
}

// initTLSCache populates the TLS cache if TLS is enabled, and attaches it to
// the cluster object.
func (c *Cluster) initTLSCache() error {
	if !c.cluster.IsTLSEnabled() {
		return nil
	}

	rootCAs, err := c.getCAs()
	if err != nil {
		c.raiseEventCached(k8sutil.TLSInvalidEvent(c.cluster))
		return err
	}

	serverCA, serverCert, serverKey, err := c.getVerifiedServerTLSData(rootCAs)
	if err != nil {
		c.raiseEventCached(k8sutil.TLSInvalidEvent(c.cluster))
		return err
	}

	cache := &tlsCache{
		rootCAs:    rootCAs,
		serverCA:   serverCA,
		serverCert: serverCert,
		serverKey:  serverKey,
	}

	if c.cluster.IsMutualTLSEnabled() {
		clientCert, clientKey, err := c.getVerifiedTLSClientData(rootCAs)
		if err != nil {
			c.raiseEventCached(k8sutil.ClientTLSInvalidEvent(c.cluster))
			return err
		}

		cache.clientCert = clientCert
		cache.clientKey = clientKey
	}

	c.tlsCache = cache

	return nil
}

// getCAs abstracts away the collection of CAs.  Before Couchbase server 7.1, only one
// was allowed, and these were provided to the API with the server cert.
func (c *Cluster) getCAs() ([][]byte, error) {
	var rootCAs [][]byte

	// When using shadowed secrets (e.g. cert-manager mode), we optionally allow
	// the use of the provided (but non standard) ca.crt key.  When in legacy
	// mode the CA must be passed in with the operator secret.
	ca, err := c.getExplcitCA()
	if err != nil {
		return nil, err
	}

	if ca != nil {
		rootCAs = append(rootCAs, ca)
	}

	// When in shadowed mode, you can supply the CA separately when in shadowed
	// mode, or even additional CAs when your clients are signed by a different
	// CA etc.
	for _, name := range c.cluster.Spec.Networking.TLS.RootCAs {
		secret, ok := c.k8s.Secrets.Get(name)
		if !ok {
			return nil, fmt.Errorf("%w: unable to get TLS CA secret %s", errors.NewStackTracedError(errors.ErrResourceRequired), name)
		}

		ca, ok := secret.Data[corev1.TLSCertKey]
		if !ok {
			return nil, fmt.Errorf("%w: TLS CA secret missing tls.crt", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
		}

		rootCAs = append(rootCAs, ca)
	}

	// The assumption is that this call will be made if TLS is enabled, and in
	// doing so we need to be provided with at least one CA (to verify the
	// server certificate).
	if len(rootCAs) == 0 {
		return nil, fmt.Errorf("%w: No TLS CA certificates detected", errors.NewStackTracedError(errors.ErrResourceRequired))
	}

	// Finally we need to process each CA by
	// decoding to separate multi PEM certificates
	var processedRootCAs [][]byte

	for _, ca := range rootCAs {
		caPem := util_x509.DecodePEM(ca)
		for _, caBlock := range caPem {
			// Then re-encode and append to list of known CAs
			caPem := pem.EncodeToMemory(caBlock)
			processedRootCAs = append(processedRootCAs, caPem)
		}
	}

	return processedRootCAs, nil
}

// refreshTLSClientSecret creates/updates a secret that contains tls certificates
// for use by sidecars and applications.  This method fetches known tls configuration
// from tlsCache which alleviates the need to evaluate which tls model is in use and
// having to inspect underlying Kubernetes Secrets.
func (c *Cluster) refreshTLSClientSecret() error {
	if !c.cluster.IsTLSEnabled() {
		return nil
	}

	name := k8sutil.ClientTLSSecretName(c.cluster)
	requestedClientSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: k8sutil.LabelsForCluster(c.cluster),
			OwnerReferences: []metav1.OwnerReference{
				c.cluster.AsOwner(),
			},
		},
		Data: map[string][]byte{},
	}

	// Bundle all known CA's into a single certificate
	// beginning with the known server CA.
	caBundle := c.tlsCache.serverCA

	for _, ca := range c.tlsCache.rootCAs {
		if !reflect.DeepEqual(ca, c.tlsCache.serverCA) {
			caBundle = append(caBundle, ca...)
		}
	}

	// Set secret CA and cert/key to allow clients to easily
	// host secure services on same network as couchbase.
	requestedClientSecret.Data[constants.ClientSecretRootCA] = caBundle
	requestedClientSecret.Data[constants.ClientSecretServerCert] = c.tlsCache.serverCert
	requestedClientSecret.Data[constants.ClientSecretServerKey] = c.tlsCache.serverKey
	// Provide client cert/keys when mtls is enabled so allow
	// applications to communitcate securely with Couchbase.
	if c.cluster.IsMutualTLSEnabled() {
		requestedClientSecret.Data[constants.ClientSecretMutualCert] = c.tlsCache.clientCert
		requestedClientSecret.Data[constants.ClientSecretMutualKey] = c.tlsCache.clientKey
	}

	return c.reconcileTLSSecrets(requestedClientSecret)
}

// refreshTLSShadowCASecret creates/updates a shadow secret that contains one
// or more CAs used by CBS for TLS verification.  This is a 7.1+ only feature
// as those versions require the CAs to reside on disk, rather than be posted
// over HTTP with legacy versions.  The shadow secret must always exist on
// a 7.1+ cluster as we may need to install the CA on a non-TLS pod in order
// to upgrade.
func (c *Cluster) refreshTLSShadowCASecret() error {
	ok, err := c.IsAtLeastVersion("7.1.0")
	if err != nil {
		return err
	}

	if !ok {
		return nil
	}

	name := k8sutil.ShadowTLSCASecretName(c.cluster)

	requestedShadowSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: k8sutil.LabelsForCluster(c.cluster),
			OwnerReferences: []metav1.OwnerReference{
				c.cluster.AsOwner(),
			},
		},
		Data: map[string][]byte{},
	}

	if c.cluster.IsTLSEnabled() {
		for i, ca := range c.tlsCache.rootCAs {
			requestedShadowSecret.Data[fmt.Sprintf("ca%d.crt", i)] = ca
		}
	}

	return c.reconcileTLSSecrets(requestedShadowSecret)
}

// refreshTLSShadowSecret does what it says, it keeps a shadow version of the TLS
// secret up to date.  Why you ask?  Well CBS is crap and requires the files be
// called chain.pem and pkey.key, whereas our users require it to be whatever they
// want it to be, so we need to change the names for them.
func (c *Cluster) refreshTLSShadowSecret() error {
	if !c.cluster.IsTLSShadowed() {
		return nil
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
			"chain.pem": c.tlsCache.serverCert,
			"pkey.key":  c.tlsCache.serverKey,
		},
	}

	// Pa's special sauce!  Support PKCS#8 keys, because we can.
	block, _ := pem.Decode(c.tlsCache.serverKey)
	if block == nil {
		return fmt.Errorf("%w: private key not in PEM format", errors.NewStackTracedError(errors.ErrPrivateKeyInvalid))
	}

	switch block.Type {
	case "RSA PRIVATE KEY":
		// PKCS#1, in the right format.
		break
	case "ENCRYPTED PRIVATE KEY":
		// Encrypted Key requires server 7.1.0 or higher
		ok, err := c.RunningVersionIsAtLeast("7.1.0")
		if err != nil {
			return errors.NewStackTracedError(err)
		}

		if !ok {
			return fmt.Errorf("%w: encrypted private key requires server version 7.1.0", errors.NewStackTracedError(errors.ErrPrivateKeyInvalid))
		}

		break
	case "PRIVATE KEY":
		// PKCS#8, way more modern and widely used, but needs conversion
		// if server version is lower than 7.1.
		ok, err := c.RunningVersionIsAtLeast("7.1.0")
		if err != nil {
			return errors.NewStackTracedError(err)
		}

		if ok {
			// no need to convert unencrypted PKCS#8 for server 7.1.0
			break
		}

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

	return c.reconcileTLSSecrets(requestedShadowSecret)
}

// Reconciles TLS secrets by keeping the current state in sync with with incomming requests.
func (c *Cluster) reconcileTLSSecrets(requestedSecret *corev1.Secret) error {
	// Look the secret, if it exists update it, otherwise create it.
	currentSecret, ok := c.k8s.Secrets.Get(requestedSecret.Name)
	if !ok {
		log.Info(fmt.Sprintf("Creating TLS secret `%s`", requestedSecret.Name), "cluster", c.namespacedName())

		if _, err := c.k8s.KubeClient.CoreV1().Secrets(c.cluster.Namespace).Create(context.Background(), requestedSecret, metav1.CreateOptions{}); err != nil {
			return errors.NewStackTracedError(err)
		}

		return nil
	}

	// There is a difference between empty and nil in Go...
	if len(requestedSecret.Data) == 0 && len(currentSecret.Data) == 0 {
		return nil
	}

	if reflect.DeepEqual(requestedSecret.Data, currentSecret.Data) {
		return nil
	}

	log.Info(fmt.Sprintf("Updating TLS secret `%s`", requestedSecret.Name), "cluster", c.namespacedName())

	updatedSecret := currentSecret.DeepCopy()
	updatedSecret.Data = requestedSecret.Data

	if _, err := c.k8s.KubeClient.CoreV1().Secrets(c.cluster.Namespace).Update(context.Background(), updatedSecret, metav1.UpdateOptions{}); err != nil {
		return errors.NewStackTracedError(err)
	}

	// also refreshing the internal passphrase only when server certs are rotated to
	// prevent them from being a security risk in the event server was comprimized
	if c.cluster.IsTLSScriptPassphraseEnabled() {
		if requestedSecret.Name == k8sutil.ClientTLSSecretName(c.cluster) {
			if _, err := c.updateInternalPassphraseSecret(); err != nil {
				return errors.NewStackTracedError(err)
			}
		}
	}

	return nil
}

// refreshTLSPassphraseResources creates resources used by Operator and
// Couchbase Server for securely unlocking an encrypted private key.
// Specifically, the Operator needs a private/public key pair and
// Couchbase Server needs a copy of the private key along with a
// shell script (as ConfigMap) to invoke the private key as a means
// of decrypting incoming information that Operator has encrypted with
// the public key.
func (c *Cluster) refreshTLSPassphraseResources() error {
	if !c.cluster.IsTLSScriptPassphraseEnabled() {
		// currently only applies to script passphrase
		return nil
	}

	// update public/private key resources
	if err := c.refreshPassphrasePublicKey(); err != nil {
		return err
	}

	// update configmap script
	if err := c.refreshPassphraseConfigMap(); err != nil {
		return err
	}

	return nil
}

// refreshPassphrasePublicKey updates the cached public key used by Operator
// to encode the passphrase.  The public key is derived from private key.
func (c *Cluster) refreshPassphrasePublicKey() error {
	var err error

	name := k8sutil.PassphraseKeySecretName(c.cluster)
	secret, ok := c.k8s.Secrets.Get(name)

	if ok && !c.cluster.IsTLSEnabled() {
		// TLS is disabled remove internal Private key
		if err := c.k8s.KubeClient.CoreV1().Secrets(secret.Namespace).Delete(context.Background(), secret.Name, metav1.DeleteOptions{}); err != nil {
			return errors.NewStackTracedError(err)
		}

		return nil
	} else if !ok {
		// TLS is enabled and internal Private key doesn't
		// exist so create it
		secret, err = c.updateInternalPassphraseSecret()
		if err != nil {
			return errors.NewStackTracedError(err)
		}
	}

	// retrieve prviate key
	privateKeyBytes, ok := secret.Data[constants.CouchbaseTLSPassphraseKey]
	if !ok {
		return fmt.Errorf("%w: TLS Passphrase secret missing private key `%s`", errors.NewStackTracedError(errors.ErrResourceAttributeRequired), constants.CouchbaseTLSPassphraseKey)
	}

	privateKey, err := util_x509.ParsePrivateKey(privateKeyBytes)
	if err != nil {
		return errors.NewStackTracedError(err)
	}

	// derive corresponding publickey and cache
	c.tlsCache.publicPassphraseKey = privateKey.Public().(*rsa.PublicKey)

	return nil
}

// refreshPassphraseConfigMap creatse configmap script if it doesn't exist
// and deletes when TLS is disabled.
func (c *Cluster) refreshPassphraseConfigMap() error {
	name := k8sutil.PassphraseKeySecretName(c.cluster)
	configMap, ok := c.k8s.ConfigMaps.Get(name)

	if ok && !c.cluster.IsTLSEnabled() {
		// TLS is disabled remove configMap script
		if err := c.k8s.KubeClient.CoreV1().ConfigMaps(configMap.Namespace).Delete(context.Background(), configMap.Name, metav1.DeleteOptions{}); err != nil {
			return errors.NewStackTracedError(err)
		}

		return nil
	} else if !ok {
		// Creating the config map as a mountable script.
		// The script receives encoded passphrase as arg '$1'
		// and decrypts with mounted private key
		decoderScript := fmt.Sprintf("#!/bin/sh\necho $1 | base64 -d | openssl rsautl -decrypt -inkey /var/run/secrets/couchbase.com/couchbase-tls/%s", constants.CouchbaseTLSPassphraseKey)
		configMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: k8sutil.LabelsForCluster(c.cluster),
				OwnerReferences: []metav1.OwnerReference{
					c.cluster.AsOwner(),
				},
			},
			Data: map[string]string{
				constants.CouchbaseTLSPassphraseScript: decoderScript,
			},
		}

		log.Info(fmt.Sprintf("Creating TLS configmap `%s`", configMap.Name), "cluster", c.namespacedName())
		if _, err := c.k8s.KubeClient.CoreV1().ConfigMaps(c.cluster.Namespace).Create(context.Background(), configMap, metav1.CreateOptions{}); err != nil {
			return errors.NewStackTracedError(err)
		}
	}

	return nil
}

// updateInternalPassphraseSecret updates the prviate key used by server to decode the passphrase.
func (c *Cluster) updateInternalPassphraseSecret() (*corev1.Secret, error) {
	// create private key
	privateKey, err := util_x509.CreateRSAPrivateKey()
	if err != nil {
		return nil, errors.NewStackTracedError(err)
	}

	// generate internal secret and check for existence
	name := k8sutil.PassphraseKeySecretName(c.cluster)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: k8sutil.LabelsForCluster(c.cluster),
			OwnerReferences: []metav1.OwnerReference{
				c.cluster.AsOwner(),
			},
		},
		Data: map[string][]byte{
			constants.CouchbaseTLSPassphraseKey: privateKey,
		},
	}

	// create internal secret, otherwise update as it may have been rotated
	_, ok := c.k8s.Secrets.Get(secret.Name)
	if !ok {
		log.Info(fmt.Sprintf("Creating TLS secret `%s`", secret.Name), "cluster", c.namespacedName())

		if _, err := c.k8s.KubeClient.CoreV1().Secrets(c.cluster.Namespace).Create(context.Background(), secret, metav1.CreateOptions{}); err != nil {
			return nil, errors.NewStackTracedError(err)
		}
	} else {
		log.Info(fmt.Sprintf("Updating TLS secret `%s`", secret.Name), "cluster", c.namespacedName())

		if _, err := c.k8s.KubeClient.CoreV1().Secrets(c.cluster.Namespace).Update(context.Background(), secret, metav1.UpdateOptions{}); err != nil {
			return nil, errors.NewStackTracedError(err)
		}
	}

	return secret, nil
}

// getPrivateKeyPassphrase fetches the user provided passphrase key.
func (c *Cluster) getPrivateKeyPassphrase() ([]byte, error) {
	secret, ok := c.k8s.Secrets.Get(c.cluster.Spec.Networking.TLS.PassphraseConfig.Script.Secret)
	if !ok {
		return []byte{}, fmt.Errorf("%w: unable to get TLS Passphrase secret", errors.NewStackTracedError(errors.ErrResourceRequired))
	}

	passphrase, ok := secret.Data[constants.PassphraseSecretKey]
	if !ok {
		return []byte{}, fmt.Errorf("%w: TLS Passphrase secret doesn't not contain passphrase in key `%s`", errors.NewStackTracedError(errors.ErrResourceAttributeRequired), constants.PassphraseSecretKey)
	}

	return passphrase, nil
}

// tlsValid checks the members TLS is valid for the CA and the certificate leaf matches.
func tlsValid(member couchbaseutil.Member, cache *tlsCache, cert *x509.Certificate) bool {
	serverChain, err := netutil.GetTLSState(member.GetHostPortTLS(), cache.serverCA, cache.clientCert, cache.clientKey)
	if err == nil && serverChain[0].Equal(cert) {
		return true
	}

	return false
}

// reloadMemberCAs reloads the cluster CA certificates.
func (c *Cluster) reloadMemberCAs(member couchbaseutil.Member) error {
	ok, err := c.RunningVersionIsAtLeast("7.1.0")
	if err != nil {
		return err
	}

	if ok {
		return c.reloadMemberCAsNew(member)
	}

	return c.reloadMemberCALegacy(member)
}

// certificatesEqual accepts two PEM encoded certificates and compare them.
func certificatesEqual(a, b []byte) (bool, error) {
	cert1, err := util_x509.ParseCertificate(a)
	if err != nil {
		return false, err
	}

	cert2, err := util_x509.ParseCertificate(b)
	if err != nil {
		return false, err
	}

	return cert1.Equal(cert2), nil
}

// serverHasCA Checks to see if the CA is already present, the problem here is server
// may have done all kinds of things with the certificate, thus tainting our
// original input, so we cannot do a stright match, and we need to decode
// it.
func serverHasCA(serverCAs couchbaseutil.TrustedCAList, requestedCA []byte) (bool, error) {
	for _, serverCA := range serverCAs {
		ok, err := certificatesEqual(requestedCA, []byte(serverCA.PEM))
		if err != nil {
			return false, err
		}

		if ok {
			return true, nil
		}
	}

	return false, nil
}

// serverHasAllCAs tells us whether all requested CAs are installed on the member.
func (c *Cluster) serverHasAllCAs(member couchbaseutil.Member) (bool, error) {
	var serverCAs couchbaseutil.TrustedCAList

	if err := couchbaseutil.ListCAs(&serverCAs).On(c.api, member); err != nil {
		return false, err
	}

	for _, requestedCA := range c.tlsCache.rootCAs {
		ok, err := serverHasCA(serverCAs, requestedCA)
		if err != nil {
			return false, err
		}

		if !ok {
			return false, nil
		}
	}

	return true, nil
}

// apiHasCA tells us whether the CA is requested by the API.
func (c *Cluster) apiHasCA(serverCA []byte) (bool, error) {
	for _, requestedCA := range c.tlsCache.rootCAs {
		ok, err := certificatesEqual(requestedCA, serverCA)
		if err != nil {
			return false, err
		}

		if ok {
			return true, nil
		}
	}

	return false, nil
}

// reloadMemberCAsNew reloads the clusters CA certificate(s) for post 7.1 versions.
func (c *Cluster) reloadMemberCAsNew(member couchbaseutil.Member) error {
	// Check to see if the CA is already present.
	ok, err := c.serverHasAllCAs(member)
	if err != nil {
		return err
	}

	if ok {
		return nil
	}

	log.Info("Reloading CA certificates", "cluster", c.namespacedName(), "name", member.Name())

	// If node to node is enabled, then server will refuse to rotate TLS, for good reason,
	// so force disable it when performing TLS updates.
	if err := c.disableNodeToNode(); err != nil {
		return err
	}

	// Next annoyance is that while we can see the CA by reading the secret,
	// there is no guarantee server can yet, because of the delay kubelet
	// imposes when synchronizing secrets with tmpfs, so we have to wang this
	// in a retry loop until we can see it's installed.
	callback := func() error {
		if err := couchbaseutil.LoadCAs().On(c.api, member); err != nil {
			return err
		}

		ok, err := c.serverHasAllCAs(member)
		if err != nil {
			return err
		}

		if !ok {
			return fmt.Errorf("%w: expected CAs not present", errors.NewStackTracedError(errors.ErrTLSInvalid))
		}

		return nil
	}

	if err := retryutil.RetryFor(secretSyncTimePeriod, callback); err != nil {
		return err
	}

	return nil
}

// reloadMemberCALegacy reloads the cluster CA certificate for pre-7.1 versions.
func (c *Cluster) reloadMemberCALegacy(member couchbaseutil.Member) error {
	var oldcacert []byte
	if err := couchbaseutil.GetClusterCACert(&oldcacert).On(c.api, member); err != nil {
		return err
	}

	ok, err := certificatesEqual(c.tlsCache.serverCA, oldcacert)
	if err != nil {
		return err
	}

	if ok {
		return nil
	}

	log.Info("Reloading CA certificate", "cluster", c.namespacedName(), "name", member.Name())

	// If node to node is enabled, then server will refuse to rotate TLS, for good reason,
	// so force disable it when performing TLS updates.
	if err := c.disableNodeToNode(); err != nil {
		return err
	}

	if err := couchbaseutil.SetClusterCACert(c.tlsCache.serverCA).On(c.api, member); err != nil {
		return err
	}

	return nil
}

// updateCAs adds CAs if they don't exist only.  This must be called before
// any client or server certificate rotation so we don't get locked out or
// do some other stupid thing.  Old CAs will be cleared out later.
func (c *Cluster) updateCAs() error {
	if !c.cluster.IsTLSEnabled() {
		return nil
	}

	for _, member := range c.callableMembers {
		if err := c.reloadMemberCAs(member); err != nil {
			return err
		}
	}

	return nil
}

// cleanCAs removes any CAs from the trust pool that aren't required any more.
func (c *Cluster) cleanCAs() error {
	if !c.cluster.IsTLSEnabled() {
		return nil
	}

	// This is a 7.1+ feature only.
	ok, err := c.IsAtLeastVersion("7.1.0")
	if err != nil {
		return err
	}

	if !ok {
		return nil
	}

	// Grab a list of all CAs installed in Couchbase...
	var serverCAs couchbaseutil.TrustedCAList

	if err := couchbaseutil.ListCAs(&serverCAs).On(c.api, c.members); err != nil {
		return err
	}

	// For each CA, if it doesn't have a corresponding CA defined in the
	// cache, then it needs deleting from Couchbase.
	for _, serverCA := range serverCAs {
		ok, err := c.apiHasCA([]byte(serverCA.PEM))
		if err != nil {
			return err
		}

		if ok {
			continue
		}

		log.Info("Removing CA", "cluster", c.namespacedName(), "id", serverCA.ID, "subject", serverCA.Subject)

		if err := couchbaseutil.DeleteCA(serverCA.ID).On(c.api, c.members); err != nil {
			return err
		}
	}

	return nil
}

// reloadChain does a reload of the TLS certificates and keys.
func (c *Cluster) reloadChain(member couchbaseutil.Member) error {
	settings, err := c.passphraseSettings()
	if err != nil {
		return err
	}

	return couchbaseutil.ReloadNodeCert(settings).On(c.api, member)
}

// reloadChainAndVerify reloads the certificate chain for a member when necessary,
// waiting until the certificate is presented by the server.
func (c *Cluster) reloadChainAndVerify(member couchbaseutil.Member, cert *x509.Certificate) error {
	log.Info("Reloading certificate chain", "cluster", c.namespacedName(), "name", member.Name())

	// Wait for the certificate data to be updated. NS server has a few quirks (as per usual... sigh).
	// We need to keep retrying until the secret mount is updated by kubelet, then this will fail
	// due to a dirty shutdown of TLS.  So prioritize the end result over the retry or we will
	// get stuck.
	callback := func() error {
		if tlsValid(member, c.tlsCache, cert) {
			return nil
		}

		if err := c.reloadChain(member); err != nil {
			return err
		}

		if !tlsValid(member, c.tlsCache, cert) {
			return fmt.Errorf("%w: certificate chain not served", errors.NewStackTracedError(errors.ErrCouchbaseServerError))
		}

		return nil
	}

	if err := retryutil.RetryFor(secretSyncTimePeriod, callback); err != nil {
		return err
	}

	return nil
}

// getExplcitCAStandard gets the optional (i.e. the result can be nil) CA certificate
// from the kubernetes.io/tls secret, if it's provided exiplictly or implicitly by
// cert-manager.
func (c *Cluster) getExplcitCAStandard() ([]byte, error) {
	if c.cluster.Spec.Networking.TLS == nil {
		return nil, fmt.Errorf("%w: TLS not defined", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	if c.cluster.Spec.Networking.TLS.SecretSource == nil {
		return nil, fmt.Errorf("%w: TLS source not defined", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	secret, ok := c.k8s.Secrets.Get(c.cluster.Spec.Networking.TLS.SecretSource.ServerSecretName)
	if !ok {
		return nil, fmt.Errorf("%w: unable to get TLS secret %s", errors.NewStackTracedError(errors.ErrResourceRequired), c.cluster.Spec.Networking.TLS.SecretSource.ServerSecretName)
	}

	ca, ok := secret.Data[constants.CertManagerCAKey]
	if !ok {
		return nil, nil
	}

	return ca, nil
}

// getExplcitCALegacy gets the mandatory CA certificate from the bespoke secrets.
func (c *Cluster) getExplcitCALegacy() ([]byte, error) {
	if c.cluster.Spec.Networking.TLS == nil {
		return nil, fmt.Errorf("%w: TLS not defined", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	if c.cluster.Spec.Networking.TLS.Static == nil {
		return nil, fmt.Errorf("%w: static TLS not defined", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	// Load the TLS data from kubernetes.
	operatorSecret, found := c.k8s.Secrets.Get(c.cluster.Spec.Networking.TLS.Static.OperatorSecret)
	if !found {
		return nil, fmt.Errorf("%w: unable to get operator secret %s", errors.NewStackTracedError(errors.ErrResourceRequired), c.cluster.Spec.Networking.TLS.Static.OperatorSecret)
	}

	// Ensure that the secrets are correctly formatted.
	ca, ok := operatorSecret.Data[constants.OperatorSecretCAKey]
	if !ok {
		return nil, fmt.Errorf("%w: operator secret missing %s", errors.NewStackTracedError(errors.ErrResourceAttributeRequired), constants.OperatorSecretCAKey)
	}

	return ca, nil
}

// getExplcitCA gets the CA if it's explcictly provided along with the server cert/key pair.
// As the CA in shadowed mode is optional, then the result may be nil.
func (c *Cluster) getExplcitCA() ([]byte, error) {
	if c.cluster.IsTLSShadowed() {
		return c.getExplcitCAStandard()
	}

	return c.getExplcitCALegacy()
}

// getServerTLSDataStandard get TLS server configuration using standard data layout.
func (c *Cluster) getServerTLSDataStandard() ([]byte, []byte, error) {
	if c.cluster.Spec.Networking.TLS == nil {
		return nil, nil, fmt.Errorf("%w: TLS not defined", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	if c.cluster.Spec.Networking.TLS.SecretSource == nil {
		return nil, nil, fmt.Errorf("%w: TLS source not defined", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	secret, ok := c.k8s.Secrets.Get(c.cluster.Spec.Networking.TLS.SecretSource.ServerSecretName)
	if !ok {
		return nil, nil, fmt.Errorf("%w: unable to get TLS secret %s", errors.NewStackTracedError(errors.ErrResourceRequired), c.cluster.Spec.Networking.TLS.SecretSource.ServerSecretName)
	}

	cert, ok := secret.Data[corev1.TLSCertKey]
	if !ok {
		return nil, nil, fmt.Errorf("%w: TLS secret missing tls.crt", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	key, ok := secret.Data[corev1.TLSPrivateKeyKey]
	if !ok {
		return nil, nil, fmt.Errorf("%w: TLS secret missing tls.key", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	return cert, key, nil
}

// getServerTLSDataLegacy gets TLS server configuration using made-up, proprietary layout.
func (c *Cluster) getServerTLSDataLegacy() ([]byte, []byte, error) {
	if c.cluster.Spec.Networking.TLS == nil {
		return nil, nil, fmt.Errorf("%w: TLS not defined", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	if c.cluster.Spec.Networking.TLS.Static == nil {
		return nil, nil, fmt.Errorf("%w: static TLS not defined", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	// Load the TLS data from kubernetes.
	serverSecret, found := c.k8s.Secrets.Get(c.cluster.Spec.Networking.TLS.Static.ServerSecret)
	if !found {
		return nil, nil, fmt.Errorf("%w: unable to get server secret %s", errors.NewStackTracedError(errors.ErrResourceRequired), c.cluster.Spec.Networking.TLS.Static.ServerSecret)
	}

	// Ensure that the secrets are correctly formatted.
	key, ok := serverSecret.Data["pkey.key"]
	if !ok {
		return nil, nil, fmt.Errorf("%w: server secret missing pkey.key", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	chain, ok := serverSecret.Data["chain.pem"]
	if !ok {
		return nil, nil, fmt.Errorf("%w: server secret missing chain.pem", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	return chain, key, nil
}

// getServerTLSData gets the TLS data from kubernetes and performs some error checking.
func (c *Cluster) getServerTLSData() ([]byte, []byte, error) {
	if c.cluster.IsTLSShadowed() {
		return c.getServerTLSDataStandard()
	}

	return c.getServerTLSDataLegacy()
}

// getVerifiedServerTLSData is an extended version of getServerTLSData that performs certificate
// verification of tainted input.  Given it's possible to configure the cluster with no prior
// knowledge of which CA is used to verify the server certificate, we use the verification data
// to select the correct root CA to use for the HTTP client verification.
func (c *Cluster) getVerifiedServerTLSData(rootCAs [][]byte) ([]byte, []byte, []byte, error) {
	// Load server TLS data from kubernetes and verify.
	chain, key, err := c.getServerTLSData()
	if err != nil {
		return nil, nil, nil, err
	}

	subjectAltNames := util_x509.MandatorySANs(c.cluster.Name, c.cluster.Namespace)

	if c.cluster.Spec.Networking.DNS != nil {
		subjectAltNames = append(subjectAltNames, "*."+c.cluster.Spec.Networking.DNS.Domain)
	}

	chains, err := util_x509.Verify(rootCAs, chain, key, x509.ExtKeyUsageServerAuth, subjectAltNames, !c.cluster.IsTLSShadowed())
	if err != nil {
		return nil, nil, nil, err
	}

	// Verify returns chains starting from the leaf, to the the root.
	cacert, err := util_x509.CreateCertificate(chains[0][len(chains[0])-1].Raw)
	if err != nil {
		return nil, nil, nil, err
	}

	return cacert, chain, key, nil
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

	cert, ok := secret.Data[corev1.TLSCertKey]
	if !ok {
		return nil, nil, fmt.Errorf("%w: TLS secret missing tls.crt", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	key, ok := secret.Data[corev1.TLSPrivateKeyKey]
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

	chain, ok := operatorSecret.Data[constants.OperatorSecretClientCertKey]
	if !ok {
		return nil, nil, fmt.Errorf("%w: operator secret missing %s", errors.NewStackTracedError(errors.ErrResourceAttributeRequired), constants.OperatorSecretClientCertKey)
	}

	key, ok := operatorSecret.Data[constants.OperatorSecretPrivateKeyKey]
	if !ok {
		return nil, nil, fmt.Errorf("%w: operator secret missing %s", errors.NewStackTracedError(errors.ErrResourceAttributeRequired), constants.OperatorSecretPrivateKeyKey)
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

// getVerifiedTLSClientData returns the client certificate/key pair to be used by
// the Operator, after verifying it validates against the CA pool.
func (c *Cluster) getVerifiedTLSClientData(rootCAs [][]byte) (chain []byte, key []byte, err error) {
	clientCert, clientKey, err := c.getTLSClientData()
	if err != nil {
		return nil, nil, err
	}

	if _, err := util_x509.Verify(rootCAs, clientCert, clientKey, x509.ExtKeyUsageClientAuth, nil, !c.cluster.IsTLSShadowed()); err != nil {
		return nil, nil, err
	}

	return clientCert, clientKey, nil
}

// reconcileMemberTLS reconciles both the CA and certificate chain on Couchbase server.
// This is done in plain text due to races involving required mTLS.
func (c *Cluster) reconcileMemberTLS(member couchbaseutil.Member, leaf *x509.Certificate) error {
	// Try connect to the target node, if it doesn't respond we assume it's
	// deleted or the admin service has gone down and needs a reconcile to fix it.
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()

	if err := netutil.WaitForHostPort(ctx, member.GetHostPort()); err != nil {
		return nil
	}

	if tlsValid(member, c.tlsCache, leaf) {
		return nil
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
	if err := c.reloadChainAndVerify(member, leaf); err != nil {
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

	clientTLS = &couchbaseutil.TLSAuth{
		CACert: c.tlsCache.serverCA,
	}

	c.api.SetTLS(clientTLS)

	if err := c.state.Insert(persistence.CACertificate, string(c.tlsCache.serverCA)); err != nil {
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

	// Parse the certificate chain.
	chainPem := util_x509.DecodePEM(c.tlsCache.serverCert)

	cert, err := x509.ParseCertificate(chainPem[0].Bytes)
	if err != nil {
		return errors.NewStackTracedError(err)
	}

	// Quiesce persistent connections, NS server doesn't quite
	// work if some are still open.
	c.api.CloseIdleConnections()

	// Update the CA and any server certificate chains that require it.
	for _, member := range c.members {
		if err := c.reconcileMemberTLS(member, cert); err != nil {
			return err
		}
	}

	return c.updateClientCA()
}

func (c *Cluster) updateClientCA() error {
	clientTLS := c.api.GetTLS()

	newClientTLS := *clientTLS
	newClientTLS.CACert = c.tlsCache.serverCA

	if !reflect.DeepEqual(clientTLS, &newClientTLS) {
		log.Info("Reloading client CA certificate", "cluster", c.namespacedName())

		c.api.SetTLS(&newClientTLS)

		if err := c.state.Update(persistence.CACertificate, string(c.tlsCache.serverCA)); err != nil {
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

		clientTLS.ClientAuth = &couchbaseutil.TLSClientAuth{
			Cert: c.tlsCache.clientCert,
			Key:  c.tlsCache.clientKey,
		}

		c.api.SetTLS(clientTLS)

		if err := c.state.Insert(persistence.ClientCertificate, string(c.tlsCache.clientCert)); err != nil {
			return err
		}

		if err := c.state.Insert(persistence.ClientKey, string(c.tlsCache.clientKey)); err != nil {
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
	newClientTLS := *clientTLS
	newClientTLS.ClientAuth = &couchbaseutil.TLSClientAuth{
		Cert: c.tlsCache.clientCert,
		Key:  c.tlsCache.clientKey,
	}

	if !reflect.DeepEqual(clientTLS, &newClientTLS) {
		log.Info("Reloading client certificate", "cluster", c.namespacedName())

		// update both active client certs and the persistence state
		newClientTLS.ClientAuth.Key = c.tlsCache.clientKey
		newClientTLS.ClientAuth.Cert = c.tlsCache.clientCert
		c.api.SetTLS(&newClientTLS)

		if err := c.state.Update(persistence.ClientCertificate, string(c.tlsCache.clientCert)); err != nil {
			return err
		}

		if err := c.state.Update(persistence.ClientKey, string(c.tlsCache.clientKey)); err != nil {
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
	if err := couchbaseutil.GetClientCertAuth(existingSettings).On(c.api, c.callableMembers); err != nil {
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

	for _, m := range c.callableMembers {
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

	listenerSettings := &couchbaseutil.ListenerConfiguration{
		AddressFamily:  couchbaseutil.AddressFamilyIPV4,
		NodeEncryption: encryptionEnabledString,
	}

	networkSettings := &couchbaseutil.NodeNetworkConfiguration{
		AddressFamily:  couchbaseutil.AddressFamilyIPV4,
		NodeEncryption: encryptionEnabledString,
	}

	antiListenerSettings := &couchbaseutil.ListenerConfiguration{
		AddressFamily:  couchbaseutil.AddressFamilyIPV4,
		NodeEncryption: encryptionDisabledString,
	}

	// Update one API per node...
	for _, m := range updatableMembers {
		// The auto-failover settings may take a while to take effect, so retry
		// this call a few times.
		if err := couchbaseutil.EnableExternalListener(listenerSettings).RetryFor(time.Minute).On(c.api, m); err != nil {
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

		if err := couchbaseutil.DisableExternalListener(antiListenerSettings).RetryFor(time.Minute).On(c.api, m); err != nil {
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
	case couchbasev2.NodeToNodeStrict:
		requestedSecuritySettings.ClusterEncryptionLevel = couchbaseutil.ClusterEncryptionStrict
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
		DisableUIOverHTTP:       c.cluster.Spec.Networking.DisableUIOverHTTP,
		DisableUIOverHTTPS:      c.cluster.Spec.Networking.DisableUIOverHTTPS,
		UISessionTimeoutSeconds: couchbaseutil.UISessionTimeoutInt(c.cluster.Spec.UISessionTimeoutMinutes * 60),
		TLSMinVersion:           couchbaseutil.TLS12,
		HonorCipherOrder:        true, // This is plain stupid, I'm hard coding it for the good of humanity.
		ClusterEncryptionLevel:  securitySettings.ClusterEncryptionLevel,
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
		case couchbasev2.TLS13:
			tlsVersion = couchbaseutil.TLS13
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

	// When enabling TLS for legacy (pre 7.1) clusters, we need to have updated the
	// CA before adding in any new pods, otherwise the generated CA will get propagated
	// during engagement, overriding what we set it to on startup.
	if err := c.updateCAs(); err != nil {
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

	// Remove any unused CAs after everything else is successful, we risk locking
	// ourselves out otherwise.
	if err := c.cleanCAs(); err != nil {
		return err
	}

	return nil
}

// nodeToNodeEnabled tells us whether N2N encyption is enabled.
func (c *Cluster) nodeToNodeEnabled() bool {
	return c.cluster.IsTLSEnabled() && c.cluster.Spec.Networking.TLS.NodeToNodeEncryption != nil
}

// passphraseSettings gets the settings to use when retrieving passphrase to unlock node key cert.
// by default rest is used when url is provided otherwise attempt to get use script settings.
func (c *Cluster) passphraseSettings() (*couchbaseutil.PrivateKeyPassphraseSettings, error) {
	if c.cluster.IsTLSRestPassphraseEnabled() {
		return c.restPassphraseSettings()
	}

	if c.cluster.IsTLSScriptPassphraseEnabled() {
		return c.scriptPassphraseSettings()
	}

	return nil, nil
}

// restPassphraseSettings compiles parameters needed to reload node certs using a rest endpoint.
func (c *Cluster) restPassphraseSettings() (*couchbaseutil.PrivateKeyPassphraseSettings, error) {
	restPassphrase := couchbaseutil.PrivateKeyPassphrase{
		Type:          string(couchbasev2.PassphraseTypeRest),
		URL:           c.cluster.Spec.Networking.TLS.PassphraseConfig.Rest.URL,
		Headers:       c.cluster.Spec.Networking.TLS.PassphraseConfig.Rest.Headers,
		AddressFamily: c.cluster.Spec.Networking.TLS.PassphraseConfig.Rest.AddressFamily,
		Timeout:       c.cluster.Spec.Networking.TLS.PassphraseConfig.Rest.Timeout,
	}

	if !c.cluster.Spec.Networking.TLS.PassphraseConfig.Rest.VerifyPeer {
		restPassphrase.HTTPOpts = map[string]bool{
			"verifyPeer": false,
		}
	}

	settings := &couchbaseutil.PrivateKeyPassphraseSettings{PrivateKeyPassphrase: restPassphrase}

	return settings, nil
}

// scriptPassphraseSettings compiles parameters needed to reload node certs using a local script.
// The server Pod has the script mounted in a known path and simply needs the passphrase arg
// which is first encrypted and then passed along.
func (c *Cluster) scriptPassphraseSettings() (*couchbaseutil.PrivateKeyPassphraseSettings, error) {
	passphrase, err := c.getPrivateKeyPassphrase()
	if err != nil {
		return nil, errors.NewStackTracedError(err)
	}

	ciphertext, err := rsa.EncryptPKCS1v15(rand.Reader, c.tlsCache.publicPassphraseKey, passphrase)
	if err != nil {
		return nil, errors.NewStackTracedError(err)
	}

	// encoding the encrypted passphrase as base64 for better transport
	encodedPassphrase := base64.StdEncoding.EncodeToString(ciphertext)

	scriptPassphrase := couchbaseutil.PrivateKeyPassphrase{
		Type:    string(couchbasev2.PassphraseTypeScript),
		Path:    constants.CouchbaseTLSPassphraseScript,
		Args:    []string{encodedPassphrase},
		Trim:    true,
		Timeout: 500,
	}

	settings := &couchbaseutil.PrivateKeyPassphraseSettings{PrivateKeyPassphrase: scriptPassphrase}

	return settings, nil
}

func (c *Cluster) checkCertExpiration(cert []byte) bool {
	pem := util_x509.DecodePEM(cert)

	// currently only single pem block client certs are supported
	// but one day the operator may use a different cert per node
	// and so it's best to check if any cert in the chain has expired.
	for _, block := range pem {
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			log.Error(err, "Failed to parse TLS certificate ", "cluster", c.namespacedName())
			return false
		}

		if time.Now().After(cert.NotAfter) {
			return true
		}
	}

	return false
}

// shouldRotateExpiredRootCAs checks if the servers root CAs have expired
// and if we should take action to rotate newly available CAs.
func (c *Cluster) shouldRotateExpiredRootCAs() bool {
	if !c.cluster.Spec.Networking.TLS.AllowPlainTextCertReload {
		return false
	}

	// check if any of the root CAs have expired
	clientTLS := c.api.GetTLS()
	rootCAs := append(clientTLS.RootCAs, clientTLS.CACert)

	for _, ca := range rootCAs {
		if !c.checkCertExpiration(ca) {
			continue
		}

		if !reflect.DeepEqual(clientTLS.RootCAs, &c.tlsCache.rootCAs) {
			// Recommend rotation now that user has provided updated root CAs
			return true
		}

		log.Error(errors.ErrCertificateInvalid, "The root CAs have expired and must be updated before proceeding. ", "cluster", c.namespacedName())

		return false
	}

	return false
}

// rotateExpiredRootCAs ensures the new root CA is loaded onto a
// server Pod and subsequently loads the CA into Couchbase over
// PLAIN TEXT with auth credentials requires `allowPlainTextCertReload`.
func (c *Cluster) rotateExpiredRootCAs() error {
	// refresh the shadow ca since this is what
	// actually gets mounted inside the server pods.
	if err := c.refreshTLSShadowCASecret(); err != nil {
		return err
	}

	// the new CA is loaded onto the Pod and now
	// we need to load it into Couchbase
	for _, member := range c.callableMembers {
		ok, err := c.RunningVersionIsAtLeast("7.1.0")
		if err != nil {
			return err
		}

		var request *couchbaseutil.Request

		if ok {
			request = couchbaseutil.LoadCAs().InPlaintext()
		} else {
			request = couchbaseutil.SetClusterCACert(c.tlsCache.serverCA).InPlaintext()
		}

		// I've thought long and hard about my life
		request.Authenticate = true
		if err := request.On(c.api, member); err != nil {
			return err
		}
	}

	return nil
}

// shouldRotateExpiredServerCerts determines if the server cert should be rotated
// if the current in use server cert has expired and new certs are provided.
func (c *Cluster) shouldRotateExpiredServerCerts() bool {
	if !c.cluster.Spec.Networking.TLS.AllowPlainTextCertReload {
		return false
	}

	clientTLS := c.api.GetTLS()
	if c.checkCertExpiration(clientTLS.CACert) {
		if !reflect.DeepEqual(clientTLS.CACert, c.tlsCache.serverCA) {
			return true
		}

		log.Error(errors.ErrCertificateInvalid, "The Couchbase Server Certificate(s) have expired and must be updated before proceeding", "cluster", c.namespacedName())
	}

	return false
}

func (c *Cluster) rotateExpiredServerCerts() error {
	// refresh the shadow secret since this is what
	// actually gets mounted inside the server pods.
	if err := c.refreshTLSShadowSecret(); err != nil {
		return err
	}

	if err := c.refreshTLSPassphraseResources(); err != nil {
		return err
	}

	settings, err := c.passphraseSettings()
	if err != nil {
		return err
	}

	for _, member := range c.callableMembers {
		// close your eyes kids
		request := couchbaseutil.ReloadNodeCert(settings).InPlaintext()
		request.Authenticate = true

		if err := request.On(c.api, member); err != nil {
			return err
		}
	}

	return nil
}

// shouldRotateExpiredClientCerts checks the expiration date of the client cert
// used during mtls and returns true if the date preceds the present.
func (c *Cluster) shouldRotateExpiredClientCerts() bool {
	tls := c.api.GetTLS()
	if tls.ClientAuth != nil {
		return c.checkCertExpiration(tls.ClientAuth.Cert)
	}

	return false
}

// rotateExpiredClientCerts first refreshes the Operators connection with server
// then internally updates it's persisted state to use the newly provided certs.
// This workflow assumes the user eventually provides us new certs to use,
// otherwise there's really nothing we can do.
func (c *Cluster) rotateExpiredClientCerts() error {
	// If server root CA was rotated then we'll also need to update clients CA
	clientTLS := c.api.GetTLS()
	if !reflect.DeepEqual(clientTLS.CACert, c.tlsCache.serverCA) {
		if err := c.updateClientCA(); err != nil {
			return err
		}
	}

	// compare provided certs with existing and update client on change
	return c.updateMutualTLS()
}

// rotateExpiredCertificates attempts to rotate expired server and/or client certs.
// rotation begins with root CA, then server certs,  and finally the client.
func (c *Cluster) rotateExpiredCertificates() error {
	// Rotation relies on cluster having some TLS state already persisted.
	// However, this may not be the case if cluster is new or someone is
	// upgrading from an old release with expired cert and...well, that's just too bad.
	if c.api.GetTLS() == nil {
		return fmt.Errorf("%w: Attempted to check if certifiates are expired but TLS was never initialized", errors.NewStackTracedError(errors.ErrTLSInvalid))
	}

	// Re-init the tls cache as this will contain any new certs the user
	// is attempting to provide now that we're locked out of reconcile.
	if err := c.initTLSCache(); err != nil {
		return fmt.Errorf("%w: Failed to collect TLS configuration", err)
	}

	if c.shouldRotateExpiredRootCAs() {
		log.Info(fmt.Sprintf("Rotating expired Root CAs"), "cluster", c.namespacedName())

		if err := c.rotateExpiredRootCAs(); err != nil {
			return err
		}
	}

	if c.shouldRotateExpiredServerCerts() {
		log.Info(fmt.Sprintf("Rotating expired server certs"), "cluster", c.namespacedName())

		if err := c.rotateExpiredServerCerts(); err != nil {
			return err
		}
	}

	if c.shouldRotateExpiredClientCerts() {
		log.Info(fmt.Sprintf("Rotating expired client certs"), "cluster", c.namespacedName())

		if err := c.rotateExpiredClientCerts(); err != nil {
			return err
		}
	}

	return nil
}
