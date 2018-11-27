PREFIX ?= $(shell pwd)
GOPATH = $(shell echo $${PWD%/src/*})
SOURCE = $(shell find . -name *.go -type f)
BINARY = build/bin/couchbase-operator
BINARY_ADMISSION = build/bin/couchbase-operator-admission
ARTIFACTS = build/artifacts

kubeconfig = $(if $(KUBECONFIG),$(KUBECONFIG),$(HOME)/.kube/config)
operatorImage = $(if $(OPERATOR_IMAGE),$(OPERATOR_IMAGE),couchbase/couchbase-operator:v1)
namespace = $(if $(KUBENAMESPACE),$(KUBENAMESPACE),default)
deploymentSpec = $(if $(DEPLOYMENTSPEC),$(DEPLOYMENTSPEC),$(PREFIX)/example/deployment.yaml)
bldNum = $(if $(BLD_NUM),$(BLD_NUM),999)
productVersion = $(if $(VERSION),$(VERSION)-$(bldNum),1.2.0-999)
testname = $(E2E_TEST)

.PHONY: all dep build container dist test test-indv

all: build

dep: vendor

vendor:
	GOPATH=$(GOPATH) dep ensure -vendor-only

build: dep $(BINARY)

$(BINARY): $(SOURCE)
	./scripts/codegen/revision
	./scripts/codegen/update-generated.sh
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o build/bin/couchbase-operator ./cmd/operator/main.go
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o build/bin/couchbase-operator-admission ./cmd/admission
	GOARCH=amd64 CGO_ENABLED=0 go build -o build/bin/cbopctl ./cmd/cbopctl/
	GOARCH=amd64 CGO_ENABLED=0 go build -o build/bin/crdgen ./cmd/crdgen/
	GOARCH=amd64 CGO_ENABLED=0 go build -o build/bin/cbopinfo ./cmd/cbopinfo
	build/bin/crdgen -outfile example/crd.yaml

# NOTE: This target is only for local development. While we use this Dockerfile
# (for now), the actual "docker build" command is located in the Jenkins job
# "couchbase-operator-docker". We could make use of this Makefile there as
# well, but it is quite possible in future that the canonical Dockerfile will
# need to be moved to a separate repo in which case the "docker build" command
# can't be here anyway.
container: build
	docker build -f Dockerfile -t couchbase/couchbase-operator:v1 .
	docker build -f Dockerfile.admission -t couchbase/couchbase-operator-admission:v1 .

# NOTE: This target is only for local development. While we use this Dockerfile
# (for now), the actual "docker build" command is located in the Jenkins job
# "couchbase-operator-docker". We could make use of this Makefile there as
# well, but it is quite possible in future that the canonical Dockerfile will
# need to be moved to a separate repo in which case the "docker build" command
# can't be here anyway.
container-rhel: build
	docker build -f Dockerfile.rhel --build-arg OPERATOR_BUILD=$(OPERATOR_BUILD) --build-arg OS_BUILD=$(BUILD) --build-arg PROD_VERSION=$(VERSION) -t couchbase/couchbase-operator-rhel:v1 .

tools: build
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o build/darwin/bin/cbopctl ./cmd/cbopctl/
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o build/linux/bin/cbopctl ./cmd/cbopctl/
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o build/windows/bin/cbopctl ./cmd/cbopctl/
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o build/darwin/bin/cbopinfo ./cmd/cbopinfo/
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o build/linux/bin/cbopinfo ./cmd/cbopinfo/
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o build/windows/bin/cbopinfo ./cmd/cbopinfo/

artifacts: tools
	WORKSPACE_DIR=$(PREFIX) ./scripts/artifact_gen.sh --platform kubernetes --os darwin --version $(productVersion)
	WORKSPACE_DIR=$(PREFIX) ./scripts/artifact_gen.sh --platform kubernetes --os linux --version $(productVersion)
	WORKSPACE_DIR=$(PREFIX) ./scripts/artifact_gen.sh --platform kubernetes --os windows --version $(productVersion)
	WORKSPACE_DIR=$(PREFIX) ./scripts/artifact_gen.sh --platform openshift --os darwin --version $(productVersion)
	WORKSPACE_DIR=$(PREFIX) ./scripts/artifact_gen.sh --platform openshift --os linux --version $(productVersion)
	WORKSPACE_DIR=$(PREFIX) ./scripts/artifact_gen.sh --platform openshift --os windows --version $(productVersion)

image-artifacts: build
	# Create a subdirectory for the operator docker build
	mkdir -p $(ARTIFACTS)/operator/docs
	mkdir -p $(ARTIFACTS)/operator/build/bin
	cp docs/License.txt $(ARTIFACTS)/operator/docs/License.txt
	cp docs/README.txt $(ARTIFACTS)/operator/docs/README.txt
	cp $(BINARY) $(ARTIFACTS)/operator/$(BINARY)
	cp Dockerfile $(ARTIFACTS)/operator/Dockerfile
	cp Dockerfile.rhel $(ARTIFACTS)/operator/Dockerfile.rhel
	# Create a subdirectory for the admission controller docker build
	mkdir -p $(ARTIFACTS)/admission/docs
	mkdir -p $(ARTIFACTS)/admission/build/bin
	cp docs/License.txt $(ARTIFACTS)/admission/docs/License.txt
	cp docs/README.txt $(ARTIFACTS)/admission/docs/README.txt
	cp $(BINARY_ADMISSION) $(ARTIFACTS)/admission/$(BINARY_ADMISSION)
	cp Dockerfile.admission $(ARTIFACTS)/admission/Dockerfile
	cp Dockerfile.admission-rhel $(ARTIFACTS)/admission/Dockerfile.rhel
	# Create the archive
	tar -C $(ARTIFACTS) -czf build/couchbase-autonomous-operator-image_$(productVersion).tgz .
	rm -rf $(ARTIFACTS)

# This target (and only this target) is invoked by the production build job.
# This job will archive all files that end up in the dist/ directory.
dist: artifacts image-artifacts
	rm -rf dist
	mkdir dist
	cp build/couchbase-autonomous-operator-image_*.tgz dist
	cp build/couchbase-autonomous-operator-*.tar.gz dist

prod: container tools artifacts

prod-rhel: container-rhel tools artifacts

test-operator:
	go test github.com/couchbase/couchbase-operator/test/e2e -run TestOperator -v --race -timeout 240m

test-unit:
	go test -v ./pkg/...
