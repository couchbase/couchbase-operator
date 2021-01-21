package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/couchbase/couchbase-operator/pkg/version"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/spf13/cobra"
)

// LabelSelectorVar allows parsing of a label selector from the CLI.
type LabelSelectorVar struct {
	LabelSelector *metav1.LabelSelector
}

// String returns the default label selector: none.
func (v *LabelSelectorVar) String() string {
	return ""
}

// Set parses a label selector in the form k=v,k=v.
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

// Type returns the variable type.
func (v *LabelSelectorVar) Type() string {
	return "labelSelector"
}

const (
	// scopeNamespace tells the command to generate application YAML that operates
	// the the namespace scope (e.g Roles and RoleBindings).
	scopeNamespace = "namespace"

	// scopeCluster tells the command to generate application YAML that operates
	// the the cluster scope (e.g ClusterRoles and ClusterRoleBindings).
	scopeCluster = "cluster"
)

// validateScope checks that the scope enumeration is valid.
func validateScope(s string) error {
	if s != scopeNamespace && s != scopeCluster {
		return fmt.Errorf(`invalid scope "%s", must be one of ["%s", "%s"]`, s, scopeNamespace, scopeCluster)
	}

	return nil
}

// ParseArgs parses command line arguments into a Config struct and executes the command.
func Execute() {
	flags := genericclioptions.NewConfigFlags(true)

	// 'cbopcfg version' prints out the Operator version this binary belongs to.
	version := &cobra.Command{
		Use:   "version",
		Short: "Prints the command version",
		Long:  "Prints the command version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("cbopcfg", version.WithBuildNumber())
		},
	}

	// 'cbopcfg generate' creates YAML for various Operator deployments.
	generate := &cobra.Command{
		Use:   "generate",
		Short: "Generates YAML manifests",
		Long:  "Generates YAML manifests for various Operator components",
	}

	generate.AddCommand(getGenerateOperatorCommand(flags))
	generate.AddCommand(getGenerateAdmissionCommand(flags))
	generate.AddCommand(getGenerateBackupCommand(flags))

	// 'cbopcfg' is the top level command.
	root := &cobra.Command{
		Use: "cbopcfg",
	}

	flags.AddFlags(root.PersistentFlags())

	root.AddCommand(version)
	root.AddCommand(generate)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
