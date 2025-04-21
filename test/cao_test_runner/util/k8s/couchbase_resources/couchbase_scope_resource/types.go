package couchbasescoperesource

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

type Resources struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type Spec struct {
	Collections struct {
		Managed                   bool        `json:"managed"`
		PreserveDefaultCollection bool        `json:"preserveDefaultCollection"`
		Resources                 []Resources `json:"resources"`
		Selector                  struct{}    `json:"selector"`
	} `json:"collections"`
	DefaultScope bool   `json:"defaultScope"`
	Name         string `json:"name"`
}

type Status struct {
}

type CouchbaseScopeResource struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
	Status     Status   `json:"status"`
}

type CouchbaseScopeResourceList struct {
	APIVersion              string                    `json:"apiVersion"`
	CouchbaseScopeResources []*CouchbaseScopeResource `json:"items"`
	Kind                    string                    `json:"kind"`
}
