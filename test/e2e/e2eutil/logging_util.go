package e2eutil

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"strconv"
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
		client, cleanup, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return err
		}

		defer cleanup()

		info := &couchbaseutil.AuditSettings{}

		request := &couchbaseutil.Request{
			Path:   "/settings/audit",
			Result: info,
		}

		if err := client.client.Get(request, client.host); err != nil {
			return err
		}

		auditSpec := couchbase.Spec.Logging.Audit
		if auditSpec != nil && auditSpec.Enabled {
			if !info.Enabled {
				return fmt.Errorf("auditing should be enabled")
			}
			if info.LogPath != k8sutil.CouchbaseVolumeMountLogsDir {
				return fmt.Errorf("auditing log path not correct: %s != %s", info.LogPath, k8sutil.CouchbaseVolumeMountLogsDir)
			}
			if info.RotateInterval != int(auditSpec.Rotation.Interval.Seconds()) {
				return fmt.Errorf("auditing rotate interval not correct: %d != %d", info.RotateSize, int(auditSpec.Rotation.Interval.Seconds()))
			}
			if info.RotateSize != int(auditSpec.Rotation.Size.Value()) {
				return fmt.Errorf("auditing rotate size not correct: %d != %d", info.RotateSize, int(auditSpec.Rotation.Size.Value()))
			}

			// Just check lengths
			if len(info.DisabledEvents) != len(auditSpec.DisabledEvents) {
				return fmt.Errorf("auditing disabled events size not correct: %d != %d", len(info.DisabledEvents), len(auditSpec.DisabledEvents))
			}
			if len(info.DisabledUsers) != len(auditSpec.DisabledUsers) {
				return fmt.Errorf("auditing disabled users size not correct: %d != %d", len(info.DisabledUsers), len(auditSpec.DisabledUsers))
			}
		} else if info.Enabled {
			// Auditing was not enabled in the CRD but is reported as enabled
			return fmt.Errorf("auditing incorrectly enabled")
		}

		return nil
	})
}

func MustCheckAuditConfiguration(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) {
	err := checkAuditConfig(k8s, couchbase)
	if err != nil {
		Die(t, err)
	}
}

func enableAuditLogging(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) error {
	return retryutil.RetryFor(1*time.Minute, func() error {
		client, cleanup, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return err
		}

		defer cleanup()

		data := url.Values{}
		data.Add("auditdEnabled", "true")
		data.Add("rotateSize", "1024")
		data.Add("disabled", "")

		request := &couchbaseutil.Request{
			Path: "/settings/audit",
			Body: []byte(data.Encode()),
		}

		if err := client.client.Post(request, client.host); err != nil {
			return err
		}

		return nil
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

	for _, pod := range pods.Items {
		if len(pod.Spec.Containers) != expectedCount+1 {
			return fmt.Errorf("invalid container count (%d != %d) for pod %s", len(pod.Spec.Containers), expectedCount+1, pod.Name)
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

func checkLoggingSecret(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) error {
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
		return fmt.Errorf("invalid configuration as mismatch in config vs annotations: %s != %s", strconv.FormatBool(managed), strconv.FormatBool(exists))
	}

	// Not technically required of Secret, just set in test cases so confirming this is not an issue before using it
	if config.Data == nil {
		return fmt.Errorf("missing data key in secret")
	}

	if _, configured := config.Data["fluent-bit.conf"]; !configured {
		return fmt.Errorf("missing contents of data key in secret")
	}

	// Check we don't have the default one if we're configuring a custom one
	if fbs.ConfigurationName != "fluent-bit-config" {
		// Check the default CM is not created
		_, err := k8s.KubeClient.CoreV1().Secrets(couchbase.Namespace).Get(context.Background(), "fluent-bit-config", metav1.GetOptions{})
		if !errors.IsNotFound(err) {
			if err != nil {
				return err
			}

			return fmt.Errorf("default configuration is incorrectly present")
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

					return fmt.Errorf("missing audit GC logs for pod %q", podName)
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

					return fmt.Errorf("logs do not contain %q for pod %q", expected, podName)
				}

				// If nothing initially then wait and recheck
				if err := retryutil.RetryFor(timeout, f); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// MustCheckLoggingConfig verifies that the logging configuration is correct if enabled.
func MustCheckLoggingConfig(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) {
	err := checkLoggingSecret(k8s, couchbase)
	if err != nil {
		Die(t, err)
	}
}

func MustFailLoggingConfig(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) {
	err := checkLoggingSecret(k8s, couchbase)
	if err == nil {
		Die(t, err)
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
