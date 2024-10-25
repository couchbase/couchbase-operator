package chaos

import (
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	"github.com/sirupsen/logrus"
)

// CBServiceName stores the process name of the couchbase service.
// Update MapCBServiceName() and ValidateCBServiceChaos() whenever adding new CBServiceName.
// Reference: docs.couchbase.com/server/current/install/server-processes.html.
type CBServiceName string

const (
	// Data Service.
	Memcached CBServiceName = "memcached"
	Projector CBServiceName = "projector"
	XDCR      CBServiceName = "goxdcr"
	BeamSMP   CBServiceName = "beam.smp"
	GoSecrets CBServiceName = "gosecrets"

	// Index Service.
	Index CBServiceName = "indexer"

	// Query Service.
	Query  CBServiceName = "cbq-engine"
	GoPort CBServiceName = "goport"

	// Analytics Service.
	Analytics CBServiceName = "cbas"

	// FTS Service.
	FTS CBServiceName = "cbft"

	// Eventing Service.
	EventingProducer CBServiceName = "eventing-producer"
	EventingConsumer CBServiceName = "eventing-consumer"
)

var (
	ErrCBServiceChaosInvalid   = errors.New("cb service chaos invalid")
	ErrCBServiceNameNotDefined = errors.New("cb service name not defined")
)

// ServiceChaosAction defines the name for the various chaos actions for services (processes).
// Update ExecuteCBServiceChaos() and validateChaosAction() whenever adding new ServiceChaosAction.
type ServiceChaosAction string

const (
	ServiceKill    ServiceChaosAction = "kill"
	ServiceKillAll ServiceChaosAction = "killAll"
	ServiceStop    ServiceChaosAction = "stop"
	ServiceRestart ServiceChaosAction = "restart"
)

// CBService stores the list of CB services on which we perform ServiceChaosAction.
type CBService struct {
	ServiceChaosAction ServiceChaosAction `yaml:"serviceChaosAction" caoCli:"required"`
	CBServices         []string           `yaml:"cbServices" caoCli:"required"`
}

type CBServiceChaosInterface interface {
	// KillService kills a service using `pkill`.
	KillService(ctx *context.Context, podName string) error

	// KillAllService stops all the processes/services except kernel threads using `/sbin/killall5`.
	KillAllService(ctx *context.Context, podName string) error

	// StopService stops a service using `/sbin/start-stop-daemon`.
	StopService(ctx *context.Context, podName string) error

	RestartService(ctx *context.Context, podName string) error
}

func ExecuteCBServiceChaos(context *context.Context, chaosConfig *CBPodChaosConfig, podName string) error {
	switch chaosConfig.CBServiceChaos.ServiceChaosAction {
	case ServiceKill:
		{
			return chaosConfig.CBServiceChaos.KillService(context, podName)
		}
	case ServiceKillAll:
		{
			return chaosConfig.CBServiceChaos.KillAllService(context, podName)
		}
	case ServiceStop:
		{
			return chaosConfig.CBServiceChaos.StopService(context, podName)
		}
	case ServiceRestart:
		{
			return chaosConfig.CBServiceChaos.RestartService(context, podName)
		}
	}

	return fmt.Errorf("execute cb service chaos: %w", ErrCBServiceChaosInvalid)
}

// MapCBServiceName maps various CB service names to CBServiceName. E.g. kv or memcached both will refer to Memcached.
func MapCBServiceName(service string) (CBServiceName, error) {
	switch strings.ToLower(service) {
	case "kv", "memcached":
		return Memcached, nil
	case "projector":
		return Projector, nil
	case "xdcr":
		return XDCR, nil
	case "beam", "beamsmp", "beam.smp":
		return BeamSMP, nil
	case "gosecrets":
		return GoSecrets, nil
	case "index", "indexer", "indexing":
		return Index, nil
	case "query", "cbq-engine", "cbq":
		return Query, nil
	case "goport":
		return GoPort, nil
	case "analytics":
		return Analytics, nil
	case "fts":
		return FTS, nil
	case "eventing-producer", "eventing":
		return EventingProducer, nil
	case "eventing-consumer":
		return EventingConsumer, nil
	default:
		return "", fmt.Errorf("map service name `%s`: %w", service, ErrCBServiceNameNotDefined)
	}
}

// KillService kills a service using `pkill`.
func (c CBService) KillService(ctx *context.Context, podName string) error {
	for _, service := range c.CBServices {
		logrus.Infof("Starting to kill service %s on pod %s", service, podName)

		cbSvc, err := MapCBServiceName(service)
		if err != nil {
			return fmt.Errorf("kill service `%s`: %w", service, err)
		}

		// E.g. kubectl exec cb-example-0000 -c couchbase-server -- pkill memcached
		_, err = kubectl.Exec(podName, "couchbase-server", "pkill", string(cbSvc)).
			InNamespace("default").Output()
		if err != nil {
			return fmt.Errorf("kill service `%s`: %w", service, err)
		}

		logrus.Infof("Successfully killed service %s on pod %s", service, podName)
	}

	return nil
}

// KillAllService stops all the processes/services except kernel threads using `/sbin/killall5`.
func (c CBService) KillAllService(ctx *context.Context, podName string) error {
	logrus.Infof("Starting to kill all services on pod %s", podName)

	// E.g. kubectl exec cb-example-0000 -c couchbase-server -- /sbin/killall5
	_, err := kubectl.Exec(podName, "couchbase-server", "/sbin/killall5").
		InNamespace("default").Output()
	if err != nil {
		return fmt.Errorf("kill all services: %w", err)
	}

	logrus.Infof("Successfully killed all services on pod %s", podName)

	return nil
}

// StopService stops a service using `/sbin/start-stop-daemon`.
func (c CBService) StopService(ctx *context.Context, podName string) error {
	for _, service := range c.CBServices {
		logrus.Infof("Starting to stop service %s on pod %s", service, podName)

		cbSvc, err := MapCBServiceName(service)
		if err != nil {
			return fmt.Errorf("kill service `%s`: %w", service, err)
		}

		// E.g. kubectl exec cb-example-0000 -c couchbase-server -- /sbin/start-stop-daemon --stop --name memcached
		_, err = kubectl.Exec(podName, "couchbase-server", "/sbin/start-stop-daemon",
			"--stop", "--name", string(cbSvc)).InNamespace("default").Output()
		if err != nil {
			return fmt.Errorf("stop service `%s`: %w", service, err)
		}

		logrus.Infof("Successfully stopped service %s on pod %s", service, podName)
	}

	return nil
}

func (c CBService) RestartService(ctx *context.Context, podName string) error {
	// TODO implement me
	panic("RestartService to be implemented")
}
