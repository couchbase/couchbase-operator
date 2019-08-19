package rbac

import (
	"fmt"
	"reflect"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/gocbmgr"

	"k8s.io/api/core/v1"
)

type ResourceAction string

const (
	ResourceActionCreated ResourceAction = "created"
	ResourceActionEdited  ResourceAction = "edited"
	ResourceActionDeleted ResourceAction = "deleted"
)

// ResourceInterface provides an interface to generically reconcile resources
type ResourceInterface interface {

	// Name of resource
	Name() string

	// Type of resource bound to roles
	BindType() couchbasev2.RoleBindingType

	// Add a role to the resource
	AddRole(role cbmgr.UserRole)

	// Equal with another of same kind
	Equal(other ResourceInterface) bool

	// Do action such as create, update, delete of resorce
	// Returns event associated with action taken or error
	Do(action ResourceAction, couchbase *couchbasev2.CouchbaseCluster, client *couchbaseutil.CouchbaseClient, members couchbaseutil.MemberSet) (*v1.Event, error)
}

// User resource
type ResourceUser struct {
	User *cbmgr.User
}

// Name of user resource
func (r ResourceUser) Name() string {
	return r.User.ID
}

// BindType of user resource
func (r ResourceUser) BindType() couchbasev2.RoleBindingType {
	return couchbasev2.RoleBindingTypeUser
}

// AddRole to list
func (r ResourceUser) AddRole(role cbmgr.UserRole) {
	if _, exists := hasRole(r.User.Roles, role.Role); !exists {
		r.User.Roles = append(r.User.Roles, role)
	}
}

// Equal with another user
func (r ResourceUser) Equal(other ResourceInterface) bool {
	if u, ok := other.(ResourceUser); ok {
		return reflect.DeepEqual(cbmgr.RolesToStr(r.User.Roles), cbmgr.RolesToStr(u.User.Roles))
	}
	return false
}

// Do create, update, or delete of user
func (r ResourceUser) Do(action ResourceAction, couchbase *couchbasev2.CouchbaseCluster, client *couchbaseutil.CouchbaseClient, members couchbaseutil.MemberSet) (*v1.Event, error) {

	if r.User == nil {
		return nil, fmt.Errorf("cannot perform action %s on missing user", action)
	}

	switch action {
	case ResourceActionCreated:
		if err := client.CreateUser(members, *r.User); err != nil {
			return nil, err
		}
		return k8sutil.UserCreateEvent(r.Name(), couchbase), nil
	case ResourceActionEdited:
		if err := client.CreateUser(members, *r.User); err != nil {
			return nil, err
		}
		return k8sutil.UserEditEvent(r.Name(), couchbase), nil
	case ResourceActionDeleted:
		if err := client.DeleteUser(members, *r.User); err != nil {
			return nil, err
		}
		return k8sutil.UserDeleteEvent(r.Name(), couchbase), nil
	}

	return nil, fmt.Errorf("cannot do unrecogized action %s on user %s", action, r.Name())
}

// Group resource
type ResourceGroup struct {
	Group *cbmgr.Group
}

// Name of group resource
func (r ResourceGroup) Name() string {
	return r.Group.ID
}

// BindType of group resource
func (r ResourceGroup) BindType() couchbasev2.RoleBindingType {
	return couchbasev2.RoleBindingTypeGroup
}

// AddRole to list
func (r ResourceGroup) AddRole(role cbmgr.UserRole) {
	if _, exists := hasRole(r.Group.Roles, role.Role); !exists {
		r.Group.Roles = append(r.Group.Roles, role)
	}
}

// Equal with another group
func (r ResourceGroup) Equal(other ResourceInterface) bool {
	if g, ok := other.(ResourceGroup); ok {
		if r.Group.LDAPGroupRef == g.Group.LDAPGroupRef {
			return reflect.DeepEqual(cbmgr.RolesToStr(r.Group.Roles), cbmgr.RolesToStr(g.Group.Roles))
		}
	}
	return false
}

// Do create, update, or delete of group
func (r ResourceGroup) Do(action ResourceAction, couchbase *couchbasev2.CouchbaseCluster, client *couchbaseutil.CouchbaseClient, members couchbaseutil.MemberSet) (*v1.Event, error) {

	if r.Group == nil {
		return nil, fmt.Errorf("cannot perform action %s on missing group", action)
	}

	switch action {
	case ResourceActionCreated:
		if err := client.CreateGroup(members, *r.Group); err != nil {
			return nil, err
		}
		return k8sutil.GroupCreateEvent(r.Name(), couchbase), nil
	case ResourceActionEdited:
		if err := client.CreateGroup(members, *r.Group); err != nil {
			return nil, err
		}
		return k8sutil.GroupEditEvent(r.Name(), couchbase), nil
	case ResourceActionDeleted:
		if err := client.DeleteGroup(members, *r.Group); err != nil {
			return nil, err
		}
		return k8sutil.GroupDeleteEvent(r.Name(), couchbase), nil
	}
	return nil, fmt.Errorf("cannot do unrecogized action %s on group %s", action, r.Name())
}

// hasRole checks if role already exists in list
func hasRole(roles []cbmgr.UserRole, name string) (int, bool) {
	for i, role := range roles {
		if role.Role == name {
			return i, true
		}
	}
	return -1, false
}

// ResourceOpts provide optional information during init
type ResourceOpts struct {
	Password string
}

// NewResource creates resource with a
// cbmgr object as concrete implementation
func NewResource(obj interface{}, opts *ResourceOpts) (ResourceInterface, error) {
	var resource ResourceInterface

	switch obj := obj.(type) {
	case *cbmgr.User:
		resource = ResourceUser{
			User: obj,
		}
	case *couchbasev2.CouchbaseUser:
		user := obj
		resource = ResourceUser{
			User: &cbmgr.User{
				ID:       user.Name,
				Name:     user.Spec.FullName,
				Domain:   cbmgr.AuthDomain(user.Spec.AuthDomain),
				Password: opts.Password,
			},
		}
	case *cbmgr.Group:
		resource = ResourceGroup{
			Group: obj,
		}
	case *couchbasev2.CouchbaseGroup:
		group := obj
		resource = ResourceGroup{
			Group: &cbmgr.Group{
				ID:           group.Name,
				LDAPGroupRef: group.Spec.LDAPGroupRef,
			},
		}
	default:
		return nil, fmt.Errorf("unknown RBAC resource kind: %T", obj)
	}

	return resource, nil
}

// ResourceList is a list of resources according to the type of role binding
type ResourceList map[couchbasev2.RoleBindingType][]ResourceInterface

// Add resource to list
func (r ResourceList) Add(element ResourceInterface) {
	kind := element.BindType()
	if _, ok := r[kind]; ok {
		r[kind] = append(r[kind], element)
	} else {
		r[kind] = []ResourceInterface{element}
	}
}

// List all resources
func (r ResourceList) List() []ResourceInterface {
	resources := []ResourceInterface{}
	for _, typeList := range r {
		resources = append(resources, typeList...)
	}
	return resources
}

// Get specific resource from list
func (r ResourceList) Get(bindType couchbasev2.RoleBindingType, name string) ResourceInterface {
	if resourceList, ok := r[bindType]; ok {
		for _, resource := range resourceList {
			if resource.Name() == name {
				return resource
			}
		}
	}
	return nil
}

// Extend list with another
func (r ResourceList) Extend(other ResourceList) {
	for kind, list := range other {
		if _, ok := r[kind]; ok {
			r[kind] = append(r[kind], list...)
		} else {
			r[kind] = list
		}
	}
}

// GetNamedResources returns resource names according to type of binding
func (r ResourceList) GetNamedResources(bindType couchbasev2.RoleBindingType) []string {
	names := []string{}
	if resources, ok := r[bindType]; ok {
		for _, resource := range resources {
			names = append(names, resource.Name())
		}
	}
	return names
}

// ListUserResources creates resource list from couchbase users
func ListUserResources(client *couchbaseutil.CouchbaseClient, members couchbaseutil.MemberSet) (ResourceList, error) {
	resourceList := make(ResourceList)
	users, err := client.ListUsers(members)
	if err != nil {
		return nil, err
	}
	for _, user := range users {
		if resource, err := NewResource(user, nil); err == nil {
			resourceList.Add(resource)
		} else {
			return nil, err
		}
	}
	return resourceList, nil
}

// ListGroupResources creates resource list from couchbase users
func ListGroupResources(client *couchbaseutil.CouchbaseClient, members couchbaseutil.MemberSet) (ResourceList, error) {
	resourceList := make(ResourceList)
	groups, err := client.ListGroups(members)
	if err != nil {
		return nil, err
	}
	for _, group := range groups {
		if resource, err := NewResource(group, nil); err == nil {
			resourceList.Add(resource)
		} else {
			return nil, err
		}
	}
	return resourceList, nil
}
