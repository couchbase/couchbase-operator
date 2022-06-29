//go:build tools
// +build tools

package tool

import (
	_ "github.com/golangci/golangci-lint/cmd/golangci-lint"
	_ "k8s.io/code-generator/cmd/deepcopy-gen"
	_ "sigs.k8s.io/controller-tools/cmd/controller-gen"
)
