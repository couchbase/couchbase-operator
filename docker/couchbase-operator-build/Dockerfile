# This must be kept in lockstep with any non-static images due to library
# dependencies.
ARG GO_VERSION=1.20.8
FROM golang:${GO_VERSION} as build

# Create the working directory to build in.
# Note this is in a rwx directory because of Red Hat UBI requirements.
WORKDIR /tmp/src/github.com/couchbase/couchbase-operator

# Copy the go module files and download, this effectively caches the modules.
COPY go.* ./
RUN go mod download

# Defines the version string.
ARG VERSION

# Defines the build number.
ARG BLD_NUM

# Defines the target triplet to compile for.
ARG TARGET

# Defines the binary to build.
ARG BINARY

# Copy in the rest of the source and compile, caching any build objects.
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build make touch-generated binary -e VERSION=${VERSION} BLD_NUM=${BLD_NUM} TARGET=${TARGET} BINARY=${BINARY}
