package nodes

import "time"

type Metadata struct {
	Annotations       map[string]string `json:"annotations"`       // kind, EKS
	CreationTimestamp time.Time         `json:"creationTimestamp"` // kind, EKS
	Labels            map[string]string `json:"labels"`            // kind, EKS
	Name              string            `json:"name"`              // kind, EKS
	ResourceVersion   string            `json:"resourceVersion"`   // kind, EKS
	UID               string            `json:"uid"`               // kind, EKS
}

type Spec struct {
	PodCIDR    string   `json:"podCIDR"`    // kind
	PodCIDRs   []string `json:"podCIDRs"`   // kind
	ProviderID string   `json:"providerID"` // kind, EKS
}

type Images struct {
	Names     []string `json:"names"`
	SizeBytes int      `json:"sizeBytes"`
}

type Status struct {
	Addresses       []map[string]string         `json:"addresses"`       // kind, EKS
	Allocatable     map[string]string           `json:"allocatable"`     // kind, EKS
	Capacity        map[string]string           `json:"capacity"`        // kind, EKS
	Conditions      []map[string]string         `json:"conditions"`      // kind, EKS
	DaemonEndpoints map[string]map[string]int64 `json:"daemonEndpoints"` // kind, EKS
	Images          []Images                    `json:"images"`          // kind, EKS
	NodeInfo        map[string]string           `json:"nodeInfo"`        // kind, EKS
	VolumesAttached []map[string]string         `json:"volumesAttached"` // EKS
	VolumesInUse    []string                    `json:"volumesInUse"`    // EKS
}

type Node struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
	Status     Status   `json:"status"`
}

type NodeList struct {
	APIVersion string `json:"apiVersion"`
	Nodes      []Node `json:"items"`
	Kind       string `json:"kind"`
}
