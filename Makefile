PREFIX ?= $(shell pwd)
GOPATH = $(shell echo $${PWD%/src/*})
SOURCE = $(shell find . -name *.go -type f)
BINARY = build/bin/couchbase-operator
ARTIFACTS = build/artifacts

kubeconfig = $(if $(KUBECONFIG),$(KUBECONFIG),$(HOME)/.kube/config)
operatorImage = $(if $(OPERATOR_IMAGE),$(OPERATOR_IMAGE),couchbase/couchbase-operator:v1)
namespace = $(if $(KUBENAMESPACE),$(KUBENAMESPACE),default)
deploymentSpec = $(if $(DEPLOYMENTSPEC),$(DEPLOYMENTSPEC),$(PREFIX)/example/deployment.yaml)
productVersion = $(if $(VERSION),$(VERSION),1.1.0)
testname = $(E2E_TEST)

.PHONY: all dep build container test test-indv

all: build

dep: vendor

vendor:
	GOPATH=$(GOPATH) glide install --strip-vendor

build: dep $(BINARY)

$(BINARY): $(SOURCE)
	./scripts/codegen/revision
	./scripts/codegen/update-generated.sh
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o build/bin/couchbase-operator ./cmd/operator/main.go
	GOARCH=amd64 CGO_ENABLED=0 go build -o build/bin/cbopctl ./cmd/cbopctl/
	GOARCH=amd64 CGO_ENABLED=0 go build -o build/bin/crdgen ./cmd/crdgen/
	GOARCH=amd64 CGO_ENABLED=0 go build -o build/bin/cbopinfo ./cmd/cbopinfo
	build/bin/crdgen -outfile example/crd.yaml

container: build
	docker build -f Dockerfile -t couchbase/couchbase-operator:v1 .

container-rhel: build
	docker build -f Dockerfile.rhel --build-arg OPERATOR_BUILD=$(OPERATOR_BUILD) --build-arg OS_BUILD=$(BUILD) --build-arg PROD_VERSION=$(VERSION) -t couchbase/couchbase-operator-rhel:v1 .

tools:
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o build/darwin/bin/cbopctl ./cmd/cbopctl/
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o build/linux/bin/cbopctl ./cmd/cbopctl/
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o build/windows/bin/cbopctl ./cmd/cbopctl/
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o build/darwin/bin/cbopinfo ./cmd/cbopinfo/
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o build/linux/bin/cbopinfo ./cmd/cbopinfo/
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o build/windows/bin/cbopinfo ./cmd/cbopinfo/

artifacts:
	WORKSPACE_DIR=$(PREFIX) ./scripts/artifact_gen.sh --platform kubernetes --os darwin --version $(productVersion)
	WORKSPACE_DIR=$(PREFIX) ./scripts/artifact_gen.sh --platform kubernetes --os linux --version $(productVersion)
	WORKSPACE_DIR=$(PREFIX) ./scripts/artifact_gen.sh --platform kubernetes --os windows --version $(productVersion)
	WORKSPACE_DIR=$(PREFIX) ./scripts/artifact_gen.sh --platform openshift --os darwin --version $(productVersion)
	WORKSPACE_DIR=$(PREFIX) ./scripts/artifact_gen.sh --platform openshift --os linux --version $(productVersion)
	WORKSPACE_DIR=$(PREFIX) ./scripts/artifact_gen.sh --platform openshift --os windows --version $(productVersion)

prod: container tools artifacts

prod-rhel: container-rhel tools artifacts

test-operator:
	go test github.com/couchbase/couchbase-operator/test/e2e -run TestOperator -v --race -timeout 240m

test-unit:
	go test -v github.com/couchbase/couchbase-operator/pkg/validator
	go test -v github.com/couchbase/couchbase-operator/pkg/util/scheduler
	go test -v github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil
	go test -v github.com/couchbase/couchbase-operator/pkg/util/k8sutil
