FROM scratch

ARG TARGETARCH

COPY passwd /etc/passwd

COPY License.txt /License.txt
COPY README.txt /README.txt
COPY kubernetes-linux-${TARGETARCH}/couchbase-operator /usr/local/bin/couchbase-operator

USER 8453
