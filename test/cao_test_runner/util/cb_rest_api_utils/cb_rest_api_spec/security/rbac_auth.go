package security

import (
	"net/url"
	"strings"

	requestutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/request"
)

// ListRoles lists all roles.
/*
 * GET :: /settings/rbac/roles.
 * docs.couchbase.com/server/current/rest-api/rbac.html.
 * Unmarshal into slice of Roles struct.
 */
func ListRoles(hostname string) *requestutils.Request {
	return &requestutils.Request{
		Host:   hostname,
		Port:   "8091",
		Path:   "/settings/rbac/roles",
		Method: "GET",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}
}

// ListCurrentUsersAndRoles lists all current users and their roles.
/*
 * GET :: /settings/rbac/users.
 * docs.couchbase.com/server/current/rest-api/rbac.html.
 * Unmarshal into a slice of Users struct.
 */
func ListCurrentUsersAndRoles(hostname string) *requestutils.Request {
	return &requestutils.Request{
		Host:   hostname,
		Port:   "8091",
		Path:   "/settings/rbac/users",
		Method: "GET",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}
}

// CheckPermissions checks the permissions.
/*
 * POST :: /pools/default/checkPermissions.
 * docs.couchbase.com/server/current/rest-api/rbac.html.
 */
func CheckPermissions(hostname, permCheckSpec string) *requestutils.Request {
	formData := url.Values{}

	if permCheckSpec != "" {
		formData.Set(permCheckSpec, "")
	}

	return &requestutils.Request{
		Host:   hostname,
		Path:   "/pools/default/checkPermissions",
		Port:   "8091",
		Method: "POST",
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
		Body: formData.Encode(),
	}
}

// CreateLocalUser creates a new local user.
/*
 * PUT :: /settings/rbac/users/local/<new-username>.
 * docs.couchbase.com/server/current/rest-api/rbac.html.
 */
func CreateLocalUser(hostname, userUsername, userPassword string, roles, groups []string) *requestutils.Request {
	formData := url.Values{}

	if userPassword != "" {
		formData.Set("password", userPassword)
	}

	if roles != nil {
		formData.Set("roles", strings.Join(roles, ","))
	}

	if groups != nil {
		formData.Set("groups", strings.Join(groups, ","))
	}

	return &requestutils.Request{
		Host:   hostname,
		Port:   "8091",
		Path:   "/settings/rbac/users/local/" + url.PathEscape(userUsername),
		Method: "PUT",
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
		Body: formData.Encode(),
	}
}

// UpdateLocalUser updates an existing local user.
/*
 * PATCH :: /settings/rbac/users/local/<existing-username>.
 * docs.couchbase.com/server/current/rest-api/rbac.html.
 */
func UpdateLocalUser(hostname, userUsername, userPassword string) *requestutils.Request {
	formData := url.Values{}

	if userPassword != "" {
		formData.Set("password", userPassword)
	}

	return &requestutils.Request{
		Host:   hostname,
		Port:   "8091",
		Path:   "/settings/rbac/users/local/" + url.PathEscape(userUsername),
		Method: "PATCH",
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
		Body: formData.Encode(),
	}
}

// DeleteLocalUser deletes a local user.
/*
 * DELETE :: /settings/rbac/users/local/<local-username>.
 * docs.couchbase.com/server/current/rest-api/rbac.html.
 */
func DeleteLocalUser(hostname, userUsername string) *requestutils.Request {
	return &requestutils.Request{
		Host:   hostname,
		Port:   "8091",
		Path:   "/settings/rbac/users/local/" + url.PathEscape(userUsername),
		Method: "DELETE",
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
	}
}

// CreateExternalUser creates a new external user.
/*
 * PUT :: /settings/rbac/users/external/<new-username>.
 * docs.couchbase.com/server/current/rest-api/rbac.html.
 */
func CreateExternalUser(hostname, userUsername string, roles, groups []string) *requestutils.Request {
	formData := url.Values{}

	if userUsername != "" {
		formData.Set("name", userUsername)
	}

	if roles != nil {
		formData.Set("roles", strings.Join(roles, ","))
	}

	if groups != nil {
		formData.Set("groups", strings.Join(groups, ","))
	}

	return &requestutils.Request{
		Host:   hostname,
		Port:   "8091",
		Path:   "/settings/rbac/users/external/" + url.PathEscape(userUsername),
		Method: "PUT",
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
		Body: formData.Encode(),
	}
}

// DeleteExternalUser deletes an external user.
/*
 * DELETE :: /settings/rbac/users/external/<external-username>.
 * docs.couchbase.com/server/current/rest-api/rbac.html.
 */
func DeleteExternalUser(hostname, userUsername string) *requestutils.Request {
	return &requestutils.Request{
		Host:   hostname,
		Port:   "8091",
		Path:   "/settings/rbac/users/external/" + url.PathEscape(userUsername),
		Method: "DELETE",
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
	}
}

// ListGroups lists all the groups.
/*
 * GET :: /settings/rbac/groups.
 * docs.couchbase.com/server/current/rest-api/rbac.html.
 * Unmarshal into a slice of Groups struct.
 */
func ListGroups(hostname string) *requestutils.Request {
	return &requestutils.Request{
		Host:   hostname,
		Port:   "8091",
		Path:   "/settings/rbac/groups",
		Method: "GET",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}
}

// CreateGroup creates a new group.
/*
 * PUT :: /settings/rbac/groups/<new-group_name>.
 * docs.couchbase.com/server/current/rest-api/rbac.html.
 */
func CreateGroup(hostname, groupName, desc, ldapGroupRef string, roles []string) *requestutils.Request {
	formData := url.Values{}

	if desc != "" {
		formData.Set("description", desc)
	}

	if roles != nil {
		formData.Set("roles", strings.Join(roles, ","))
	}

	if ldapGroupRef != "" {
		formData.Set("ldap_group_ref", ldapGroupRef)
	}

	return &requestutils.Request{
		Host:   hostname,
		Port:   "8091",
		Path:   "/settings/rbac/groups/" + url.PathEscape(groupName),
		Method: "PUT",
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
		Body: formData.Encode(),
	}
}

// DeleteGroup deletes a group.
/*
 * DELETE :: /settings/rbac/groups/<group_name>.
 * docs.couchbase.com/server/current/rest-api/rbac.html.
 */
func DeleteGroup(hostname, groupName string) *requestutils.Request {
	return &requestutils.Request{
		Host:   hostname,
		Port:   "8091",
		Path:   "/settings/rbac/groups/" + url.PathEscape(groupName),
		Method: "DELETE",
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
	}
}
