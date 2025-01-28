package services

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

type Port struct {
	Name       string `json:"name"`
	Protocol   string `json:"protocol"`
	Port       int    `json:"port"`
	TargetPort int    `json:"targetPort"`
}

type Spec struct {
	ClusterIP             string             `json:"clusterIP"`
	ClusterIPs            []*string          `json:"clusterIPs"`
	InternalTrafficPolicy string             `json:"internalTrafficPolicy"`
	IpFamilies            []*string          `json:"ipFamilies"`
	IpFamilyPolicy        string             `json:"ipFamilyPolicy"`
	Ports                 []*Port            `json:"ports"`
	SessionAffinity       string             `json:"sessionAffinity"`
	Type                  string             `json:"type"`
	Selector              map[string]*string `json:"selector"`
}

type Status struct {
	LoadBalancer interface{} `json:"loadBalancer"`
}

type Service struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
	Status     Status   `json:"status"`
}

type ServiceList struct {
	APIVersion string     `json:"apiVersion"`
	Services   []*Service `json:"items"`
	Kind       string     `json:"kind"`
}
