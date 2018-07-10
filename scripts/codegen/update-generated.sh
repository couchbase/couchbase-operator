#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Following two commands should be very quick after the first time
go get k8s.io/code-generator/...

# Version control is important here!!  Update with the client libraries in glide.yaml
pushd ${GOPATH}/src/k8s.io/code-generator
git checkout kubernetes-1.9.2
popd

go install k8s.io/code-generator/cmd/...
./scripts/codegen/codegen.sh \
  "all" \
  "github.com/couchbase/couchbase-operator/pkg/generated" \
  "github.com/couchbase/couchbase-operator/pkg/apis" \
  "couchbase:v1" \
  --go-header-file "./scripts/codegen/boilerplate.go.txt" \
  $@
