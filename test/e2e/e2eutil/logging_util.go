/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2eutil

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/cluster"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func checkAuditConfig(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) error {
	return retryutil.RetryFor(time.Minute, func() error {
		client, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return err
		}

		info := couchbaseutil.AuditSettings{}

		request := newRequest("/settings/audit", nil, &info)

		if err := client.client.Get(request, client.host); err != nil {
			return err
		}

		return checkAuditSpec(couchbase.Spec.Logging.Audit, info)
	})
}

func checkAuditSpec(auditSpec *couchbasev2.CouchbaseClusterAuditLoggingSpec, actualSettings couchbaseutil.AuditSettings) error {
	if auditSpec != nil && auditSpec.Enabled {
		if !actualSettings.Enabled {
			return NewErrAuditNotEnabled()
		}

		if actualSettings.LogPath != k8sutil.CouchbaseVolumeMountLogsDir {
			return NewErrInvalidPathCheck("auditing log path", actualSettings.LogPath, k8sutil.CouchbaseVolumeMountLogsDir)
		}

		if actualSettings.RotateInterval != int(auditSpec.Rotation.Interval.Seconds()) {
			return NewErrInvalidIntegerCheck("auditing rotate interval", actualSettings.RotateSize, int(auditSpec.Rotation.Interval.Seconds()))
		}

		if actualSettings.RotateSize != int(auditSpec.Rotation.Size.Value()) {
			return NewErrInvalidIntegerCheck("auditing rotate size", actualSettings.RotateSize, int(auditSpec.Rotation.Size.Value()))
		}

		// Just check lengths
		if len(actualSettings.DisabledEvents) != len(auditSpec.DisabledEvents) {
			return NewErrInvalidIntegerCheck("auditing disabled events length", len(actualSettings.DisabledEvents), len(auditSpec.DisabledEvents))
		}

		if len(actualSettings.DisabledUsers) != len(auditSpec.DisabledUsers) {
			return NewErrInvalidIntegerCheck("auditing disabled users length: %d != %d", len(actualSettings.DisabledUsers), len(auditSpec.DisabledUsers))
		}
	} else if actualSettings.Enabled {
		// Auditing was not enabled in the CRD but is reported as enabled
		return NewErrAuditInvalidClusterConfig()
	}

	return nil
}

func MustCheckAuditConfiguration(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) {
	err := checkAuditConfig(k8s, couchbase)
	if err != nil {
		Die(t, err)
	}
}

func enableAuditLogging(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) error {
	return retryutil.RetryFor(1*time.Minute, func() error {
		client, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return err
		}

		data := url.Values{}
		data.Add("auditdEnabled", "true")
		data.Add("rotateSize", "1024")
		data.Add("disabled", "")

		request := newRequest("/settings/audit", []byte(data.Encode()), nil)

		return client.client.Post(request, client.host)
	})
}

// MustEnableAuditLogging turns on audit logging.
func MustEnableAuditLogging(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) {
	err := enableAuditLogging(k8s, couchbase)
	if err != nil {
		Die(t, err)
	}
}

func checkLoggingSidecarCount(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, expectedCount int) error {
	// Find all pods, get number of containers and subtract one for server container then compare to expected value
	listOptions := metav1.ListOptions{
		LabelSelector: constants.CouchbaseServerClusterKey + "=" + couchbase.Name,
	}

	pods, err := k8s.KubeClient.CoreV1().Pods(couchbase.Namespace).List(context.Background(), listOptions)
	if err != nil {
		return err
	}

	containerCount := 0

	for _, pod := range pods.Items {
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name == k8sutil.CouchbaseLogSidecarContainerName || status.Name == k8sutil.CouchbaseAuditCleanupSidecarContainerName {
				containerCount++
			}
		}

		if containerCount != expectedCount {
			return NewErrContainerCountInvalid(containerCount, expectedCount, pod.Name)
		}
	}

	return nil
}

func MustCheckLoggingSidecarsCount(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, expectedCount int) {
	err := checkLoggingSidecarCount(k8s, couchbase, expectedCount)
	if err != nil {
		Die(t, err)
	}
}

func getLogsOfContainerInPod(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, podName, containerName string) (string, error) {
	logs := k8s.KubeClient.CoreV1().Pods(couchbase.Namespace).GetLogs(podName, &v1.PodLogOptions{Container: containerName})

	readCloser, err := logs.Stream(context.Background())
	if err != nil {
		return "", err
	}
	defer readCloser.Close()

	buffer := &bytes.Buffer{}

	if _, err := io.Copy(buffer, readCloser); err != nil {
		return "", err
	}

	return buffer.String(), nil
}

func checkLoggingSecret(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, isOwnedByCluster bool) error {
	fbs := couchbase.Spec.Logging.Server
	if fbs == nil || !fbs.Enabled {
		return nil
	}

	// Verify that the Secret is valid
	config, err := k8s.KubeClient.CoreV1().Secrets(couchbase.Namespace).Get(context.Background(), fbs.ConfigurationName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	// Check if not operator managed that we don't have the annotation
	managed := true
	if fbs.ManageConfiguration != nil {
		managed = *fbs.ManageConfiguration
	}

	_, exists := config.Annotations[cluster.LoggingConfigurationManagedAnnotation]

	if exists != managed {
		return NewErrLogInvalidConfig(managed, exists)
	}

	// Not technically required of Secret, just set in test cases so confirming this is not an issue before using it
	if config.Data == nil {
		return NewErrLogMissingConfigKey("data")
	}

	if _, configured := config.Data["fluent-bit.conf"]; !configured {
		return NewErrLogMissingConfigKeyContents("Data")
	}

	// Check we don't have the default one if we're configuring a custom one
	if fbs.ConfigurationName != "fluent-bit-config" {
		// Check the default CM is not created
		_, err := k8s.KubeClient.CoreV1().Secrets(couchbase.Namespace).Get(context.Background(), "fluent-bit-config", metav1.GetOptions{})
		if !errors.IsNotFound(err) {
			if err != nil {
				return err
			}

			return NewErrDefaultConfigPresent()
		}
	}

	owners := config.GetOwnerReferences()
	// only check if we have an owner (we usually don't for custom ones created by the tests directly)
	if len(owners) > 0 {
		uidEquals := (owners[0].UID == couchbase.UID)
		if isOwnedByCluster != uidEquals {
			if isOwnedByCluster {
				return NewErrInvalidLogConfigOwner(fmt.Sprintf("not owned by cluster %q when it should be (%q)", couchbase.Name, owners[0].Name))
			}

			return NewErrInvalidLogConfigOwner(fmt.Sprintf("owned by cluster %q incorrectly", couchbase.Name))
		}
	} else if isOwnedByCluster {
		return NewErrInvalidLogConfigOwner(fmt.Sprintf("no owner but should be cluster %q", couchbase.Name))
	}

	return nil
}

func checkPodLogs(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration, expected string, pod v1.Pod) error {
	// Dump the pod's logs
	podName := pod.Name

	for _, container := range pod.Spec.Containers {
		containerName := container.Name

		switch containerName {
		case k8sutil.CouchbaseAuditCleanupSidecarContainerName:
			// Confirm the output of the audit cleanup sidecar shows it running
			f := func() error {
				log, err := getLogsOfContainerInPod(k8s, couchbase, podName, containerName)
				if err == nil && strings.Contains(log, "Cleaning audit logs") {
					return nil
				}

				// Back off for a retry
				time.Sleep(10 * time.Second)

				return NewErrMissingAuditCleanupLogs(podName)
			}

			// If nothing initially then wait for next cleanup cycle and recheck
			if err := retryutil.RetryFor(timeout, f); err != nil {
				return err
			}

		case k8sutil.CouchbaseLogSidecarContainerName:
			// Check we don't get a Fluent Bit config error - if we get logs then we cannot do so
			// Check we do get some logs unrelated to Fluent Bit
			f := func() error {
				log, err := getLogsOfContainerInPod(k8s, couchbase, podName, containerName)
				if err == nil && strings.Contains(log, expected) {
					return nil
				}

				time.Sleep(10 * time.Second)

				return NewErrMissingLogs(expected, podName)
			}

			// If nothing initially then wait and recheck
			if err := retryutil.RetryFor(timeout, f); err != nil {
				return err
			}
		}
	}

	return nil
}

func checkLogging(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration, expected string) error {
	fbs := couchbase.Spec.Logging.Server
	if fbs == nil || !fbs.Enabled {
		return nil
	}

	listOptions := metav1.ListOptions{
		LabelSelector: constants.CouchbaseServerClusterKey + "=" + couchbase.Name,
	}

	pods, err := k8s.KubeClient.CoreV1().Pods(couchbase.Namespace).List(context.Background(), listOptions)
	if err != nil {
		return err
	}

	// Iterate over all pods and their containers
	for _, pod := range pods.Items {
		if err := checkPodLogs(k8s, couchbase, timeout, expected, pod); err != nil {
			return err
		}
	}

	return nil
}

// MustCheckLoggingConfig verifies that the logging configuration is correct if enabled.
func MustCheckLoggingConfig(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) {
	err := checkLoggingSecret(k8s, couchbase, true)
	if err != nil {
		Die(t, err)
	}
}

func MustCheckLoggingConfigNotOwned(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) {
	err := checkLoggingSecret(k8s, couchbase, false)
	if err != nil {
		Die(t, err)
	}
}

func MustFailLoggingConfig(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) {
	err := checkLoggingSecret(k8s, couchbase, false)
	if err == nil {
		Die(t, NewErrUnexpectedSuccess("checking log config"))
	}
}

// MustCheckLogging verifies that the logging sidecar container is receiving logs on all pods in the operator if enabled.
func MustCheckLogging(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration) {
	MustCheckLogsForString(t, k8s, couchbase, timeout, "[0] couchbase.log.")
}

// MustCheckLogsForString verifies that the logging sidecar container logs contain a particular string is receiving logs on all pods in the operator if enabled.
func MustCheckLogsForString(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration, expected string) {
	err := checkLogging(k8s, couchbase, timeout, expected)
	if err != nil {
		Die(t, err)
	}
}

func MustCheckLogsAreMissingString(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration, unexpected string) {
	err := checkLogging(k8s, couchbase, timeout, unexpected)
	if err == nil {
		Die(t, NewErrUnexpectedSuccess("logs should not contain: "+unexpected))
	}
}

// Need to add test data for parsers we provide to confirm they function with known input.
func SetLogStreamingConfig(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, config map[string]string, update bool) {
	if couchbase.Spec.Logging.Server.ManageConfiguration == nil || (*couchbase.Spec.Logging.Server.ManageConfiguration) {
		Die(t, NewErrLogInvalidClusterConfig())
	}

	// Indicate whether we should be updating an existing one or creating a new one
	if update {
		existing, err := k8s.KubeClient.CoreV1().Secrets(k8s.Namespace).Get(context.Background(), couchbase.Spec.Logging.Server.ConfigurationName, metav1.GetOptions{})
		if err != nil {
			Die(t, err)
		}

		existing.StringData = config

		if _, err := k8s.KubeClient.CoreV1().Secrets(k8s.Namespace).Update(context.Background(), existing, metav1.UpdateOptions{}); err != nil {
			Die(t, err)
		}
	} else {
		// Make sure we clear out any existing config maps just in case to ensure a clean run
		if err := k8s.KubeClient.CoreV1().Secrets(k8s.Namespace).Delete(context.Background(), couchbase.Spec.Logging.Server.ConfigurationName, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
			Die(t, err)
		}

		// Provide a config that generates known data to test and confirm logs are parsed correctly
		defaultConfig := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: couchbase.Spec.Logging.Server.ConfigurationName,
			},
			StringData: config,
		}

		if _, err := k8s.KubeClient.CoreV1().Secrets(k8s.Namespace).Create(context.Background(), defaultConfig, metav1.CreateOptions{}); err != nil {
			Die(t, err)
		}
	}
}

func SetInvalidLogStreamingConfig(t *testing.T, k8s *types.Cluster, configName string) {
	// Make sure we clear out any existing config maps just in case to ensure a clean run
	if err := DeleteSecret(k8s, configName, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
		Die(t, err)
	}
	// Set up a invalid custom default - right name, just nothing in it but it is operator managed
	defaultConfig := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: configName,
		},
	}
	MustCreateSecret(t, k8s, defaultConfig)
}
