ARG GO_VERSION=1.17.2
FROM golang:${GO_VERSION} as build

ARG PROD_VERSION=2.3.0
ARG WORKDIR=/src/github.com/couchbase/couchbase-operator

# Create the working directory to build in.
WORKDIR ${WORKDIR}

# Copy the go module files and download, this effectively caches the modules.
COPY go.* ./
RUN go mod download

# Copy in the rest of the source and compile, caching any build objects.
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build make touch-generated binaries -e VERSION=${PROD_VERSION}

FROM scratch

ARG WORKDIR=/src/github.com/couchbase/couchbase-operator

COPY --from=build ${WORKDIR}/scripts/passwd /etc/passwd

COPY --from=build ${WORKDIR}/docs/License.txt /License.txt
COPY --from=build ${WORKDIR}/docs/README.txt /README.txt
COPY --from=build ${WORKDIR}/build/bin/couchbase-operator /usr/local/bin/couchbase-operator

USER 8453
