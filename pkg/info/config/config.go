package config

import (
	"fmt"
	"os"

	"github.com/couchbase/couchbase-operator/pkg/revision"
	"github.com/couchbase/couchbase-operator/pkg/version"

	"github.com/spf13/pflag"

	"k8s.io/cli-runtime/pkg/genericclioptions"
)

const (
	// help is the header text to echo out before printing individual flags.
	help = `usage: cbopinfo [flags] [cluster ...]
flags:`
)

// Configuration holds command line parameters.
type Configuration struct {
	// ConfigFlags is a generic set of Kubernetes client flags.
	ConfigFlags *genericclioptions.ConfigFlags
	// Clusters is the set of clusters to collect information for.
	Clusters []string
	// All specifies whether to collect all information in the namespace.
	All bool
	// System specifies whether to attempt to collect system service logs.
	System bool
	// CollectInfo determines whether to collect Couchbase server logs.
	CollectInfo bool
	// CollectInfoRedact determines whether to automatically redact logs.
	CollectInfoRedact bool
	// CollectInfoList lists the logs that can be collected to the console and
	// immediatedly exists.
	CollectInfoList bool
	// CollectInfoCollect determines the logs to collect from the CLI.  Valid values
	// are an indices e.g. 1, ranges 2-3, a comma separated combination 1,3,5-6 or all.
	CollectInfoCollect string
	// Help determines whether to print help and exit.
	Help bool
	// Version determines whether to print out the version string and exit.
	Version bool
	// OperatorImage defines what the operator image is called.
	OperatorImage string
	// OperatorRestPort defines what port the operator is listening on for HTTP requests.
	OperatorRestPort string
	// OperatorMetricsPort defines what port the operator is listening on for prometheus metrics.
	OperatorMetricsPort string
	// ServerImage defines the server image to use for log collection.
	ServerImage string
	// Directory records where to store the files.
	Directory string
}

const (
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
	directoryFlag           = "directory"
)

// Parse parses configuration from the command line and returns an initialized
// Configuration object.
func Parse() Configuration {
	c := Configuration{
		ConfigFlags: genericclioptions.NewConfigFlags(true),
	}

	flagSet := pflag.NewFlagSet("cbopinfo", pflag.ExitOnError)
	c.ConfigFlags.AddFlags(flagSet)

	// Parse command line flags into our configuration
	flagSet.StringVar(&c.OperatorImage, operatorImageFlag, "couchbase/operator:"+version.Version, "Operator image name")
	flagSet.StringVar(&c.OperatorRestPort, operatorRestPortFlag, "8080", "Operator rest port")
	flagSet.StringVar(&c.OperatorMetricsPort, operatorMetricsPortFlag, "8383", "Operator metrics port")
	flagSet.StringVar(&c.ServerImage, serverImageFlag, "couchbase/server:enterprise-6.0.1", "Couchbase server image")
	flagSet.StringVar(&c.CollectInfoCollect, collectInfoCollectFlag, "", "Collect couchbase server logs non-interactively, requires the -"+collectInfoFlag+" flag to be set")
	flagSet.StringVar(&c.Directory, directoryFlag, "", "Collect logs in a specific directory")
	flagSet.BoolVar(&c.All, allFlag, false, "Collect all resources from the namespace")
	flagSet.BoolVar(&c.System, systemFlag, false, "Collect kube-system resources and logs")
	flagSet.BoolVar(&c.CollectInfo, collectInfoFlag, false, "Collect couchbase server logs")
	flagSet.BoolVar(&c.CollectInfoRedact, collectInfoRedactFlag, false, "Redact couchbase server logs, requires the -"+collectInfoFlag+" flag to be set")
	flagSet.BoolVar(&c.CollectInfoList, collectInfoListFlag, false, "List all log sources in json and exit, requires the -"+collectInfoFlag+" flag to be set")
	flagSet.BoolVar(&c.Help, "help", false, "Print this message and exit")
	flagSet.BoolVar(&c.Version, "version", false, "Print the version string and exit")

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
		fmt.Printf("cbopinfo version %s (%s)\n", version.WithBuildNumber(), revision.Revision())
		os.Exit(0)
	}

	// Process non-flag arguments
	c.Clusters = flagSet.Args()

	return c
}
