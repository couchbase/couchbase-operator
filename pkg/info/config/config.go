package config

import (
	"github.com/couchbase/couchbase-operator/pkg/version"

	"github.com/spf13/pflag"

	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// Configuration holds command line parameters.
type Configuration struct {
	// ConfigFlags is a generic set of Kubernetes client flags.
	ConfigFlags *genericclioptions.ConfigFlags
	// Clusters is the set of clusters to collect information for.
	Clusters AppendStringVar
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
	clusterFlag             = "couchbase-cluster"
)

type AppendStringVar struct {
	Values []string
}

func (v *AppendStringVar) Type() string {
	return "string"
}

func (v *AppendStringVar) String() string {
	return ""
}

func (v *AppendStringVar) Set(s string) error {
	v.Values = append(v.Values, s)

	return nil
}

func (c *Configuration) AddFlags(flags *pflag.FlagSet) {
	c.ConfigFlags.AddFlags(flags)

	// Parse command line flags into our configuration
	flags.StringVar(&c.OperatorImage, operatorImageFlag, "couchbase/operator:"+version.Version, "Operator image name")
	flags.StringVar(&c.OperatorRestPort, operatorRestPortFlag, "8080", "Operator rest port")
	flags.StringVar(&c.OperatorMetricsPort, operatorMetricsPortFlag, "8383", "Operator metrics port")
	flags.StringVar(&c.ServerImage, serverImageFlag, "couchbase/server:6.6.2", "Couchbase server image")
	flags.StringVar(&c.CollectInfoCollect, collectInfoCollectFlag, "", "Collect couchbase server logs non-interactively, requires the -"+collectInfoFlag+" flag to be set")
	flags.StringVar(&c.Directory, directoryFlag, "", "Collect logs in a specific directory")
	flags.BoolVar(&c.All, allFlag, false, "Collect all resources from the namespace")
	flags.BoolVar(&c.System, systemFlag, false, "Collect kube-system resources and logs")
	flags.BoolVar(&c.CollectInfo, collectInfoFlag, false, "Collect couchbase server logs")
	flags.BoolVar(&c.CollectInfoRedact, collectInfoRedactFlag, false, "Redact couchbase server logs, requires the -"+collectInfoFlag+" flag to be set")
	flags.BoolVar(&c.CollectInfoList, collectInfoListFlag, false, "List all log sources in json and exit, requires the -"+collectInfoFlag+" flag to be set")
	flags.Var(&c.Clusters, clusterFlag, "Collect only resource for the named CouchbaseCluster, may be used multiple times")
}
