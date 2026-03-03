# Copyright 2022-Present Couchbase, Inc.
#
# Use of this software is governed by the Business Source License included in
# the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
# file, in accordance with the Business Source License, use of this software
# will be governed by the Apache License, Version 2.0, included in the file
# licenses/APL2.txt.

FROM scratch

ARG TARGETARCH

COPY passwd /etc/passwd

COPY License.txt /License.txt
COPY README.txt /README.txt
COPY kubernetes-linux-${TARGETARCH}/couchbase-operator /usr/local/bin/couchbase-operator

USER 8453
