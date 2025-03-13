package couchbaseautoscalerresource

import (
	"time"
)

type Metadata struct {
	Annotations       map[string]string   `json:"annotations"`
	CreationTimestamp time.Time           `json:"creationTimestamp"`
	Labels            map[string]string   `json:"labels"`
	Name              string              `json:"name"`
	Namespace         string              `json:"namespace"`
	OwnerReferences   []MetadataOwnerRefs `json:"ownerReferences"`
	ResourceVersion   string              `json:"resourceVersion"`
	UID               string              `json:"uid"`
}

type MetadataOwnerRefs struct {
	APIVersion         string `json:"apiVersion"`
	BlockOwnerDeletion bool   `json:"blockOwnerDeletion"`
	Controller         bool   `json:"controller"`
	Kind               string `json:"kind"`
	Name               string `json:"name"`
	UID                string `json:"uid"`
}

type Spec struct {
	Servers string `json:"servers"`
	Size    int    `json:"size"`
}

type Status struct {
	LabelSelector string `json:"labelSelector"`
	Size          int    `json:"size"`
}

type CouchbaseAutoScalerResource struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
	Status     Status   `json:"status"`
}

type CouchbaseAutoScalerResourceList struct {
	APIVersion                   string                         `json:"apiVersion"`
	CouchbaseAutoScalerResources []*CouchbaseAutoScalerResource `json:"items"`
	Kind                         string                         `json:"kind"`
}
