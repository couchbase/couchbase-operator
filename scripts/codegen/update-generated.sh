#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

go install ./vendor/k8s.io/code-generator/cmd/...
./scripts/codegen/codegen.sh \
  "all" \
  "github.com/couchbase/couchbase-operator/pkg/generated" \
  "github.com/couchbase/couchbase-operator/pkg/apis" \
  "couchbase:v1,v2" \
  --go-header-file "./scripts/codegen/boilerplate.go.txt" \
  $@
