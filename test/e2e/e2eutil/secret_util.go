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

	cberrors "github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func CreateSecret(kubeClient kubernetes.Interface, namespace string, secretSpec *v1.Secret) (*v1.Secret, error) {
	return kubeClient.CoreV1().Secrets(namespace).Create(secretSpec)
}

func MustCreateSecret(t *testing.T, k8s *types.Cluster, namespace string, secret *v1.Secret) *v1.Secret {
	secret, err := CreateSecret(k8s.KubeClient, namespace, secret)
	if err != nil {
		Die(t, err)
	}
	return secret
}

func MustRecreateSecret(t *testing.T, k8s *types.Cluster, namespace string, secret *v1.Secret) *v1.Secret {
	secret.ObjectMeta = metav1.ObjectMeta{
		Name:        secret.Name,
		Labels:      secret.Labels,
		Annotations: secret.Annotations,
	}
	return MustCreateSecret(t, k8s, namespace, secret)
}

func DeleteSecret(kubeClient kubernetes.Interface, namespace string, secretName string, options *metav1.DeleteOptions) error {
	return kubeClient.CoreV1().Secrets(namespace).Delete(secretName, options)
}

func MustDeleteSecret(t *testing.T, k8s *types.Cluster, namespace string, secretName string) {
	if err := DeleteSecret(k8s.KubeClient, namespace, secretName, nil); err != nil {
		Die(t, err)
	}
}

func GetSecret(kubeClient kubernetes.Interface, namespace string, secretName string) (*v1.Secret, error) {
	return kubeClient.CoreV1().Secrets(namespace).Get(secretName, metav1.GetOptions{})
}

func MustGetSecret(t *testing.T, k8s *types.Cluster, namespace string, secretName string) *v1.Secret {
	secret, err := GetSecret(k8s.KubeClient, namespace, secretName)
	if err != nil {
		Die(t, err)
	}
	return secret
}

func UpdateSecret(kubeClient kubernetes.Interface, namespace string, secret *v1.Secret) error {
	_, err := kubeClient.CoreV1().Secrets(namespace).Update(secret)
	return err
}

// Use username and password from secret store
func GetClusterAuth(kubeClient kubernetes.Interface, namespace string, secretName string) (string, string, error) {

	var username string
	var password string

	secret, err := GetSecret(kubeClient, namespace, secretName)
	if err != nil {
		return username, password, err
	}

	data := secret.Data
	if val, ok := data[constants.AuthSecretUsernameKey]; ok {
		username = string(val)
	} else {
		return username, password, cberrors.ErrSecretMissingUsername{Reason: secretName}
	}
	if val, ok := data[constants.AuthSecretPasswordKey]; ok {
		password = string(val)
	} else {
		return username, password, cberrors.ErrSecretMissingPassword{Reason: secretName}
	}

	return username, password, nil
}
