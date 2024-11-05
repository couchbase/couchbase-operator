package jobs

import (
	"time"

	v1 "k8s.io/api/core/v1"
)

type Job struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
	Status     Status   `yaml:"status,omitempty" json:"status"`
	Spec       JobSpec  `yaml:"spec" json:"spec"`
}

type Metadata struct {
	Name string `yaml:"name" json:"name"`
}

type JobSpec struct {
	Template PodTemplateSpec `yaml:"template" json:"template"`
}

type PodTemplateSpec struct {
	Metadata Metadata `yaml:"metadata" json:"metadata"`
	Spec     PodSpec  `yaml:"spec" json:"spec"`
}

type PodSpec struct {
	Containers    []Container       `yaml:"containers" json:"containers"`
	NodeSelector  map[string]string `yaml:"nodeSelector,omitempty" json:"nodeSelector"`
	RestartPolicy v1.RestartPolicy  `yaml:"restartPolicy,omitempty" json:"restartPolicy"`
}

type Container struct {
	Name    string   `yaml:"name" json:"name"`
	Image   string   `yaml:"image" json:"image"`
	Command []string `yaml:"command" json:"command"`
	Args    []string `yaml:"args,omitempty" json:"args"`
}

type Status struct {
	StartTime      time.Time          `yaml:"startTime,omitempty" json:"startTime,omitempty"`
	CompletionTime time.Time          `yaml:"completionTime,omitempty" json:"completionTime,omitempty"`
	Conditions     []StatusConditions `yaml:"conditions,omitempty" json:"conditions,omitempty"`
	Ready          int                `yaml:"ready,omitempty" json:"ready,omitempty"`
	Active         int                `yaml:"active,omitempty" json:"active,omitempty"`
	Failed         int                `yaml:"failed,omitempty" json:"failed,omitempty"`
	Succeeded      int                `yaml:"succeeded,omitempty" json:"succeeded,omitempty"`
}

type StatusConditions struct {
	LastProbeTime      time.Time `yaml:"lastProbeTime,omitempty" json:"lastProbeTime"`
	LastTransitionTime time.Time `yaml:"lastTransitionTime,omitempty" json:"lastTransitionTime"`
	Status             string    `yaml:"status,omitempty" json:"status"`
	Type               string    `yaml:"type,omitempty" json:"type"`
}
