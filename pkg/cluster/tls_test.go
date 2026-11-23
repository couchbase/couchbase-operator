/*
Copyright 2026-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cluster

import (
	"testing"

	v2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func TestGeneratedCAInUse(t *testing.T) {
	t.Parallel()

	mandatory := v2.ClientCertificatePolicyMandatory
	enable := v2.ClientCertificatePolicyEnable
	controlPlane := v2.NodeToNodeControlPlaneOnly
	all := v2.NodeToNodeAll
	strict := v2.NodeToNodeStrict

	cases := []struct {
		name       string
		image      string
		tls        *v2.TLSPolicy
		nodeClient string
		want       bool
	}{
		{name: "no TLS"},
		{name: "TLS only", tls: &v2.TLSPolicy{}},
		{name: "enable strict", tls: &v2.TLSPolicy{ClientCertificatePolicy: &enable, NodeToNodeEncryption: &strict}},
		{name: "mandatory no N2N", tls: &v2.TLSPolicy{ClientCertificatePolicy: &mandatory}},
		{name: "mandatory control plane", tls: &v2.TLSPolicy{ClientCertificatePolicy: &mandatory, NodeToNodeEncryption: &controlPlane}},
		{name: "mandatory all", tls: &v2.TLSPolicy{ClientCertificatePolicy: &mandatory, NodeToNodeEncryption: &all}, want: true},
		{name: "mandatory strict", tls: &v2.TLSPolicy{ClientCertificatePolicy: &mandatory, NodeToNodeEncryption: &strict}, want: true},
		{name: "mandatory strict node client cert", tls: &v2.TLSPolicy{ClientCertificatePolicy: &mandatory, NodeToNodeEncryption: &strict}, nodeClient: "node-client"},
		{name: "mandatory strict static TLS", tls: &v2.TLSPolicy{Static: &v2.StaticTLS{}, ClientCertificatePolicy: &mandatory, NodeToNodeEncryption: &strict}, want: true},
		{name: "mandatory strict before 7.6", image: "couchbase/server:7.2.0", tls: &v2.TLSPolicy{ClientCertificatePolicy: &mandatory, NodeToNodeEncryption: &strict}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			image := tc.image
			if image == "" {
				image = "couchbase/server:8.0.0"
			}

			c := &Cluster{
				cluster: &v2.CouchbaseCluster{Spec: v2.ClusterSpec{Image: image}},
				log:     logf.Log.WithName("test"),
			}

			if tc.tls != nil {
				tls := *tc.tls
				if tls.Static == nil {
					tls.SecretSource = &v2.TLSSecretSource{ServerSecretName: "server", NodeClientSecretName: tc.nodeClient}
				}

				c.cluster.Spec.Networking.TLS = &tls
			}

			if got := c.generatedCAInUse(); got != tc.want {
				t.Errorf("generatedCAInUse() = %v, want %v", got, tc.want)
			}
		})
	}
}
