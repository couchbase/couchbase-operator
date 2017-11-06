FROM alpine:3.6
ADD build/bin/couchbase-operator /usr/local/bin
CMD ["/bin/sh", "-c", "/usr/local/bin/couchbase-operator"]
