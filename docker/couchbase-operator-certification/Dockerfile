# Copyright 2022-Present Couchbase, Inc.
#
# Use of this software is governed by the Business Source License included in
# the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
# file, in accordance with the Business Source License, use of this software
# will be governed by the Apache License, Version 2.0, included in the file
# licenses/APL2.txt.

# ------------------------------------------------------------------------------
# Stage 1: Extraction
# Install bzip2 and isolate the binary + libraries for copying
# ------------------------------------------------------------------------------
FROM dhi.io/debian-base:trixie-dev AS extractor

# Update apt, install bzip2, and stage the files.
# The wildcard /usr/lib/*/ gracefully catches both x86_64 and aarch64 paths.
RUN apt-get update && apt-get install -y --no-install-recommends --allow-downgrades \
    bzip2 libbz2-1.0=1.0.8-6 && \
    mkdir -p /extra-bins && \
    cp /usr/bin/bzip2 /extra-bins/bzip2 && \
    cp /usr/lib/*/libbz2.so.1* /extra-bins/

# ------------------------------------------------------------------------------
# Stage 2: Hardened Runtime
# ------------------------------------------------------------------------------
FROM dhi.io/debian-base:trixie

ARG TARGETARCH

# Switch to root via numeric ID to create folders and set permissions
USER 0

# 1. Copy bzip2 and its libraries from the extractor stage.
# Dropping the libraries globally into /usr/lib/ ensures the dynamic 
# linker finds them instantly, regardless of the architecture.
COPY --from=extractor /extra-bins/bzip2 /usr/bin/bzip2
COPY --from=extractor /extra-bins/libbz2.so.1* /usr/lib/

# Ensure the binary is strictly executable
RUN chmod +x /usr/bin/bzip2

# 2. Setup application directories
RUN mkdir -p /artifacts && chown 65532:65532 /artifacts

# 3. Copy application binaries and resources
COPY --chown=65532:65532 kubernetes-linux-${TARGETARCH}/cao /
COPY --chown=65532:65532 kubernetes-linux-${TARGETARCH}/couchbase-operator-certification /
COPY --chown=65532:65532 crd.yaml /
COPY --chown=65532:65532 README.txt /
COPY --chown=65532:65532 License.txt /
COPY --chown=65532:65532 validation /resources/validation/

# 4. Drop privileges back to the secure non-root user
USER 65532

ENTRYPOINT ["/couchbase-operator-certification"]