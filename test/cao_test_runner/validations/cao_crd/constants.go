package caocrdvalidator

const (
	operatorPath string = "couchbase-autonomous-operator_%s-%s-%s-%s-%s%s"
)

var versionCRDs = map[string][]string{
	"2.8.0": {
		"couchbaseautoscalers.couchbase.com",
		"couchbasebackuprestores.couchbase.com",
		"couchbasebackups.couchbase.com",
		"couchbasebuckets.couchbase.com",
		"couchbaseclusters.couchbase.com",
		"couchbasecollectiongroups.couchbase.com",
		"couchbasecollections.couchbase.com",
		"couchbaseephemeralbuckets.couchbase.com",
		"couchbasegroups.couchbase.com",
		"couchbasememcachedbuckets.couchbase.com",
		"couchbasemigrationreplications.couchbase.com",
		"couchbasereplications.couchbase.com",
		"couchbaserolebindings.couchbase.com",
		"couchbasescopegroups.couchbase.com",
		"couchbasescopes.couchbase.com",
		"couchbaseusers.couchbase.com",
	},
}
