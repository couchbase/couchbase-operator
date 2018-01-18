PREFIX ?= $(shell pwd)
GOPATH = $(shell echo $${PWD%/src/*})
SOURCE = $(shell find . -name *.go -type f)
BINARY = build/bin/couchbase-operator

kubeconfig = $(if $(KUBECONFIG),$(KUBECONFIG),$(HOME)/.kube/config)
operatorImage = $(if $(OPERATOR_IMAGE),$(OPERATOR_IMAGE),couchbase/couchbase-operator:v1)
namespace = $(if $(KUBENAMESPACE),$(KUBENAMESPACE),default)
deploymentSpec = $(if $(DEPLOYMENTSPEC),$(DEPLOYMENTSPEC),$(PREFIX)/example/deployment.yaml)
testname = $(E2E_TEST)

.PHONY: all dep build container test test-indv

all: build

dep: vendor

vendor:
	GOPATH=$(GOPATH) glide install --strip-vendor

build: dep $(BINARY)

$(BINARY): $(SOURCE)
	./scripts/codegen/update-generated.sh
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o build/bin/couchbase-operator ./cmd/operator/main.go

container: build
	docker build -t couchbase/couchbase-operator:v1 .

test:
	go test github.com/couchbase/couchbase-operator/test/e2e -run TestAll -v -timeout 360m \
		--race --kubeconfig $(kubeconfig) --operator-image $(operatorImage) \
		--namespace $(namespace) --deployment-spec $(deploymentSpec)

test-sanity:
	go test github.com/couchbase/couchbase-operator/test/e2e -run TestSanity -v -timeout 240m \
		--race --kubeconfig $(kubeconfig) --operator-image $(operatorImage) \
		--namespace $(namespace) --deployment-spec $(deploymentSpec)

test-p0:
	go test github.com/couchbase/couchbase-operator/test/e2e -run TestP0 -v -timeout 240m \
		--race --kubeconfig $(kubeconfig) --operator-image $(operatorImage) \
		--namespace $(namespace) --deployment-spec $(deploymentSpec)

test-p1:
	go test github.com/couchbase/couchbase-operator/test/e2e -run TestP1 -v -timeout 240m \
		--race --kubeconfig $(kubeconfig) --operator-image $(operatorImage) \
		--namespace $(namespace) --deployment-spec $(deploymentSpec)

test-indv:
	go test github.com/couchbase/couchbase-operator/test/e2e -run $(testname) \
		-v -timeout 60m --race --kubeconfig $(kubeconfig) --operator-image \
		$(operatorImage) --namespace $(namespace) --deployment-spec \
		$(deploymentSpec)
