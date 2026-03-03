/*
Copyright 2017-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2eutil

import (
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func CreateCluster(t *testing.T, k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster) (*couchbasev2.CouchbaseCluster, error) {
	// This is the only place where all cluster creations converge due to code sprawl.
	// So regardless of whether the CRD was hand crafted, or a cookie cutter we are
	// guaranteed to apply the correct pod policy mutations here before every creation.
	e2espec.ApplyImagePullSecret(cl, k8s.PullSecrets)

	cl.Namespace = k8s.Namespace

	res, err := CreateCouchbaseCluster(k8s.CRClient, cl)
	if err != nil {
		return res, err
	}

	t.Logf("creating couchbase cluster: %s", res.Name)

	return res, nil
}

func getClusterCRD(crClient versioned.Interface, cl *couchbasev2.CouchbaseCluster) (*couchbasev2.CouchbaseCluster, error) {
	return crClient.CouchbaseV2().CouchbaseClusters(cl.Namespace).Get(cl.Name, metav1.GetOptions{})
}

// NameLabelSelector returns a label selector of the form name=<name>.
func NameLabelSelector(label, name string) map[string]string {
	return map[string]string{label: name}
}
