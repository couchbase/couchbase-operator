package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

var (
	ErrServiceNameNotProvided = errors.New("service name is not provided")
	ErrNamespaceNotProvided   = errors.New("namespace is not provided")
	ErrServiceDoesNotExist    = errors.New("service does not exist")
	ErrNoServicesInNamespace  = errors.New("no services in namespace")
)

// GetServiceNames returns a slice of strings containing names of all services in the given namespace.
// Defined errors returned: ErrNoServicesInNamespace.
func GetServiceNames(namespace string) ([]string, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get service names: %w", ErrNamespaceNotProvided)
	}

	serviceNamesOutput, err := kubectl.Get("services").FormatOutput("name").InNamespace(namespace).Output()
	if err != nil {
		return nil, fmt.Errorf("get service names: %w", err)
	}

	if serviceNamesOutput == "" {
		return nil, fmt.Errorf("get service names: %w", ErrNoServicesInNamespace)
	}

	serviceNames := strings.Split(serviceNamesOutput, "\n")
	for i := range serviceNames {
		// kubectl returns service names as service/service-name. We remove the prefix "service/"
		serviceNames[i] = strings.TrimPrefix(serviceNames[i], "service/")
	}

	return serviceNames, nil
}

// GetService gets the service information of the service in the given namespace and returns *Service.
// Defined errors returned: ErrServiceDoesNotExist.
func GetService(serviceName string, namespace string) (*Service, error) {
	if serviceName == "" {
		return nil, fmt.Errorf("get service: %w", ErrServiceNameNotProvided)
	}

	if namespace == "" {
		return nil, fmt.Errorf("get service: %w", ErrNamespaceNotProvided)
	}

	serviceJSON, stderr, err := kubectl.GetByTypeAndName("service", serviceName).FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.TrimSpace(stderr) == fmt.Sprintf("Error from server (NotFound): services \"%s\" not found", serviceName) {
			return nil, fmt.Errorf("get service: %w", ErrServiceDoesNotExist)
		}

		return nil, fmt.Errorf("get service: %w", err)
	}

	var service Service

	err = json.Unmarshal([]byte(serviceJSON), &service)
	if err != nil {
		return nil, fmt.Errorf("get service: %w", err)
	}

	return &service, nil
}

// GetServices gets the service information and returns the *ServiceList containing the list of Services.
// If serviceNames = nil, then all the services in the namespace are taken into account.
// Defined errors returned: ErrServiceDoesNotExist.
func GetServices(serviceNames []string, namespace string) (*ServiceList, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get services: %w", ErrNamespaceNotProvided)
	}

	if serviceNames == nil {
		servicesNamesList, err := GetServiceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get services: %w", err)
		}

		serviceNames = servicesNamesList
	}

	var serviceList ServiceList

	// When we execute `get service <single-service>`, then we receive a single Service JSON instead of list of Service JSONs.
	if len(serviceNames) == 1 {
		service, err := GetService(serviceNames[0], namespace)
		if err != nil {
			return nil, fmt.Errorf("get services: %w", err)
		}

		serviceList.Services = append(serviceList.Services, service)

		return &serviceList, nil
	}

	servicesJSON, stderr, err := kubectl.GetByTypeAndName("services", serviceNames...).FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.Contains(stderr, "Error from server (NotFound)") {
			return nil, fmt.Errorf("get services: %w", ErrServiceDoesNotExist)
		}

		return nil, fmt.Errorf("get services: %w", err)
	}

	err = json.Unmarshal([]byte(servicesJSON), &serviceList)
	if err != nil {
		return nil, fmt.Errorf("get services: json unmarshal: %w", err)
	}

	return &serviceList, nil
}

// GetServicesMap gets the service information and returns the map[string]*Service which has the *Service for each pod names in given list.
// If serviceNames = nil, then all the services in the namespace are taken into account.
func GetServicesMap(serviceNames []string, namespace string) (map[string]*Service, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get services map: %w", ErrNamespaceNotProvided)
	}

	if len(serviceNames) == 0 {
		serviceNamesList, err := GetServiceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get services map: %w", err)
		}

		serviceNames = serviceNamesList
	}

	serviceMap := make(map[string]*Service)

	servicesList, err := GetServices(serviceNames, namespace)
	if err != nil {
		return nil, fmt.Errorf("get services map: %w", err)
	}

	for i := range servicesList.Services {
		serviceMap[serviceNames[i]] = servicesList.Services[i]
	}

	return serviceMap, nil
}
