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
	"fmt"
	"testing"

	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func CreateSecret(k8s *types.Cluster, secretSpec *v1.Secret) (*v1.Secret, error) {
	return k8s.KubeClient.CoreV1().Secrets(k8s.Namespace).Create(context.Background(), secretSpec, metav1.CreateOptions{})
}

func MustCreateSecret(t *testing.T, k8s *types.Cluster, secret *v1.Secret) *v1.Secret {
	secret, err := CreateSecret(k8s, secret)
	if err != nil {
		Die(t, err)
	}

	return secret
}

func MustRecreateSecret(t *testing.T, k8s *types.Cluster, secret *v1.Secret) *v1.Secret {
	secret.ObjectMeta = metav1.ObjectMeta{
		Name:        secret.Name,
		Labels:      secret.Labels,
		Annotations: secret.Annotations,
	}

	return MustCreateSecret(t, k8s, secret)
}

func DeleteSecret(k8s *types.Cluster, secretName string, options metav1.DeleteOptions) error {
	return k8s.KubeClient.CoreV1().Secrets(k8s.Namespace).Delete(context.Background(), secretName, options)
}

func MustDeleteSecret(t *testing.T, k8s *types.Cluster, secretName string) {
	if err := DeleteSecret(k8s, secretName, metav1.DeleteOptions{}); err != nil {
		Die(t, err)
	}
}

func GetSecret(kubeClient kubernetes.Interface, namespace string, secretName string) (*v1.Secret, error) {
	return kubeClient.CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
}

func MustGetSecret(t *testing.T, k8s *types.Cluster, secretName string) *v1.Secret {
	secret, err := GetSecret(k8s.KubeClient, k8s.Namespace, secretName)
	if err != nil {
		Die(t, err)
	}

	return secret
}

func UpdateSecret(kubeClient kubernetes.Interface, namespace string, secret *v1.Secret) error {
	_, err := kubeClient.CoreV1().Secrets(namespace).Update(context.Background(), secret, metav1.UpdateOptions{})
	return err
}

// Use username and password from secret store.
func GetClusterAuth(kubeClient kubernetes.Interface, namespace string, secretName string) (string, string, error) {
	secret, err := GetSecret(kubeClient, namespace, secretName)
	if err != nil {
		return "", "", err
	}

	username, ok := secret.Data[constants.AuthSecretUsernameKey]
	if !ok {
		return "", "", fmt.Errorf("admin secret missing username")
	}

	password, ok := secret.Data[constants.AuthSecretPasswordKey]
	if !ok {
		return "", "", fmt.Errorf("admin secret missing password")
	}

	return string(username), string(password), nil
}

// MustRotateClusterPassword updates the cluster admin secret with a new random
// password.  Note this will affect all subsequent tests, but that's not a bad thing
// because they all should refer to the secret and not hard code "password" anywhere!
func MustRotateClusterPassword(t *testing.T, k8s *types.Cluster) {
	secret := MustGetSecret(t, k8s, k8s.DefaultSecret.Name)

	password := []byte(RandomString(32))

	secret.Data["password"] = password

	if err := UpdateSecret(k8s.KubeClient, secret.Namespace, secret); err != nil {
		Die(t, err)
	}

	k8s.DefaultSecret.Data["password"] = password
}
