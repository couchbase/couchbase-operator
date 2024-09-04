package jobs

import (
	v1 "k8s.io/api/core/v1"
)

func CreateBasicJob(jobName string) *Job {
	var job Job

	job.APIVersion = "batch/v1"
	job.Kind = "Job"
	job.Metadata.Name = jobName

	return &job
}

func CheckJobCompletion(jobName string) (bool, error) {
	// TODO
	panic("CheckJobCompletion to be implemented")
}

func (j *Job) AddContainer(cntName, cntImage string, cmdList, argList []string) {
	c := Container{
		Name:    cntName,
		Image:   cntImage,
		Command: cmdList,
		Args:    argList,
	}

	j.Spec.Template.Spec.Containers = append(j.Spec.Template.Spec.Containers, c)
}

func (j *Job) AddRestartPolicy(policy v1.RestartPolicy) {
	j.Spec.Template.Spec.RestartPolicy = policy
}

func (j *Job) AddNodeSelector(key, value string) {
	if j.Spec.Template.Spec.NodeSelector == nil {
		j.Spec.Template.Spec.NodeSelector = make(map[string]string)
	}

	j.Spec.Template.Spec.NodeSelector[key] = value
}

func (j *Job) AddTemplateName(specName string) {
	j.Spec.Template.Metadata.Name = specName
}
