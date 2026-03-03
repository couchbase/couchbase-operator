# Copyright 2022-Present Couchbase, Inc.
#
# Use of this software is governed by the Business Source License included in
# the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
# file, in accordance with the Business Source License, use of this software
# will be governed by the Apache License, Version 2.0, included in the file
# licenses/APL2.txt.

# This must be kept in lockstep with the builder images due to library
# dependencies.
ARG GO_VERSION=1.17.8

FROM golang:$GO_VERSION-alpine

# Install whatever compression and archival stuff we need to stream off
# the test archive (logs).
RUN --mount=type=cache,target=/var/cache/apk apk add tar bzip2 libc6-compat

ARG TARGET=kubernetes-linux-amd64

COPY ${TARGET}/cao /
COPY ${TARGET}/couchbase-operator-certification /
COPY crd.yaml /

COPY README.txt /
COPY License.txt /
COPY validation.yaml /resources/validation/

# Create a non-root user to run as.
RUN adduser -u 8453 -s /bin/false -D couchbase
RUN mkdir /artifacts
RUN chown couchbase /artifacts

USER 8453

ENTRYPOINT ["/couchbase-operator-certification"]
