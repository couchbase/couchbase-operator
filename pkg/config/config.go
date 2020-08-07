package config

import (
	"flag"
	"fmt"
	"os"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LabelSelectorVar allows parsing of a label selector from the CLI.
type LabelSelectorVar struct {
	LabelSelector *metav1.LabelSelector
}

// String returns the default label selector: none.
func (v *LabelSelectorVar) String() string {
	return ""
}

// Set parses a albel selector in the form k=v,k=v.
func (v *LabelSelectorVar) Set(value string) error {
	if value == "" {
		return nil
	}

	pairs := strings.Split(value, ",")

	for _, pair := range pairs {
		kv := strings.Split(pair, "=")

		if len(kv) != 2 {
			return fmt.Errorf(`label selector "%s" not formatted as string=string`, pair)
		}

		if v.LabelSelector == nil {
			v.LabelSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{},
			}
		}

		v.LabelSelector.MatchLabels[kv[0]] = kv[1]
	}

	return nil
}

// Component describes the thing we are installing.
type Component string

const (
	ComponentOperator  Component = "operator"
	ComponentAdmission Component = "admission"
	ComponentBackup    Component = "backup"
)

// Config contains command line configuration.
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
	// File specifies whether to create files rather than emit to stdout.
	File bool
	// Version specifies whether to print out the version string.
	Version bool
	// Cluster defines whether to install at the cluster or namepsace scope.
	Cluster bool
	// NamespaceSelector must be set for namespace scoped DAC installs.
	NamespaceSelector LabelSelectorVar
	// Component tells the tool what thing to install.
	Component Component
}

// ParseArgs parses command line arguments into a Config struct.
func (c *Config) ParseArgs() error {
	// Reuqire the component to be explicitly stated now as a positional parameter.
	// First is the binary, second should be the component, so expect at least 2.
	if len(os.Args) < 2 {
		return fmt.Errorf("expected one component of [operator,admssion,backup]")
	}

	c.Component = Component(os.Args[1])
	if c.Component != ComponentOperator && c.Component != ComponentAdmission && c.Component != ComponentBackup {
		return fmt.Errorf(`expected component "%v" to be one of [operator,admssion,backup]`, c.Component)
	}

	flagSet := flag.NewFlagSet("cbopcfg", flag.ExitOnError)
	flagSet.StringVar(&c.Namespace, "namespace", "default", "Operator/dynamic admission controller namespace")
	flagSet.StringVar(&c.OperatorImage, "operator-image", operatorImageDefault, "Couchbase operator container image")
	flagSet.StringVar(&c.AdmissionImage, "dynamic-admission-controller-image", admissionImageDefault, "Couchbase dynamic admission controller image")
	flagSet.StringVar(&c.ImagePullSecret, "image-pull-secret", "", "Image pull secret (for private repos or RedHat container registry)")
	flagSet.BoolVar(&c.File, "file", false, "Create separate files for each resource")
	flagSet.BoolVar(&c.Version, "version", false, "Displays the version and exits")
	flagSet.BoolVar(&c.Cluster, "cluster", false, "Deploys the components at the cluster scope")
	flagSet.Var(&c.NamespaceSelector, "namespace-selector", "Namespace selector for namespace scoped dynamic admission controller installs, in the form label=value[,...]")

	if err := flagSet.Parse(os.Args[2:]); err != nil {
		return err
	}

	// If we want to install the DAC, and scoped to a namespace, ensure that a namespace
	// selector has been specified.
	if c.Component == ComponentAdmission && !c.Cluster && c.NamespaceSelector.LabelSelector == nil {
		return fmt.Errorf("namespaced dynamic admission controller needs a namespace selector (-namespace-selector) argument")
	}

	return nil
}
