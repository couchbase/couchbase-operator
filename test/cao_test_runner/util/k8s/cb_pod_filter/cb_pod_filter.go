package cbpodfilter

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	cbpods "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/cb_pods"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	jsonpatchutil "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/jsonpatch"
	"github.com/sirupsen/logrus"
)

type PodFilterType string

const (
	FilterAll                 PodFilterType = "all"
	FilterByServices          PodFilterType = "services"
	FilterByServerNames       PodFilterType = "serverNames"
	FilterBySvcAndServerNames PodFilterType = "servicesAndServerNames"
)

var (
	ErrFilterTypeInvalid     = errors.New("invalid filter type")
	ErrLessPods              = errors.New("number of cb pods in the namespace is less than given count")
	ErrServicesNotFound      = errors.New("could not find cb pods with all the given services")
	ErrSelectStrategyInvalid = errors.New("invalid select strategy")
	ErrServicesEmpty         = errors.New("services list is empty")
	ErrServerNamesEmpty      = errors.New("server names list is empty")
	ErrNoMatchingPods        = errors.New("zero matching cb pods found")
)

// CBPodFilter is used to set the parameters based on which the CB pods will be filtered.
type CBPodFilter struct {
	// FilterType determines which CB pods to filter.
	FilterType PodFilterType `yaml:"filterType" caoCli:"required"`

	// SelectionStrategy determines which pods to select after we have identified and filtered the pods based on CBPodFilter.FilterType.
	SelectionStrategy PodSelectStrategy `yaml:"selectionStrategy" caoCli:"required"`

	// Services filters the CB pods by which services are present. It finds the `spec.servers[i].name` which has all the
	// services (strict checking) provided by the user and then filters by server name. Services slice is sorted and then used.
	Services []string `yaml:"services"`

	// ServicesNotStrict if set to true, then the checking of CBPodFilter.Services will not be strict.
	// So if CBPodFilter.Services = ["data"] then it will match the lists: ["data", "index"], ["data", "query", "analytics"] etc.
	ServicesNotStrict bool `yaml:"servicesNotStrict"`

	// ServerNames filters the CB pods by the names of the server as provided in the cluster YAML `spec.servers.name`.
	ServerNames []string `yaml:"serverNames"`

	// Count determines the number of CB pods to be selected.
	Count int `yaml:"count" caoCli:"required"`
}

func NewCBPodFilter(filterType PodFilterType, strategy PodSelectStrategy, services, serverNames []string, strict bool, count int) *CBPodFilter {
	return &CBPodFilter{
		FilterType:        filterType,
		SelectionStrategy: strategy,
		Services:          services,
		ServicesNotStrict: strict,
		ServerNames:       serverNames,
		Count:             count,
	}
}

// FilterPods filters the pods based on the CBPodFilter provided. It finds all the pods matching CBPodFilter and then
// based on the PodSelectStrategy it finally returns the pod names.
func (cpf *CBPodFilter) FilterPods() ([]string, error) {
	var filteredPods []string

	var err error

	switch cpf.FilterType {
	case FilterAll:
		{
			filteredPods, err = cpf.filterAll()
			if err != nil {
				return nil, fmt.Errorf("filter pods: %w", err)
			}
		}
	case FilterByServices:
		{
			if cpf.Services == nil {
				return nil, fmt.Errorf("filter pods by services: %w", ErrServicesEmpty)
			}

			slices.Sort(cpf.Services)

			filteredPods, err = cpf.filterByServices()
			if err != nil {
				return nil, fmt.Errorf("filter pods: %w", err)
			}
		}
	case FilterByServerNames:
		{
			if cpf.ServerNames == nil {
				return nil, fmt.Errorf("filter by server names: %w", ErrServerNamesEmpty)
			}

			filteredPods, err = cpf.filterByServerNames()
			if err != nil {
				return nil, fmt.Errorf("filter pods: %w", err)
			}
		}
	case FilterBySvcAndServerNames:
		{
			if cpf.Services == nil {
				return nil, fmt.Errorf("filter by services and server names: %w", ErrServicesEmpty)
			}

			if cpf.ServerNames == nil {
				return nil, fmt.Errorf("filter by services and server names: %w", ErrServerNamesEmpty)
			}

			slices.Sort(cpf.Services)

			filteredPods, err = cpf.filterBySvcAndServerNames()
			if err != nil {
				return nil, fmt.Errorf("filter pods: %w", err)
			}
		}
	default:
		{
			return nil, fmt.Errorf("filter cb pods by filter type %s: %w", cpf.FilterType, ErrFilterTypeInvalid)
		}
	}

	logrus.Infof("filtered the following pods based on filter `%s`:", cpf.FilterType)
	logrus.Infof("%v", filteredPods)

	selectedPods, err := SelectPodsUsingStrategy(cpf.SelectionStrategy, cpf.Count, filteredPods, "default")
	if err != nil {
		return nil, fmt.Errorf("filter pods: %w", err)
	}

	logrus.Infof("selected the following pods based on strategy `%s`:", cpf.SelectionStrategy)
	logrus.Infof("%v", selectedPods)

	return selectedPods, nil
}

// filterAll returns all the cb pods in the namespace.
func (cpf *CBPodFilter) filterAll() ([]string, error) {
	cbPodNames, err := cbpods.GetCBPodNames("default")
	if err != nil {
		return nil, fmt.Errorf("filter all: %w", err)
	}

	if len(cbPodNames) == 0 {
		return nil, fmt.Errorf("filter all: %w", ErrNoMatchingPods)
	}

	return cbPodNames, nil
}

// filterByServices filters the couchbase pods based on the CB services.
/*
 * By default, CB pods which have all the services (strict matching) listed in CBPodFilter.Services will be selected.
 * If ServicesNotStrict is set to true, then the checking of CBPodFilter.Services will not be strict.
 */
func (cpf *CBPodFilter) filterByServices() ([]string, error) {
	// Get the couchbasecluster resource in json format
	cbClustersJSON, err := kubectl.Get("couchbasecluster").FormatOutput("json").InNamespace("default").Output()
	if err != nil {
		return nil, fmt.Errorf("filter by services: %w", err)
	}

	i := 0
	cbClustersJSONMap := make(map[string]interface{})
	cpf.ServerNames = make([]string, 0)

	// Unmarshal the result into a map[string]interface{} variable
	err = json.Unmarshal([]byte(cbClustersJSON), &cbClustersJSONMap)
	if err != nil {
		return nil, fmt.Errorf("filter by services: %w", err)
	}

	for {
		// Get the list of services in a server group
		services, err := jsonpatchutil.Get(&cbClustersJSONMap, fmt.Sprintf("/items/0/spec/servers/%d/services", i))
		if err != nil {
			if errors.Is(err, jsonpatchutil.ErrPathNotFoundInJSON) {
				break
			}

			return nil, fmt.Errorf("filter by services: %w", err)
		}

		serviceList, err := jsonpatchutil.UnmarshalStringSlice(services)
		if err != nil {
			return nil, fmt.Errorf("filter by services: %w", err)
		}

		serverName, err := jsonpatchutil.GetString(&cbClustersJSONMap, fmt.Sprintf("/items/0/spec/servers/%d/name", i))
		if err != nil {
			if errors.Is(err, jsonpatchutil.ErrPathNotFoundInJSON) {
				break
			}

			return nil, fmt.Errorf("filter by services: %w", err)
		}

		i++

		if !cpf.ServicesNotStrict {
			slices.Sort(serviceList)

			// If the slices are equal then we have found the required `spec.servers.name`
			// We will set the cpf.ServerNames and call cpf.filterByServerNames()
			if slices.Equal(serviceList, cpf.Services) {
				cpf.ServerNames = append(cpf.ServerNames, serverName)
			}
		} else {
			// ServicesNotStrict is true, hence we just check if all the cpf.Services are present in the
			// serviceList (it may have other services as well) we retrieved.
			checkSvc := true

			for _, svc := range cpf.Services {
				if !slices.Contains(serviceList, svc) {
					checkSvc = false
					break
				}
			}

			if checkSvc {
				cpf.ServerNames = append(cpf.ServerNames, serverName)
			}
		}
	}

	if len(cpf.ServerNames) > 0 {
		filteredPods, err := cpf.filterByServerNames()
		if err != nil {
			return nil, fmt.Errorf("filter by services: %w", err)
		}

		if len(filteredPods) == 0 {
			return nil, fmt.Errorf("filter by services %v (strict=%t): %w", cpf.Services, !cpf.ServicesNotStrict, ErrNoMatchingPods)
		}

		return filteredPods, nil
	}

	return nil, fmt.Errorf("filter by services %v (strict=%t): %w", cpf.Services, !cpf.ServicesNotStrict, ErrServicesNotFound)
}

// filterByServerNames filters the CB pods according to the specified server names.
func (cpf *CBPodFilter) filterByServerNames() ([]string, error) {
	var filteredPods []string

	cbPods, err := cbpods.GetCBPods("default")
	if err != nil {
		return nil, fmt.Errorf("filter by server names: %w", err)
	}

	if len(cbPods) < cpf.Count {
		return nil, fmt.Errorf("filter by server names: %w", ErrLessPods)
	}

	for _, cbPod := range cbPods {
		if cbPodServerName, ok := cbPod.Metadata.Labels["couchbase_node_conf"]; ok && slices.Contains(cpf.ServerNames, cbPodServerName) {
			filteredPods = append(filteredPods, cbPod.Metadata.Name)
		}
	}

	if len(filteredPods) == 0 {
		return nil, fmt.Errorf("filter by server names %v: %w", cpf.ServerNames, ErrNoMatchingPods)
	}

	return filteredPods, nil
}

// filterBySvcAndServerNames filters CB pods using both filterByServerNames() and filterByServerNames() and then returns
// union of the two results.
func (cpf *CBPodFilter) filterBySvcAndServerNames() ([]string, error) {
	filteredPodsByServerNames, err := cpf.filterByServerNames()
	if err != nil {
		return nil, fmt.Errorf("filter by services and server names: %w", err)
	}

	cpf.ServerNames = nil

	filteredPodsByServices, err := cpf.filterByServices()
	if err != nil {
		return nil, fmt.Errorf("filter by services and server names: %w", err)
	}

	filteredPodsMap := make(map[string]bool)
	filteredPods := make([]string, 0)

	for _, podName := range filteredPodsByServerNames {
		filteredPodsMap[podName] = true
	}

	for _, podName := range filteredPodsByServices {
		filteredPodsMap[podName] = true
	}

	for podName := range filteredPodsMap {
		filteredPods = append(filteredPods, podName)
	}

	if len(filteredPods) == 0 {
		return nil, fmt.Errorf("filter by services and server names: %w", ErrNoMatchingPods)
	}

	return filteredPods, nil
}
