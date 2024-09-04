package secrets

import "time"

type Secret struct {
	APIVersion string            `json:"apiVersion"`
	Data       map[string]string `json:"data"`
	Kind       string            `json:"kind"`
	Metadata   Metadata          `json:"metadata"`
	Type       string            `json:"type"`
}

type Metadata struct {
	Annotations       map[string]string `json:"annotations"`
	CreationTimestamp time.Time         `json:"creationTimestamp"`
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	ResourceVersion   string            `json:"resourceVersion"`
	UID               string            `json:"uid"`
}
