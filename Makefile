PREFIX ?= $(shell pwd)

pkgs = $(shell go list ./... | grep -v /vendor/)

.PHONY: all build container

all: build

build:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o build/bin/couchbase-operator ./cmd/operator/main.go

container: build
	docker build -t couchbase/couchbase-operator:v1 .