package jobs

import (
	v1 "k8s.io/api/core/v1"
)

type Job struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   Metadata `yaml:"metadata"`
	Spec       JobSpec  `yaml:"spec"`
}

type Metadata struct {
	Name string `yaml:"name"`
}

type JobSpec struct {
	Template PodTemplateSpec `yaml:"template"`
}

type PodTemplateSpec struct {
	Metadata Metadata `yaml:"metadata"`
	Spec     PodSpec  `yaml:"spec"`
}

type PodSpec struct {
	Containers    []Container       `yaml:"containers"`
	NodeSelector  map[string]string `yaml:"nodeSelector"`
	RestartPolicy v1.RestartPolicy  `yaml:"restartPolicy"`
}

type Container struct {
	Name    string   `yaml:"name"`
	Image   string   `yaml:"image"`
	Command []string `yaml:"command"`
	Args    []string `yaml:"args"`
}
