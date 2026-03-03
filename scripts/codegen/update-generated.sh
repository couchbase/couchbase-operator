#!/usr/bin/env bash

# Copyright 2017-Present Couchbase, Inc.
#
# Use of this software is governed by the Business Source License included in
# the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
# file, in accordance with the Business Source License, use of this software
# will be governed by the Apache License, Version 2.0, included in the file
# licenses/APL2.txt.

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
