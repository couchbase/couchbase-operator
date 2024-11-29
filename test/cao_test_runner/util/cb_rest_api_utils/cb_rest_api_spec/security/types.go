package security

import "time"

type Roles struct {
	Role string `json:"role"`
	Name string `json:"name"`
	Desc string `json:"desc"`
	Ce   bool   `json:"ce"`
}

type Users struct {
	ID                 string        `json:"id"`
	Domain             string        `json:"domain"`
	Roles              []UserRoles   `json:"roles"`
	Groups             []interface{} `json:"groups"`
	ExternalGroups     []interface{} `json:"external_groups"`
	Name               string        `json:"name,omitempty"`
	PasswordChangeDate time.Time     `json:"password_change_date"`
}

type UserRoles struct {
	Role       string `json:"role"`
	BucketName string `json:"bucket_name,omitempty"`
	ScopeName  string `json:"scope_name,omitempty"`
	Origins    []struct {
		Type string `json:"type"`
	} `json:"origins"`
}

type Groups struct {
	ID           string       `json:"id"`
	Roles        []GroupRoles `json:"roles"`
	LdapGroupRef string       `json:"ldap_group_ref"`
	Description  string       `json:"description"`
}

type GroupRoles struct {
	Role           string `json:"role,omitempty"`
	BucketName     string `json:"bucket_name,omitempty"`
	ScopeName      string `json:"scope_name,omitempty"`
	CollectionName string `json:"collection_name,omitempty"`
}
