package framework

import (
	"context"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Read Test run params from test_config yaml file.
func readRuntimeConfig(ymlFilePath string) (runTimeConfig TestRunParam, err error) {
	ymlFileContent, err := ioutil.ReadFile(ymlFilePath)
	if err != nil {
		err = fmt.Errorf("unable to read cluster config file `%s`: %w", ymlFilePath, err)
		return
	}

	if err = yaml.Unmarshal(ymlFileContent, &runTimeConfig); err != nil {
		err = fmt.Errorf("unable to decode test config: %w", err)
		return
	}

	return
}

// Function to read Suite and required cluster info from suite.yaml file.
func getSuiteDataFromYml(ymlFilePath string) (suiteData SuiteData, err error) {
	yamlFileContent, err := ioutil.ReadFile(ymlFilePath)
	if err != nil {
		err = fmt.Errorf("unable to read suite config file: %w", err)
		return
	}

	err = yaml.Unmarshal(yamlFileContent, &suiteData)
	if err != nil {
		err = fmt.Errorf("unable to decode suite config: %w", err)
		return
	}

	return
}

func RemoveServiceAccount(k8s *types.Cluster, serviceAccountName string) error {
	svcAccList, err := k8s.KubeClient.CoreV1().ServiceAccounts(k8s.Namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, svcAcc := range svcAccList.Items {
		if svcAcc.GetName() == serviceAccountName {
			if err := k8s.KubeClient.CoreV1().ServiceAccounts(k8s.Namespace).Delete(context.Background(), svcAcc.GetName(), metav1.DeleteOptions{}); err != nil {
				return err
			}

			if err := waitForServiceAccountDeleted(k8s, serviceAccountName, 30); err != nil {
				return err
			}
		}
	}

	return nil
}

// RecreateDockerAuthSecret deletes existing secrets and creates a new one if specified.
// This secret, if defined, will be added to the operator and admission controllers in
// order to pull from a private repository.
func recreateDockerAuthSecret(k8s *types.Cluster, namespace string) ([]string, error) {
	pullSecretLabel := "type"
	pullSecretValue := "qe-docker-pull-secret"

	// Clean up the old authentication secrets if they exist.
	if err := k8s.KubeClient.CoreV1().Secrets(namespace).DeleteCollection(context.Background(), *metav1.NewDeleteOptions(0), metav1.ListOptions{LabelSelector: fmt.Sprintf("%s=%s", pullSecretLabel, pullSecretValue)}); err != nil {
		return nil, err
	}

	pullSecrets := make([]string, len(runtimeParams.RegistryConfigs))

	// If specified create the authentication secrets
	for i, registry := range runtimeParams.RegistryConfigs {
		// auth string is simply "username:password" base64 encoded
		auth := registry.Username + ":" + registry.Password
		auth = base64.StdEncoding.EncodeToString([]byte(auth))

		// authentication data is encoded as per "~/.docker/config.json", and created by "docker login"
		data := `{"auths":{"` + registry.Server + `":{"auth":"` + auth + `"}}}`

		// create the new secret
		secret := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-docker-pull-secret-",
				Labels: map[string]string{
					pullSecretLabel: pullSecretValue,
				},
			},
			Type: v1.SecretTypeDockerConfigJson,
			Data: map[string][]byte{
				".dockerconfigjson": []byte(data),
			},
		}

		newSecret, err := k8s.KubeClient.CoreV1().Secrets(namespace).Create(context.Background(), secret, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}

		// Register that we have a pull secret, this will be used for all couchbase
		// clusters and deployments.
		pullSecrets[i] = newSecret.Name
	}

	return pullSecrets, nil
}

func RecreateServiceAccount(k8s *types.Cluster, serviceAccountName string) error {
	if err := RemoveServiceAccount(k8s, serviceAccountName); err != nil {
		return err
	}

	if serviceAccountName == "default" {
		return nil
	}

	if err := RemoveServiceAccount(k8s, config.BackupResourceName); err != nil {
		return err
	}

	// Create service account given by the name
	serviceAccount := &v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceAccountName,
		},
	}

	_, err := k8s.KubeClient.CoreV1().ServiceAccounts(k8s.Namespace).Create(context.Background(), serviceAccount, metav1.CreateOptions{})

	return err
}

func waitForServiceAccountDeleted(k8s *types.Cluster, serviceAccountName string, waitTimeInSec int) error {
	timeOutChan := time.NewTimer(time.Duration(waitTimeInSec) * time.Second).C
	tickChan := time.NewTicker(time.Second * time.Duration(1)).C

	for {
		select {
		case <-timeOutChan:
			return fmt.Errorf("timed out waiting for service account %s to be deleted", serviceAccountName)

		case <-tickChan:
			svcAccList, err := k8s.KubeClient.CoreV1().ServiceAccounts(k8s.Namespace).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return err
			}

			for _, svcAcc := range svcAccList.Items {
				if svcAcc.GetName() == "default" {
					break
				}

				if svcAcc.GetName() == serviceAccountName {
					break
				}
			}

			return nil
		}
	}
}
