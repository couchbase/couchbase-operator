package config

import (
	"flag"
	"fmt"
	"os"

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
	// CollectInfo determines whether to collect Couchbase server logs
	CollectInfo bool
	// Help determines whether to print help and exit
	Help bool
	// Version determines whether to print out the version string and exit
	Version bool
}

// Parse parses configuration from the command line and returns an initialized
// Configuration object
func Parse() Configuration {
	c := Configuration{}

	// Parse command line flags into our configuration
	flag.StringVar(&c.Namespace, "namespace", "default", "namespace to search for couchbase clusters")
	flag.StringVar(&c.KubeConfig, "kubeconfig", "~/.kube/config", "kubernetes cluster configuration file")
	flag.BoolVar(&c.All, "all", false, "collect all resources from the namespace")
	flag.BoolVar(&c.System, "system", false, "collect kube-system resources and logs")
	flag.BoolVar(&c.CollectInfo, "collectinfo", false, "collect couchbase server logs")
	flag.BoolVar(&c.Help, "help", false, "print this message and exit")
	flag.BoolVar(&c.Version, "version", false, "print the version string and exit")
	flag.Parse()

	// Echo out help if we need it
	if c.Help {
		fmt.Println(help)
		flag.PrintDefaults()
		os.Exit(0)
	}

	// Echo out version if requested
	if c.Version {
		fmt.Printf("cbopinfo version %s (%s)\n", version.Version, revision.Revision())
		os.Exit(0)
	}

	// Process non-flag arguments
	c.Clusters = flag.Args()

	return c
}
