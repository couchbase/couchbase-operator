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
version = $(if $(VERSION),$(VERSION),2.0.0)
productVersion = $(version)-$(bldNum)
testname = $(E2E_TEST)

# Set this to, for example beta1, for a beta release.
# This will affect the "-v" version strings and docker images.
# This is analogous to revisions in DEB and RPM archives.
revision = $(if $(REVISION),$(REVISION),)

# Red Hat CC has its own revisioning system that adds another one
# on above and beyond ours.  This should always be 1
revisionRedHat = $(if $(REVISION_REDHAT),$(REVISION_REDHAT),1)

# These are propagated into each binary so we can tell for sure the exact build
# that a binary came from.
LDFLAGS = "-X github.com/couchbase/couchbase-operator/pkg/version.Version=$(version) -X github.com/couchbase/couchbase-operator/pkg/version.Revision=$(revision) -X github.com/couchbase/couchbase-operator/pkg/version.RevisionRedHat=$(revisionRedHat) -X github.com/couchbase/couchbase-operator/pkg/version.BuildNumber=$(bldNum)"

.PHONY: all dep build container dist test test-indv

all: build

dep: vendor

vendor:
	GOPATH=$(GOPATH) dep ensure -vendor-only

build: dep $(BINARY)

$(BINARY): $(SOURCE)
	./scripts/codegen/revision
	rm -rf pkg/generated
	./scripts/codegen/update-generated.sh
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o build/bin/couchbase-operator -ldflags $(LDFLAGS) ./cmd/operator/main.go
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o build/bin/couchbase-operator-admission -ldflags $(LDFLAGS) ./cmd/admission
	GOARCH=amd64 CGO_ENABLED=0 go build -o build/bin/crdgen -ldflags $(LDFLAGS) ./cmd/crdgen/
	GOARCH=amd64 CGO_ENABLED=0 go build -o build/bin/cbopinfo -ldflags $(LDFLAGS) ./cmd/cbopinfo
	GOARCH=amd64 CGO_ENABLED=0 go build -o build/bin/cbopcfg -ldflags $(LDFLAGS) ./cmd/cbopcfg
	GOARCH=amd64 CGO_ENABLED=0 go build -o build/bin/cbopconv -ldflags $(LDFLAGS) ./cmd/cbopconv
	build/bin/crdgen > example/crd.yaml

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

tools-kubernetes: PLATFORM=kubernetes
tools-kubernetes: tools-platform-specific

tools-openshift: PLATFORM=openshift
tools-openshift: GO_BUILD_FLAGS=-tags redhat
tools-openshift: tools-platform-specific

tools: build
	$(MAKE) tools-kubernetes
	$(MAKE) tools-openshift
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o build/darwin/bin/cbopinfo -ldflags $(LDFLAGS) ./cmd/cbopinfo/
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o build/linux/bin/cbopinfo -ldflags $(LDFLAGS) ./cmd/cbopinfo/
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o build/windows/bin/cbopinfo.exe -ldflags $(LDFLAGS) ./cmd/cbopinfo/
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o build/darwin/bin/cbopconv -ldflags $(LDFLAGS) ./cmd/cbopconv
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o build/linux/bin/cbopconv -ldflags $(LDFLAGS) ./cmd/cbopconv
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o build/windows/bin/cbopconv.exe -ldflags $(LDFLAGS) ./cmd/cbopconv

tools-platform-specific:
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o build/${PLATFORM}/darwin/bin/cbopcfg -ldflags $(LDFLAGS) ${GO_BUILD_FLAGS} ./cmd/cbopcfg/
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o build/${PLATFORM}/linux/bin/cbopcfg -ldflags $(LDFLAGS) ${GO_BUILD_FLAGS} ./cmd/cbopcfg/
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o build/${PLATFORM}/windows/bin/cbopcfg.exe -ldflags $(LDFLAGS) ${GO_BUILD_FLAGS} ./cmd/cbopcfg/

artifacts: tools
	WORKSPACE_DIR=$(PREFIX) ./scripts/artifact_gen.sh --platform kubernetes --os darwin --version $(version) --bld_num $(bldNum)
	WORKSPACE_DIR=$(PREFIX) ./scripts/artifact_gen.sh --platform kubernetes --os linux --version $(version) --bld_num $(bldNum)
	WORKSPACE_DIR=$(PREFIX) ./scripts/artifact_gen.sh --platform kubernetes --os windows --version $(version) --bld_num $(bldNum)
	WORKSPACE_DIR=$(PREFIX) ./scripts/artifact_gen.sh --platform openshift --os darwin --version $(version) --bld_num $(bldNum)
	WORKSPACE_DIR=$(PREFIX) ./scripts/artifact_gen.sh --platform openshift --os linux --version $(version) --bld_num $(bldNum)
	WORKSPACE_DIR=$(PREFIX) ./scripts/artifact_gen.sh --platform openshift --os windows --version $(version) --bld_num $(bldNum)

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
	cp build/couchbase-autonomous-operator-*.zip dist

prod: container tools artifacts

prod-rhel: container-rhel tools-rhel artifacts

test-operator:
	go test github.com/couchbase/couchbase-operator/test/e2e -run TestOperator -v --race -timeout 240m

test-unit:
	go test -v ./pkg/...

test-helm:
	ct install --charts helm/test-resources/ --namespace ci-testnamespace
