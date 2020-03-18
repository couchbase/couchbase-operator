package config

import (
	"flag"
	"os"
)

// Config contains command line configuration
type Config struct {
	// Namespace is the namespace to target, relevant for services and TLS.
	Namespace string
	// OperatorImage is the operator image name to use in deployments.
	OperatorImage string
	// OperatorImage is the admission controller image name to use in deployments.
	AdmissionImage string
	// ImagePullSecret indicates the image pull secret to use for deployments.
	ImagePullSecret string
	// NoOperator specifies not to dump operator configuration.
	NoOperator bool
	// NoAdmission specifies not to dump admission controller configuration.
	NoAdmission bool
	// Backup specifies whether to dump operator-backup configuration.
	Backup bool
	// File specified whether to create files rather than emit to stdout.
	File bool
}

// ParseArgs parses command line arguments into a Config struct.
func (c *Config) ParseArgs() error {
	flagSet := flag.NewFlagSet("cbopcfg", flag.ExitOnError)
	flagSet.StringVar(&c.Namespace, "namespace", "default", "Operator/dynamic admission controller namespace")
	flagSet.StringVar(&c.OperatorImage, "operator-image", operatorImageDefault, "Couchbase operator container image")
	flagSet.StringVar(&c.AdmissionImage, "dynamic-admission-controller-image", admissionImageDefault, "Couchbase dynamic admission controller image")
	flagSet.StringVar(&c.ImagePullSecret, "image-pull-secret", "", "Image pull secret (for private repos or RedHat container registry)")
	flagSet.BoolVar(&c.NoOperator, "no-operator", false, "Don't generate operator configuration")
	flagSet.BoolVar(&c.NoAdmission, "no-admission", false, "Dont generate dynamic admission controller configuration")
	flagSet.BoolVar(&c.NoAdmission, "backup", false, "Generate backup configuration")
	flagSet.BoolVar(&c.File, "file", false, "Create separate files rather than echo to standard out")
	return flagSet.Parse(os.Args[1:])
}
