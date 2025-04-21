package couchbasegroupresource

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
	LDAPGroupRef string `json:"ldapGroupRef"`
	Roles        []struct {
		Bucket  string `json:"bucket"`
		Buckets struct {
			Resources []struct {
				Kind string `json:"kind"`
				Name string `json:"name"`
			} `json:"resources"`
			Selector struct {
				MatchExpressions []struct {
					Key      string   `json:"key"`
					Operator string   `json:"operator"`
					Values   []string `json:"values"`
				} `json:"matchExpressions"`
				MatchLabels map[string]string `json:"matchLabels"`
			} `json:"selector"`
		} `json:"buckets"`
		Collections struct {
			Resources []struct {
				Kind string `json:"kind"`
				Name string `json:"name"`
			} `json:"resources"`
			Selector struct {
				MatchExpressions []struct {
					Key      string   `json:"key"`
					Operator string   `json:"operator"`
					Values   []string `json:"values"`
				} `json:"matchExpressions"`
				MatchLabels map[string]string `json:"matchLabels"`
			} `json:"selector"`
		} `json:"collections"`
		Name   string `json:"name"`
		Scopes struct {
			Resources []struct {
				Kind string `json:"kind"`
				Name string `json:"name"`
			} `json:"resources"`
			Selector struct {
				MatchExpressions []struct {
					Key      string   `json:"key"`
					Operator string   `json:"operator"`
					Values   []string `json:"values"`
				} `json:"matchExpressions"`
				MatchLabels map[string]string `json:"matchLabels"`
			} `json:"selector"`
		} `json:"scopes"`
	} `json:"roles"`
}

type Status struct {
}

type CouchbaseGroupResource struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
	Status     Status   `json:"status"`
}

type CouchbaseGroupResourceList struct {
	APIVersion              string                    `json:"apiVersion"`
	CouchbaseGroupResources []*CouchbaseGroupResource `json:"items"`
	Kind                    string                    `json:"kind"`
}
