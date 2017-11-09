PREFIX ?= $(shell pwd)

kubeconfig = $(if $(KUBECONFIG),$(KUBECONFIG),$(HOME)/.kube/config)
operatorImage = $(if $(OPERATOR_IMAGE),$(OPERATOR_IMAGE),couchbase/couchbase-operator:v1)
namespace = $(if $(KUBENAMESPACE),$(KUBENAMESPACE),default)
testname = $(E2E_TEST)

.PHONY: all build container test test-indv

all: build

build:
	./scripts/codegen/update-generated.sh
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o build/bin/couchbase-operator ./cmd/operator/main.go

container: build
	docker build -t ritak8s18reg.azurecr.io/couchbase-operator:v1.0.2 .

test:
	go test github.com/couchbaselabs/couchbase-operator/test/e2e -v -timeout 30m \
		--race --kubeconfig $(kubeconfig) --operator-image $(operatorImage) \
		--namespace $(namespace)

test-indv:
	go test github.com/couchbaselabs/couchbase-operator/test/e2e -run $(testname) \
		-v -timeout 30m --race --kubeconfig $(kubeconfig) --operator-image \
		$(operatorImage) --namespace $(namespace)
