FROM scratch

ARG TARGETARCH

COPY passwd /etc/passwd

COPY License.txt /License.txt
COPY README.txt /README.txt
COPY kubernetes-linux-${TARGETARCH}/couchbase-admission-controller /usr/local/bin/

USER 8453
