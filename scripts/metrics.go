package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
)

const (
	metricsFileBase = "metrics"

	containerName = "certification"
)

func getMetrics(path string) ([]metricsv1beta1.PodMetrics, error) {
	var metrics []metricsv1beta1.PodMetrics

	samples, err := ioutil.ReadDir(path)
	if err != nil {
		return nil, err
	}

	for _, sample := range samples {
		metricsFile := filepath.Join(path, sample.Name(), metricsFileBase)

		metricsJSON, err := os.ReadFile(metricsFile)
		if err != nil {
			return nil, err
		}

		var metric metricsv1beta1.PodMetrics

		if err := json.Unmarshal(metricsJSON, &metric); err != nil {
			return nil, err
		}

		metrics = append(metrics, metric)
	}

	return metrics, nil
}

func getContainerMetrics(metrics *metricsv1beta1.PodMetrics) (*metricsv1beta1.ContainerMetrics, error) {
	for i := range metrics.Containers {
		metric := metrics.Containers[i]

		if metrics.Name == containerName {
			return &metric, nil
		}
	}

	return nil, fmt.Errorf("unable to find container metrics")
}

func getCPUMetric(metrics *metricsv1beta1.ContainerMetrics) (float64, error) {
	metric, ok := metrics.Usage[corev1.ResourceCPU]
	if !ok {
		return 0.0, fmt.Errorf("unable to locate cpu metric")
	}

	return metric.AsApproximateFloat64(), nil
}

func getMemoryMetric(metrics *metricsv1beta1.ContainerMetrics) (float64, error) {
	metric, ok := metrics.Usage[corev1.ResourceMemory]
	if !ok {
		return 0.0, fmt.Errorf("unable to locate memory metric")
	}

	return metric.AsApproximateFloat64() / (1 << 30), nil
}

func main() {
	var path string

	flag.StringVar(&path, "path", "", "Path to profile directory")

	flag.Parse()

	metrics, err := getMetrics(path)
	if err != nil {
		fmt.Println(err)
	}

	fmt.Println("Time,CPU,Memory (Gi)")

	for i := range metrics {
		podMetric := metrics[i]

		containerMetric, err := getContainerMetrics(&podMetric)
		if err != nil {
			fmt.Println(err)
		}

		cpuMetric, err := getCPUMetric(containerMetric)
		if err != nil {
			fmt.Println(err)
		}

		memoryMetric, err := getMemoryMetric(containerMetric)
		if err != nil {
			fmt.Println(err)
		}

		fmt.Println(fmt.Sprintf("%s,%f,%f", podMetric.Timestamp, cpuMetric, memoryMetric))
	}
}
