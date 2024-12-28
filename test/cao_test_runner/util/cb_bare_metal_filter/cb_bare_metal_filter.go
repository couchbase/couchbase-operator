package cbbaremetalfilter

import (
	"errors"
	"fmt"
	"slices"
	"time"

	cbrestapi "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cb_rest_api_utils/cb_rest_api"
	"github.com/sirupsen/logrus"
)

type CBNodeFilterType string

const (
	FilterAll            CBNodeFilterType = "all"
	FilterByServices     CBNodeFilterType = "services"
	FilterByServerGroups CBNodeFilterType = "serverGroups"
)

var (
	ErrFilterTypeInvalid     = errors.New("invalid filter type")
	ErrLessCBNodes           = errors.New("number of cb nodes in the namespace is less than given count")
	ErrServicesNotFound      = errors.New("could not find cb nodes with all the given services")
	ErrSelectStrategyInvalid = errors.New("invalid select strategy")
	ErrServicesEmpty         = errors.New("services list is empty")
	ErrServerNamesEmpty      = errors.New("server names list is empty")
	ErrNoMatchingCBNodes     = errors.New("zero matching cb nodes found")
	ErrServerGroupsNotFound  = errors.New("could not find cb nodes with the given server groups")
)

// CBBareMetalFilter is used to set the parameters based on which the bare metal CB nodes will be filtered.
type CBBareMetalFilter struct {
	// FilterType determines which CB nodes to filter.
	FilterType CBNodeFilterType `yaml:"filterType" caoCli:"required"`

	// SelectionStrategy determines which cb nodes to select after we have identified and filtered the cb nodes based on CBBareMetalFilter.FilterType.
	SelectionStrategy CBNodeSelectStrategy `yaml:"selectionStrategy" caoCli:"required"`

	// Services filters the CB nodes by which services are present. It finds the `spec.servers[i].name` which has all the
	// services (strict checking) provided by the user and then filters by server name. Services slice is sorted and then used.
	Services []string `yaml:"services"`

	// ServicesNotStrict if set to true, then the checking of CBBareMetalFilter.Services will not be strict.
	// So if CBBareMetalFilter.Services = ["data"] then it will match the lists: ["data", "index"], ["data", "query", "analytics"] etc.
	ServicesNotStrict bool `yaml:"servicesNotStrict"`

	// ServerGroups filters the CB nodes by the Server Groups as defined in the CB cluster.
	ServerGroups []string `yaml:"serverGroups"`

	// Count determines the number of CB nodes to be selected.
	Count int `yaml:"count" caoCli:"required"`

	// ClusterSecretName determines which cluster secret name to take.
	ClusterSecretName string `yaml:"clusterSecretName"`

	// CB Hostname which is also the DNS name for the VM.
	CBHostname string `yaml:"cbHostname"`
}

func NewCBBareMetalFilter(filterType CBNodeFilterType, strategy CBNodeSelectStrategy, services []string, strict bool, count int) *CBBareMetalFilter {
	return &CBBareMetalFilter{
		FilterType:        filterType,
		SelectionStrategy: strategy,
		Services:          services,
		ServicesNotStrict: strict,
		Count:             count,
	}
}

// FilterCBNodes filters the cb nodes based on the CBBareMetalFilter provided. It finds all the cb nodes matching CBBareMetalFilter and then
// based on the CBNodeSelectStrategy it finally returns the cb node names.
func (cpf *CBBareMetalFilter) FilterCBNodes() ([]string, error) {
	var filteredCBNodes []string

	var err error

	switch cpf.FilterType {
	case FilterAll:
		{
			filteredCBNodes, err = cpf.filterAll()
			if err != nil {
				return nil, fmt.Errorf("filter cb nodes: %w", err)
			}
		}
	case FilterByServices:
		{
			if cpf.Services == nil {
				return nil, fmt.Errorf("filter cb nodes by services cb: %w", ErrServicesEmpty)
			}

			slices.Sort(cpf.Services)

			filteredCBNodes, err = cpf.filterByServicesCB()
			if err != nil {
				return nil, fmt.Errorf("filter cb nodes: %w", err)
			}
		}
	case FilterByServerGroups:
		{
			if cpf.ServerGroups == nil {
				return nil, fmt.Errorf("filter by server groups: %w", ErrServerNamesEmpty)
			}

			filteredCBNodes, err = cpf.filterByServerGroups()
			if err != nil {
				return nil, fmt.Errorf("filter cb nodes: %w", err)
			}
		}
	default:
		{
			return nil, fmt.Errorf("filter cb nodes by filter type %s: %w", cpf.FilterType, ErrFilterTypeInvalid)
		}
	}

	logrus.Infof("filtered the following cb nodes based on filter `%s`:", cpf.FilterType)
	logrus.Infof("%v", filteredCBNodes)

	selectedCBNodes, err := SelectCBNodesUsingStrategy(cpf.SelectionStrategy, cpf.Count, filteredCBNodes)
	if err != nil {
		return nil, fmt.Errorf("filter cb nodes: %w", err)
	}

	logrus.Infof("selected the following cb nodes based on strategy `%s`:", cpf.SelectionStrategy)
	logrus.Infof("%v", selectedCBNodes)

	return selectedCBNodes, nil
}

// filterAll returns all the cb nodes in the namespace.
func (cpf *CBBareMetalFilter) filterAll() ([]string, error) {
	var cbNodeNames []string

	var err error

	hostname := "localhost" // TODO take from TestAsset
	if cpf.CBHostname != "" {
		hostname = cpf.CBHostname
	}

	secretName := "cb-example-auth" // TODO take from TestAsset
	if cpf.ClusterSecretName != "" {
		secretName = cpf.ClusterSecretName
	}

	clusterNodesAPI, err := cbrestapi.NewClusterNodesAPI(hostname, "", "", "", secretName, "default", 5*time.Second, false, true)
	if err != nil {
		return nil, err
	}

	poolsDefault, err := clusterNodesAPI.PoolsDefault(false)
	if err != nil {
		return nil, fmt.Errorf("filter all: %w", err)
	}

	for _, cbNodes := range poolsDefault.Nodes {
		cbNodeName := cbNodes.OtpNode[5:] // removing `ns_1@` from ns_1@<cb-node-name> to get the cb node name.
		cbNodeNames = append(cbNodeNames, cbNodeName)
	}

	if len(cbNodeNames) == 0 {
		return nil, fmt.Errorf("filter all: %w", ErrNoMatchingCBNodes)
	}

	return cbNodeNames, nil
}

// filterByServicesCB filters the cb nodes based on the CB services using /pools/default information.
/*
 * By default, CB nodes which have all the services (strict matching) listed in CBBareMetalFilter.Services will be selected.
 * If ServicesNotStrict is set to true, then the checking of CBBareMetalFilter.Services will not be strict.
 */
func (cpf *CBBareMetalFilter) filterByServicesCB() ([]string, error) {
	hostname := "localhost" // TODO take from TestAsset
	if cpf.CBHostname != "" {
		hostname = cpf.CBHostname
	}

	secretName := "cb-example-auth" // TODO take from TestAsset
	if cpf.ClusterSecretName != "" {
		secretName = cpf.ClusterSecretName
	}

	clusterNodesAPI, err := cbrestapi.NewClusterNodesAPI(hostname, "", "", "", secretName, "default", 5*time.Second, false, true)
	if err != nil {
		return nil, err
	}

	poolsDefault, err := clusterNodesAPI.PoolsDefault(false)
	if err != nil {
		return nil, fmt.Errorf("filter all: %w", err)
	}

	var requiredCBNodes []string

	for _, cbNodes := range poolsDefault.Nodes {
		if !cpf.ServicesNotStrict {
			slices.Sort(cbNodes.Services)

			if slices.Equal(cbNodes.Services, cpf.Services) {
				cbNodeName := cbNodes.OtpNode[5:] // removing `ns_1@` from ns_1@<cb-node-name> to get the cb node name.
				requiredCBNodes = append(requiredCBNodes, cbNodeName)
			}
		} else {
			for _, svc := range cpf.Services {
				if slices.Contains(cbNodes.Services, svc) {
					cbNodeName := cbNodes.OtpNode[5:] // removing `ns_1@` from ns_1@<cb-node-name> to get the cb node name.
					requiredCBNodes = append(requiredCBNodes, cbNodeName)

					break
				}
			}
		}
	}

	if len(requiredCBNodes) < 1 {
		return nil, fmt.Errorf("filter by services cb %v (strict=%t): %w", cpf.Services, !cpf.ServicesNotStrict, ErrServicesNotFound)
	}

	return requiredCBNodes, nil
}

// filterByServerGroups filters the CB nodes according to the specified server groups.
func (cpf *CBBareMetalFilter) filterByServerGroups() ([]string, error) {
	hostname := "localhost" // TODO take from TestAsset
	if cpf.CBHostname != "" {
		hostname = cpf.CBHostname
	}

	secretName := "cb-example-auth" // TODO take from TestAsset
	if cpf.ClusterSecretName != "" {
		secretName = cpf.ClusterSecretName
	}

	clusterNodesAPI, err := cbrestapi.NewClusterNodesAPI(hostname, "", "", "", secretName, "default", 5*time.Second, false, true)
	if err != nil {
		return nil, err
	}

	poolsDefault, err := clusterNodesAPI.PoolsDefault(false)
	if err != nil {
		return nil, fmt.Errorf("filter all: %w", err)
	}

	var requiredCBNodes []string

	for _, cbNodes := range poolsDefault.Nodes {
		if slices.Contains(cpf.ServerGroups, cbNodes.ServerGroup) {
			cbNodeName := cbNodes.OtpNode[5:] // removing `ns_1@` from ns_1@<cb-node-name> to get the cb node name.
			requiredCBNodes = append(requiredCBNodes, cbNodeName)
		}
	}

	if len(requiredCBNodes) < 1 {
		return nil, fmt.Errorf("filter by server groups %v: %w", cpf.ServerGroups, ErrServerGroupsNotFound)
	}

	return requiredCBNodes, nil
}
