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
	"context"

	"github.com/couchbase/couchbase-operator/test/e2e/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func DeleteService(k8s *types.Cluster, serviceName string, options *metav1.DeleteOptions) error {
	return k8s.KubeClient.CoreV1().Services(k8s.Namespace).Delete(context.Background(), serviceName, *options)
}
