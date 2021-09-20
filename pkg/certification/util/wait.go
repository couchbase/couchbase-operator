package util

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// retryPeriod is how often to poll a WaitFunc.
	retryPeriod = 10 * time.Millisecond
)

var (
	ErrConditionMissing    = errors.New("pod condition missing")
	ErrConditionUnready    = errors.New("pod condition unready")
	ErrConditionRunning    = errors.New("pod condition runnning")
	ErrStatusTerminated    = errors.New("pod status is terminated")
	ErrStatusNotTerminated = errors.New("pod status not terminated")
	ErrVolumeExists        = errors.New("pvc still exists")
)

// WaitFunc is a callback that stops a wait when nil.
type WaitFunc func() error

// WaitFor waits until a condition is nil or the container is terminated.
func WaitFor(f WaitFunc, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	tick := time.NewTicker(retryPeriod)
	defer tick.Stop()

	for err := f(); err != nil; err = f() {
		if errors.Is(err, ErrStatusTerminated) {
			return ErrStatusTerminated
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return fmt.Errorf("failed to wait for condition: %w", err)
		}
	}

	return nil
}

func PodReady(client kubernetes.Interface, namespace, name string) error {
	pod, err := client.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			if condition.Status == corev1.ConditionTrue {
				return nil
			}

			if condition.Status == corev1.ConditionFalse && pod.Status.ContainerStatuses[0].State.Terminated != nil {
				return ErrStatusTerminated
			}

			return ErrConditionUnready
		}
	}

	return ErrConditionMissing
}

func PodCompleted(client kubernetes.Interface, namespace, name string, exitCode *int32) error {
	pod, err := client.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	// This can happen if the pod is evicted due to memory pressure, and annoyingly
	// there are no status conditions #legacy.
	if pod.Status.Phase == corev1.PodFailed {
		return nil
	}

	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			if condition.Status == corev1.ConditionFalse {
				if pod.Status.ContainerStatuses[0].State.Terminated == nil {
					return ErrStatusNotTerminated
				}

				if exitCode != nil {
					*exitCode = pod.Status.ContainerStatuses[0].State.Terminated.ExitCode
				}

				return nil
			}

			return ErrConditionRunning
		}
	}

	return ErrConditionMissing
}

func VolumeDeleted(client kubernetes.Interface, namespace, name string) error {
	_, err := client.CoreV1().PersistentVolumeClaims(namespace).Get(context.TODO(), name, metav1.GetOptions{})

	if err == nil {
		return ErrVolumeExists
	}

	return nil
}
