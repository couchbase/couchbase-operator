# Copyright 2022-Present Couchbase, Inc.
#
# Use of this software is governed by the Business Source License included in
# the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
# file, in accordance with the Business Source License, use of this software
# will be governed by the Apache License, Version 2.0, included in the file
# licenses/APL2.txt.

FROM golang:1.25.5-trixie

WORKDIR /tmp/src/github.com/couchbase/couchbase-operator
ADD go.mod go.sum ./

RUN go mod download

ADD . .

RUN make pkg/apis/couchbase/v2/zz_generated.deepcopy.go
RUN make pkg/generated/clientset
RUN make pkg/generated/listers
RUN make pkg/generated/informers
RUN make example/crd.yaml

RUN make binary -e BINARY=cao
RUN make binary -e BINARY=cbopcfg
RUN make binary -e BINARY=cbopinfo
RUN make binary -e BINARY=couchbase-operator
RUN make binary -e BINARY=couchbase-admission-controller

