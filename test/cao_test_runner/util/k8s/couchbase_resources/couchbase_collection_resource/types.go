package couchbasecollectionresource

import "time"

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
	History bool   `json:"history"`
	MaxTTL  string `json:"maxTTL"`
	Name    string `json:"name"`
}

type Status struct {
}

type CouchbaseCollectionResource struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
	Status     Status   `json:"status"`
}

type CouchbaseCollectionResourceList struct {
	APIVersion                   string                         `json:"apiVersion"`
	CouchbaseCollectionResources []*CouchbaseCollectionResource `json:"items"`
	Kind                         string                         `json:"kind"`
}
