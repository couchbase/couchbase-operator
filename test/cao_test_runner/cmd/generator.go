package cmd

import (
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/generator"
	"github.com/spf13/cobra"
)

func generateDocumentation() *cobra.Command {
	documentCmd := &cobra.Command{
		Use:   "generator",
		Short: "generate action and validation for cao-test-runner",
		Long:  "generate action and validation for cao-test-runner",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := generator.MainA(); err != nil {
				return err
			}

			return generator.MainV()
		},
	}

	return documentCmd
}
