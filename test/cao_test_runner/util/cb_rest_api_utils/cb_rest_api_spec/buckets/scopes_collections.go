package buckets

import (
	"fmt"
	"net/url"

	requestutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/request"
)

// CreateScope creates a new scope in the specified bucket.
/*
 * POST :: /pools/default/buckets/<bucket_name>/scopes
 * docs.couchbase.com/server/current/rest-api/creating-a-scope.html
 */
func CreateScope(hostname, bucketName, scopeName string) *requestutils.Request {
	formData := url.Values{}
	formData.Set("name", scopeName)

	return &requestutils.Request{
		Host:   hostname,
		Path:   "/pools/default/buckets/" + url.PathEscape(bucketName) + "/scopes",
		Method: "POST",
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
		Body: formData.Encode(),
	}
}

// DropScope deletes a scope from the specified bucket.
/*
 * DELETE :: /pools/default/buckets/<bucket_name>/scopes/<scope_name>
 * docs.couchbase.com/server/current/rest-api/dropping-a-scope.html
 */
func DropScope(hostname, bucketName, scopeName string) *requestutils.Request {
	return &requestutils.Request{
		Host:   hostname,
		Path:   "/pools/default/buckets/" + url.PathEscape(bucketName) + "/scopes/" + url.PathEscape(scopeName),
		Method: "DELETE",
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
	}
}

// CreateCollection creates a new collection in the specified scope.
/*
 * POST :: /pools/default/buckets/<bucket_name>/scopes/<scope_name>/collections
 * docs.couchbase.com/server/current/rest-api/creating-a-collection.html
 */
func CreateCollection(hostname, bucketName, scopeName, collectionName string, maxTTL int, history bool) *requestutils.Request {
	formData := url.Values{}

	formData.Set("name", collectionName)

	if maxTTL != 0 {
		formData.Set("maxTTL", fmt.Sprintf("%d", maxTTL))
	}

	if history {
		formData.Set("history", "true")
	}

	return &requestutils.Request{
		Host:   hostname,
		Path:   "/pools/default/buckets/" + url.PathEscape(bucketName) + "/scopes/" + url.PathEscape(scopeName) + "/collections",
		Method: "POST",
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
		Body: formData.Encode(),
	}
}

// EditCollection updates the specified collection in the specified scope.
/*
 * PATCH :: /pools/default/buckets/<bucket_name>/scopes/<scope_name>/collections/<collection_name>
 * docs.couchbase.com/server/current/rest-api/creating-a-collection.html
 */
func EditCollection(hostname, bucketName, scopeName, collectionName string, maxTTL int, history bool) *requestutils.Request {
	formData := url.Values{}

	if maxTTL != 0 {
		formData.Set("maxTTL", fmt.Sprintf("%d", maxTTL))
	}

	if history {
		formData.Set("history", "true")
	}

	return &requestutils.Request{
		Host:   hostname,
		Path:   "/pools/default/buckets/" + url.PathEscape(bucketName) + "/scopes/" + url.PathEscape(scopeName) + "/collections/" + url.PathEscape(collectionName),
		Method: "PATCH",
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
		Body: formData.Encode(),
	}
}

// DropCollection deletes a collection from the specified scope.
/*
 * DELETE :: /pools/default/buckets/<bucket_name>/scopes/<scope_name>/collections/<collection_name>
 * docs.couchbase.com/server/current/rest-api/dropping-a-collection.html
 */
func DropCollection(hostname, bucketName, scopeName, collectionName string) *requestutils.Request {
	return &requestutils.Request{
		Host:   hostname,
		Path:   "/pools/default/buckets/" + url.PathEscape(bucketName) + "/scopes/" + url.PathEscape(scopeName) + "/collections/" + url.PathEscape(collectionName),
		Method: "DELETE",
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
	}
}

// ListScopesCollections lists all collections in the specified bucket.
/*
 * GET :: /pools/default/buckets/<bucket_name>/scopes/
 * docs.couchbase.com/server/current/rest-api/listing-scopes-and-collections.html
 * Unmarshal into ListScopesCollectionsStruct.
 */
func ListScopesCollections(hostname, bucketName string) *requestutils.Request {
	return &requestutils.Request{
		Host:   hostname,
		Path:   "/pools/default/buckets/" + url.PathEscape(bucketName) + "/scopes/",
		Method: "GET",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}
}
