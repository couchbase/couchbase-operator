package buckets

import (
	"fmt"
	"net/url"

	requestutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/request"
)

// GetBucketsInfo retrieves the details for all the buckets. If bucketName is provided then specific bucket stats are retrieved.
/*
 * GET :: /pools/default/buckets or /pools/default/buckets/<bucketName>.
 * docs.couchbase.com/server/current/rest-api/rest-buckets-summary.html.
 * Unmarshal into BucketsInfo if /buckets, else into the struct BucketInfo if /buckets/<bucketName>.
 */
func GetBucketsInfo(hostname, port, bucketName string, basicStats bool) *requestutils.Request {
	apiEndpoint := "/pools/default/buckets"

	if port == "" {
		port = "8091"
	}

	if bucketName != "" {
		bucketName = url.PathEscape(bucketName)
		apiEndpoint += "/" + bucketName
	}

	if basicStats {
		apiEndpoint += "?basic_stats=true"
	}

	return &requestutils.Request{
		Host:   hostname,
		Port:   port,
		Path:   apiEndpoint,
		Method: "GET",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}
}

// GetBucketStats retrieves statistics for a bucket.
/*
 * GET :: /pools/default/buckets/<bucketName>/stats.
 * docs.couchbase.com/server/current/rest-api/rest-bucket-stats.html.
 * Values for zoom: minute | hour | day | week | month | year. Default is minute.
 * Unmarshal into the struct BucketStats.
 */
func GetBucketStats(hostname, port, bucketName, zoom string) *requestutils.Request {
	if bucketName == "" {
		return nil
	}

	if port == "" {
		port = "8091"
	}

	bucketName = url.PathEscape(bucketName)
	apiEndpoint := "/pools/default/buckets/" + bucketName + "/stats"

	if zoom == "minute" || zoom == "hour" || zoom == "day" || zoom == "week" || zoom == "month" || zoom == "year" {
		apiEndpoint += "?zoom=" + zoom
	}

	return &requestutils.Request{
		Host:   hostname,
		Port:   port,
		Path:   apiEndpoint,
		Method: "GET",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}
}

// GetBucketStatsFromNode retrieves statistics for a bucket from a specific node.
/*
 * GET :: /pools/default/buckets/<bucketName>/nodes/<cbPodHostname>:8091/stats.
 * Values for zoom: minute | hour | day | week | month | year. Default is minute.
 * docs.couchbase.com/server/current/rest-api/rest-bucket-stats.html
 * Unmarshal into the struct BucketStats.
 */
func GetBucketStatsFromNode(hostname, port, bucketName, cbPodHostname, zoom string) *requestutils.Request {
	if bucketName == "" {
		return nil
	}

	if port == "" {
		port = "8091"
	}

	bucketName = url.PathEscape(bucketName)
	// api := "/pools/default/buckets/" + bucketName + "/nodes/" + cbPodHostname + ":8091" + "/stats"
	apiEndpoint := fmt.Sprintf("/pools/default/buckets/%s/nodes/%s:8091/stats", bucketName, cbPodHostname)

	if zoom == "minute" || zoom == "hour" || zoom == "day" || zoom == "week" || zoom == "month" || zoom == "year" {
		apiEndpoint += "?zoom=" + zoom
	}

	return &requestutils.Request{
		Host:   hostname,
		Port:   port,
		Path:   apiEndpoint,
		Method: "GET",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}
}
