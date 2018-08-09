FROM alpine:3.6

ADD docs/License.txt /License.txt
ADD docs/README.txt /README.txt
ADD build/bin/couchbase-operator /usr/local/bin/couchbase-operator

