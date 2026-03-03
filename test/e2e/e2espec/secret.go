/*
Copyright 2017-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2espec

import (
	"github.com/couchbase/couchbase-operator/test/e2e/constants"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	BasicSecretData = map[string][]byte{
		constants.SecretUsernameKey: []byte(constants.CbClusterUsername),
		constants.SecretPasswordKey: []byte(constants.CbClusterPassword),
	}
)

func NewDefaultSecret(namespace string) *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      constants.KubeTestSecretName,
		},
		Data: BasicSecretData,
	}
}
