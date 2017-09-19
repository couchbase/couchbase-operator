FROM ceejatec/centos-65-couchbase-build
ADD build/bin/couchbase-operator /usr/local/bin
CMD ["/bin/sh", "-c", "/usr/local/bin/couchbase-operator"]
