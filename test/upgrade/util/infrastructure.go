/*
Copyright 2020-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package util

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// Namespace is the namespace to use for testing the operator/cluster.
	// This is hard coded because of the TLS certs, which is another hard problem
	// to solve, having to remember the formatting of older version and any upgrades
	// that need to happen during an upgrade, if we were to generate them on the fly.
	Namespace = "upgrade-test"
)

// Setup generates an ephemeral namespace to test in so we can just
// blow it away after the test.
func Setup(t *testing.T, c *Clients) {
	requested := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: Namespace,
		},
	}

	_, err := c.kubernetes.CoreV1().Namespaces().Create(context.Background(), requested, metav1.CreateOptions{})
	Assert(t, err)
}

// Teardown removes anything we may have installed as part of a test.
func Teardown(t *testing.T, c *Clients) {
	// Clean up the global DAC.
	logrus.Info("Cleaning up DAC ...")

	Assert(t, CouchbaseOperatorConfig("delete", "admission"))

	// Clean up the namespace(s).
	logrus.Info("Cleaning up namespaces ...")

	Assert(t, c.kubernetes.CoreV1().Namespaces().Delete(context.Background(), Namespace, metav1.DeleteOptions{}))
	MustWaitFor(t, ResourceDeleted(c, "", "v1", "namespace", "", Namespace), time.Minute)

	// Cleanup the CRDs
	logrus.Info("Cleaning up CRDs ...")

	gvr := schema.GroupVersionResource{
		Group:    "apiextensions.k8s.io",
		Version:  "v1",
		Resource: "customresourcedefinitions",
	}

	crds, err := c.dynamic.Resource(gvr).List(context.Background(), metav1.ListOptions{})
	Assert(t, err)

	for _, crd := range crds.Items {
		if !strings.HasSuffix(crd.GetName(), "couchbase.com") {
			continue
		}

		Assert(t, c.dynamic.Resource(gvr).Delete(context.Background(), crd.GetName(), metav1.DeleteOptions{}))
	}
}
