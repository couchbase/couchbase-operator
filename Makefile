PREFIX ?= $(shell pwd)
GOPATH = $(shell echo $${PWD%/src/*})
SOURCE = $(shell find . -name *.go -type f)
BINARY = build/bin/couchbase-operator
ARTIFACTS = build/artifacts

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
	./scripts/codegen/revision
	./scripts/codegen/update-generated.sh
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o build/bin/couchbase-operator ./cmd/operator/main.go
	GOARCH=amd64 CGO_ENABLED=0 go build -o build/bin/cbopctl ./cmd/cbopctl/
	GOARCH=amd64 CGO_ENABLED=0 go build -o build/bin/crdgen ./cmd/crdgen/
	GOARCH=amd64 CGO_ENABLED=0 go build -o build/bin/cbopinfo ./cmd/cbopinfo
	build/bin/crdgen -outfile example/crd.yaml

container: build
	docker build -t couchbase/couchbase-operator:v1 .

prod: container
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o build/darwin/bin/cbopctl ./cmd/cbopctl/
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o build/linux/bin/cbopctl ./cmd/cbopctl/
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o build/windows/bin/cbopctl ./cmd/cbopctl/
	mkdir -p $(ARTIFACTS)
	cp -r build/darwin $(ARTIFACTS)
	cp -r build/linux $(ARTIFACTS)
	cp -r build/windows $(ARTIFACTS)
	cp example/crd.yaml $(ARTIFACTS)/crd.yaml
	cp example/couchbase-cluster.yaml $(ARTIFACTS)/couchbase-cluster.yaml
	cp example/deployment.yaml $(ARTIFACTS)/operator.yaml
	cp example/tools/couchbase-cli-create-user.yaml $(ARTIFACTS)/couchbase-cli-create-user.yaml
	cp example/tools/pillowfight-data-loader.yaml $(ARTIFACTS)/pillowfight.yaml
	cp example/tools/pillowfight-data-loader-openshift.yaml $(ARTIFACTS)/pillowfight-openshift.yaml
	tar -czf $(ARTIFACTS)/rbac.zip example/rbac
	cd build && tar -czf artifacts.zip artifacts && cd ..
	rm -r $(ARTIFACTS)

test-operator:
	go test github.com/couchbase/couchbase-operator/test/e2e -run TestOperator -v --race -timeout 240m

test-unit:
	go test -v github.com/couchbase/couchbase-operator/pkg/validator
	go test -v github.com/couchbase/couchbase-operator/pkg/util/scheduler
	go test -v github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil
	go test -v github.com/couchbase/couchbase-operator/pkg/util/k8sutil
