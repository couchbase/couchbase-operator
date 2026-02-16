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
	// Parallel determines how many pods to collect logs from at once
	Parallel int
	// EventCollectorPort defines the port that the event collector
	EventCollectorPort string
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
	// LogLevel tells us what to collect.
	LogLevel int
	// Upload specifies whether to attempt to upload logs
	Upload bool
	// UploadHost defines where to upload logs to
	UploadHost string
	// Customer specifies which customer to associate log uploads with
	Customer string
	// UploadProxy specifies a proxy for log upload
	UploadProxy string
	// Ticket specifies a Couchbase Support ticket number for log uploads
	Ticket string
	// BackupLogs determines whether to collect cbbackupmgr logs
	BackupLogs bool
	// BackupLogsName specifies which backup to collect logs from (empty means all)
	BackupLogsName string
	// BackupLogsKeepJob determines whether to keep the backup logs collection job after completion for debugging
	BackupLogsKeepJob bool
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
	parallelFlag            = "parallel"
	directoryFlag           = "directory"
	clusterFlag             = "couchbase-cluster"
	logLevelFlag            = "log-level"
	uploadFlag              = "upload-logs"
	uploadHostFlag          = "upload-host"
	customerFlag            = "customer"
	uploadProxyFlag         = "upload-proxy"
	ticketFlag              = "ticket"
	eventCollectorPortFlag  = "event-collector-port"
	backupLogsFlag          = "backup-logs"
	backupLogsNameFlag      = "backup-logs-name"
	backupLogsKeepJobFlag   = "backup-logs-keep-job"
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
	// Parse command line flags into our configuration
	flags.StringVar(&c.OperatorImage, operatorImageFlag, "couchbase/operator:"+version.Version, "Operator image name")
	flags.StringVar(&c.OperatorRestPort, operatorRestPortFlag, "8080", "Operator rest port")
	flags.StringVar(&c.OperatorMetricsPort, operatorMetricsPortFlag, "8383", "Operator metrics port")
	flags.StringVar(&c.ServerImage, serverImageFlag, "couchbase/server:7.1.3", "Couchbase server image")
	flags.StringVar(&c.CollectInfoCollect, collectInfoCollectFlag, "", "Collect couchbase server logs non-interactively, requires the -"+collectInfoFlag+" flag to be set")
	flags.IntVar(&c.Parallel, parallelFlag, 5, "How many pods to collect logs from at the same time")
	flags.StringVar(&c.Directory, directoryFlag, "", "Collect logs in a specific directory")
	flags.BoolVar(&c.All, allFlag, false, "Collect all resources from the namespace")
	flags.BoolVar(&c.System, systemFlag, false, "Collect kube-system resources and logs")
	flags.BoolVar(&c.CollectInfo, collectInfoFlag, false, "Collect couchbase server logs")
	flags.BoolVar(&c.CollectInfoRedact, collectInfoRedactFlag, false, "Redact couchbase server logs, requires the -"+collectInfoFlag+" flag to be set")
	flags.BoolVar(&c.CollectInfoList, collectInfoListFlag, false, "List all log sources in json and exit, requires the -"+collectInfoFlag+" flag to be set")
	flags.Var(&c.Clusters, clusterFlag, "Collect only resource for the named CouchbaseCluster, may be used multiple times")
	flags.IntVar(&c.LogLevel, logLevelFlag, 0, "Control the verbosity of collection, 0 will collect couchbase resources and those scoped to the cluster, 1 will collect more sensitive things that may be required for support such as secrets, roles etc.")
	flags.BoolVar(&c.Upload, uploadFlag, false, "Upload logs to support portal")
	flags.StringVar(&c.UploadHost, uploadHostFlag, "https://uploads.couchbase.com", "Specifies the fully-qualified domain name of the host you want the logs uploaded to. The protocol prefix of the domain name")
	flags.StringVar(&c.Customer, customerFlag, "default", "Specifies the customer name for log uploading. This value must be a string whose maximum length is 50 characters. Only the following characters can be used: [A-Za-z0-9_.-].")
	flags.StringVar(&c.UploadProxy, uploadProxyFlag, "", "Specifies a proxy for log uploading")
	flags.StringVar(&c.Ticket, ticketFlag, "", "Specifies the Couchbase Support ticket-number. The value must be a string with a maximum length of 7 characters, containing only digits in the range of 0-9.")
	flags.StringVar(&c.EventCollectorPort, eventCollectorPortFlag, "8080", "Event collector API port")
	flags.BoolVar(&c.BackupLogs, backupLogsFlag, false, "Collect cbbackupmgr logs from backup PVCs")
	flags.StringVar(&c.BackupLogsName, backupLogsNameFlag, "", "Specify which backup to collect logs from (default: all backups)")
	flags.BoolVar(&c.BackupLogsKeepJob, backupLogsKeepJobFlag, false, "Keep backup logs collection job after completion for debugging")
}
