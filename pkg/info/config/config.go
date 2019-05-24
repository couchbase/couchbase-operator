package config

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/couchbase/couchbase-operator/pkg/revision"
	"github.com/couchbase/couchbase-operator/pkg/version"
)

const (
	// help is the header text to echo out before printing individual flags
	help = `usage: cbopinfo [flags] [cluster ...]
flags:`
)

// Configuration holds command line parameters
type Configuration struct {
	// Clusters is the set of clusters to collect information for
	Clusters []string
	// Namespace is the namespace to search for the specified clusters
	Namespace string
	// All specifies whether to collect all information in the namespace
	All bool
	// System specifies whether to attempt to collect system service logs
	System bool
	// Kubeconfig specifies a specific Kubernetes configuration file
	KubeConfig string
	// Context selects the kubectl context to use
	Context string
	// CollectInfo determines whether to collect Couchbase server logs
	CollectInfo bool
	// CollectInfoRedact determines whether to automatically redact logs
	CollectInfoRedact bool
	// CollectInfoList lists the logs that can be collected to the console and
	// immediatedly exists.
	CollectInfoList bool
	// CollectInfoCollect determines the logs to collect from the CLI.  Valid values
	// are an indices e.g. 1, ranges 2-3, a comma separated combination 1,3,5-6 or all.
	CollectInfoCollect string
	// Help determines whether to print help and exit
	Help bool
	// Version determines whether to print out the version string and exit
	Version bool
	// OperatorImage defines what the operator image is called
	OperatorImage string
	// OperatorRestPort defines what port the operator is listening on for HTTP requests
	OperatorRestPort string
	// OperatorMetricsPort defines what port the operator is listening on for prometheus metrics
	OperatorMetricsPort string
	// ServerImage defines the server image to use for log collection
	ServerImage string
}

const (
	namespaceFlag           = "namespace"
	kubeconfigFlag          = "kubeconfig"
	contextFlag             = "context"
	operatorImageFlag       = "operator-image"
	operatorRestPortFlag    = "operator-rest-port"
	operatorMetricsPortFlag = "operator-metrics-port"
	serverImageFlag         = "server-image"
	allFlag                 = "all"
	systemFlag              = "system"
	collectInfoFlag         = "collectinfo"
	collectInfoRedactFlag   = "collectinfo-redact"
	collectInfoListFlag     = "collectinfo-list"
	collectInfoCollectFlag  = "collectinfo-collect"
)

// flagToEnvVar converts a command line flag to a environment variable.
func flagToEnvVar(flag string) string {
	// Uppercase the flag
	key := strings.ToUpper(flag)

	// Translate hyphens to underscores
	re := regexp.MustCompile("-")
	key = re.ReplaceAllString(key, "_")

	return key
}

// lookupFlagFromEnvString looks up a string environment variable and returns a default
// value if not defined.
func lookupFlagFromEnvString(flag, def string) string {
	// Lookup the environment variable and return it if it exists.
	if value, ok := os.LookupEnv(flagToEnvVar(flag)); ok {
		return value
	}

	// Default to the default value
	return def
}

// lookupFlagFromEnvBool looks up a boolean environment variable and returns a default
// value if not defined.
func lookupFlagFromEnvBool(flag string, def bool) bool {
	// Lookup the environment variable and return it if it exists and is valid.
	if value, ok := os.LookupEnv(flagToEnvVar(flag)); ok {
		if v, err := strconv.ParseBool(value); err != nil {
			return v
		}
	}

	// Default to the default value
	return def
}

// Parse parses configuration from the command line and returns an initialized
// Configuration object
func Parse() Configuration {
	c := Configuration{}

	// Parse command line flags into our configuration
	flagSet := flag.NewFlagSet("cbopinfo", flag.ExitOnError)
	flagSet.StringVar(&c.Namespace, namespaceFlag, lookupFlagFromEnvString(namespaceFlag, "default"), "namespace to search for couchbase clusters")
	flagSet.StringVar(&c.KubeConfig, kubeconfigFlag, lookupFlagFromEnvString(kubeconfigFlag, "~/.kube/config"), "kubernetes cluster configuration file")
	flagSet.StringVar(&c.Context, contextFlag, lookupFlagFromEnvString(contextFlag, ""), "kubernetes cluster context")
	flagSet.StringVar(&c.OperatorImage, operatorImageFlag, lookupFlagFromEnvString(operatorImageFlag, "couchbase/operator:"+version.Version), "operator image name")
	flagSet.StringVar(&c.OperatorRestPort, operatorRestPortFlag, lookupFlagFromEnvString(operatorRestPortFlag, "8080"), "operator rest port")
	flagSet.StringVar(&c.OperatorMetricsPort, operatorMetricsPortFlag, lookupFlagFromEnvString(operatorMetricsPortFlag, "8383"), "operator metrics port")
	flagSet.StringVar(&c.ServerImage, serverImageFlag, lookupFlagFromEnvString(serverImageFlag, "couchbase/server:enterprise-6.0.1"), "couchbase server image")
	flagSet.StringVar(&c.CollectInfoCollect, collectInfoCollectFlag, lookupFlagFromEnvString(collectInfoCollectFlag, ""), "collect couchbase server logs non-interactively, requires the -"+collectInfoFlag+" flag to be set")
	flagSet.BoolVar(&c.All, allFlag, lookupFlagFromEnvBool(allFlag, false), "collect all resources from the namespace")
	flagSet.BoolVar(&c.System, systemFlag, lookupFlagFromEnvBool(systemFlag, false), "collect kube-system resources and logs")
	flagSet.BoolVar(&c.CollectInfo, collectInfoFlag, lookupFlagFromEnvBool(collectInfoFlag, false), "collect couchbase server logs")
	flagSet.BoolVar(&c.CollectInfoRedact, collectInfoRedactFlag, lookupFlagFromEnvBool(collectInfoRedactFlag, false), "redact couchbase server logs, requires the -"+collectInfoFlag+" flag to be set")
	flagSet.BoolVar(&c.CollectInfoList, collectInfoListFlag, lookupFlagFromEnvBool(collectInfoListFlag, false), "list all log sources in json and exit, requires the -"+collectInfoFlag+" flag to be set")
	flagSet.BoolVar(&c.Help, "help", false, "print this message and exit")
	flagSet.BoolVar(&c.Version, "version", false, "print the version string and exit")
	if err := flagSet.Parse(os.Args[1:]); err != nil {
		fmt.Println("failed to parse arguments:", err)
		os.Exit(1)
	}

	// Echo out help if we need it
	if c.Help {
		fmt.Println(help)
		flagSet.PrintDefaults()
		os.Exit(0)
	}

	// Echo out version if requested
	if c.Version {
		fmt.Printf("cbopinfo version %s (%s)\n", version.Version, revision.Revision())
		os.Exit(0)
	}

	// Process non-flag arguments
	c.Clusters = flagSet.Args()

	return c
}
