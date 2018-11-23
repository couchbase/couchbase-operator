package cluster

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"reflect"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/netutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/gocbmgr"
)

// decodePEM takes a raw blob of data and tries to decode PEM encoded data.
func decodePEM(data []byte) (blocks []*pem.Block) {
	for len(data) != 0 {
		var block *pem.Block
		if block, data = pem.Decode(data); block == nil {
			break
		}
		blocks = append(blocks, block)
	}
	return
}

// tlsValid checks the members TLS is valid for the CA and the certificate leaf matches.
func tlsValid(member *couchbaseutil.Member, ca []byte, cert *x509.Certificate) bool {
	serverChain, err := netutil.GetTLSState(member.HostURL(), ca)
	if err == nil && serverChain[0].Equal(cert) {
		return true
	}
	return false
}

// reloadCA insecurely reloads the cluster CA certificate.
func (c *Cluster) reloadCA(member *couchbaseutil.Member, cacert []byte) error {
	// Perform this insecurely but over TLS so as not to leak credentials.
	// This handles where the client is using an updated CA but the cluster
	// is still using certificates signed by an old one.
	tls := c.client.GetTLS()
	c.client.SetTLS(&cbmgr.TLSAuth{CACert: cacert, Insecure: true})
	defer c.client.SetTLS(tls)

	oldcacert, err := c.client.GetClusterCACert(member)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(cacert, oldcacert) {
		c.logger.Infof("Reloading CA certificate for member %s", member.Name)
		if err := c.client.UploadClusterCACert(member, cacert); err != nil {
			return err
		}
	}
	return nil
}

// reloadChain does an insecure reload of the TLS certificates and keys.
func (c *Cluster) reloadChain(member *couchbaseutil.Member, cacert []byte) error {
	// Perform this insecurely but over TLS so as not to leak credentials.
	// This handles where the client is using an updated CA but the cluster
	// is still using certificates signed by an old one.
	tls := c.client.GetTLS()
	c.client.SetTLS(&cbmgr.TLSAuth{CACert: cacert, Insecure: true})
	defer c.client.SetTLS(tls)

	if err := c.client.ReloadNodeCert(member); err != nil {
		return err
	}
	return nil
}

// reloadChainAndVerify reloads the certificate chain for a member when necessary,
// waiting until the certificate is presented by the server.
func (c *Cluster) reloadChainAndVerify(member *couchbaseutil.Member, cacert []byte, cert *x509.Certificate) error {
	c.logger.Infof("Reloading certificate chain for member %s", member.Name)

	// Refresh the server certificate chain.
	if err := c.reloadChain(member, cacert); err != nil {
		return err
	}

	// Wait for the certificate data to be updated. NS server has a few quirks (as per usual... sigh).
	// Reloading the chain will sometimes not work and need to be repeatedy prodded until it decides
	// to obey our command.
	return retryutil.Retry(c.ctx, 5*time.Second, couchbaseutil.ExtendedRetryCount, func() (bool, error) {
		if tlsValid(member, cacert, cert) {
			return true, nil
		}
		if err := c.reloadChain(member, cacert); err != nil {
			return false, nil
		}
		return false, nil
	})
}

// reconcileTLS performs any certificate rotations that are necessary.
func (c *Cluster) reconcileTLS() error {
	// Insecure cluster, ignore.
	if !c.cluster.Spec.TLS.IsSecureClient() {
		return nil
	}

	// Load the TLS data from kubernetes to extract the server certificate and CA.
	operatorSecret, err := k8sutil.GetSecret(c.config.KubeCli, c.cluster.Spec.TLS.Static.OperatorSecret, c.cluster.Namespace, nil)
	if err != nil {
		return err
	}
	cacert := operatorSecret.Data["ca.crt"]

	serverSecret, err := k8sutil.GetSecret(c.config.KubeCli, c.cluster.Spec.TLS.Static.Member.ServerSecret, c.cluster.Namespace, nil)
	if err != nil {
		return err
	}

	chainPem := decodePEM(serverSecret.Data["chain.pem"])
	cert, err := x509.ParseCertificate(chainPem[0].Bytes)
	if err != nil {
		return err
	}

	// VERIFY CONFIGURATION IS SANE HERE BEFORE BREAKING THE CLUSTER

	// Update the client to use the new CA certificate for verification.
	tls := c.client.GetTLS()
	if !reflect.DeepEqual(tls.CACert, cacert) {
		c.client.SetTLS(&cbmgr.TLSAuth{CACert: cacert})
	}

	changed := false
	for _, member := range c.members {
		// Try connect to the target node, if it doesn't respond we assume it's
		// deleted or the admin service has gone down and needs a reconcile to fix it.
		ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
		defer cancel()
		if err := netutil.WaitForHostPort(ctx, member.HostURL()); err != nil {
			continue
		}

		// If the server is correctly configured, ignore it.
		if tlsValid(member, cacert, cert) {
			continue
		}

		// Reload the CA certificate if necessary.
		if err := c.reloadCA(member, cacert); err != nil {
			return err
		}

		// Reload the server certificate chain.
		if err := c.reloadChainAndVerify(member, cacert, cert); err != nil {
			return err
		}

		// Indicate something happened for raising events.
		changed = true
	}

	// Finally if we did anything raise an event.
	if changed {
		c.raiseEvent(k8sutil.TLSUpdatedEvent(c.cluster))
	}

	return nil
}
