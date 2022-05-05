package certification

import (
	"flag"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"

	"github.com/spf13/pflag"
)

// define flag names shared between cao and test suite.
const (
	ipv6Flag         = "ipv6"
	storageClassFlag = "storage-class"
)

// certifyOptions defines all options for a certification task.
type certifyOptions struct {
	// image is the certification image name.
	image string

	// image pull policy to use when downloading the certification container.
	imagePullPolicy string

	// parallel is the default parallelism (really concurrency) to use when running
	// test cases.
	parallel int

	// timeout is timeout for the certification process.
	timeout string

	// config is set at runtime and is the Kubernetes configuration.
	config *rest.Config

	// client is set at runtime and is a Kubernetes client.
	client kubernetes.Interface

	// metricsclient allows us to see CPU and memory utilization.
	metricsclient metricsclient.Interface

	// namespace is set at runtime and is the namespace this is being run in.
	namespace string

	// clean assumes the previous run was aborted and there are some resources
	// left lying about.
	clean bool

	// archiveName allows us to set a unique archive name for each run, in case
	// there are any races.
	archiveName archiveNameConfigValue

	// useFSGroup dictates whether to set the file system group.
	useFSGroup bool

	// fsGroup allows the file system group to be set.
	fsGroup int

	// RegistryConfigs define private container registries that need to be defined
	// as docker pull secrets in order to access private container images.
	registries RegistryConfigValue

	// enable periodic profiling via pprof.
	profile bool

	// profileDir defines where to dump profiling information.
	profileDir profileDirConfigValue

	// profilerStopChan is used to stop the profiler.
	profilerStopChan chan interface{}

	// profilerStoppedChan is used to wait for the profiler to stop.
	profilerStoppedChan chan interface{}

	// debug is used to dump out verbose information.
	debug bool

	// SharedTestFlags are flags shared between the cao cli and test suite.
	SharedTestFlags
}

// Shared test flags are flags which affect the behavior of both
// the certification Pod and Couchbase test Pods.
type SharedTestFlags struct {
	// IPv6 determines networking stack to use throughout certification framework.
	IPv6 bool

	// StorageClassName is the name of storage class to use for storing test artifacts
	// along with provisioning test persistent volumes.
	StorageClassName string
}

// Bind flags shared with test framework to typed variables.
// Optional arg allows binding shared values to a flagset.
func (s *SharedTestFlags) BindSharedFlags(flagSet *pflag.FlagSet) {
	flag.BoolVar(&s.IPv6, ipv6Flag,
		false,
		"Force the use of IPv6 with Couchbase Server.")
	flag.StringVar(&s.StorageClassName, storageClassFlag,
		"",
		"Storage class to use for result artifacts and test volumes. The default storage class of the platform is used if not specified.")

	// add flag to cli flagset if provided
	if flagSet != nil {
		flag.VisitAll(func(f *flag.Flag) {
			flagSet.AddGoFlag(f)
		})
	}
}
