FROM golang:1.24.7-trixie

WORKDIR /tmp/src/github.com/couchbase/couchbase-operator
ADD go.mod go.sum ./

RUN go mod download

ADD . .

RUN make pkg/apis/couchbase/v2/zz_generated.deepcopy.go
RUN make pkg/generated/clientset
RUN make pkg/generated/listers
RUN make pkg/generated/informers
RUN make example/crd.yaml

RUN make binary -e BINARY=cao
RUN make binary -e BINARY=cbopcfg
RUN make binary -e BINARY=cbopinfo
RUN make binary -e BINARY=couchbase-operator
RUN make binary -e BINARY=couchbase-admission-controller

