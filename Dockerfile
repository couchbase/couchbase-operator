FROM scratch

ADD scripts/passwd /etc/passwd

ADD docs/License.txt /License.txt
ADD docs/README.txt /README.txt
ADD build/bin/couchbase-operator /usr/local/bin/couchbase-operator

USER 8453
