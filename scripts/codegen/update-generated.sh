#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Following two commands should be very quick after the first time
go get k8s.io/code-generator/...
go install k8s.io/code-generator/cmd/...
./scripts/codegen/codegen.sh \
  "all" \
  "github.com/couchbase/couchbase-operator/pkg/generated" \
  "github.com/couchbase/couchbase-operator/pkg/apis" \
  "couchbase:v1beta1" \
  --go-header-file "./scripts/codegen/boilerplate.go.txt" \
  $@
