package clusternodes

import (
	"net/url"
	"strconv"
	"strings"

	requestutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/request"
)

// RebalanceStart starts a rebalance.
/*
 * POST :: /controller/rebalance.
 * docs.couchbase.com/server/current/rest-api/rest-cluster-rebalance.html.
 * Unmarshal into the map[string]interface{}.
 * NOTE: The CB node names shall be otp node names. E.g. ns_1@<cb-node-name>.
 * NOTE: To eject an active node, add it to the ejectedNodes.
 */
func RebalanceStart(hostname string, knownNodes, ejectedNodes, deltaRecoveryBuckets []string) *requestutils.Request {
	formData := url.Values{}

	knownNodes = ConvertToOTPNames(knownNodes)

	formData.Set("knownNodes", url.PathEscape(strings.Join(knownNodes, ",")))

	if ejectedNodes != nil {
		ejectedNodes = ConvertToOTPNames(ejectedNodes)
		formData.Set("ejectedNodes", url.PathEscape(strings.Join(ejectedNodes, ",")))
	}

	if deltaRecoveryBuckets != nil {
		formData.Set("deltaRecoveryBuckets", url.PathEscape(strings.Join(deltaRecoveryBuckets, ",")))
	}

	// TODO `topology` to be added later. Valid from Morpheus release to update services dynamically

	return &requestutils.Request{
		Host:   hostname,
		Path:   "/controller/rebalance",
		Method: "POST",
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
		Body: formData.Encode(),
	}
}

// RebalanceProgress retrieves the rebalance progress.
/*
 * GET :: /pools/default/rebalanceProgress.
 * docs.couchbase.com/server/current/rest-api/rest-get-rebalance-progress.html.
 * Unmarshal into the map[string]interface{}.
 */
func RebalanceProgress(hostname string) *requestutils.Request {
	return &requestutils.Request{
		Host:   hostname,
		Path:   "/pools/default/rebalanceProgress",
		Method: "GET",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}
}

// RebalanceReport retrieves the rebalance report using the given report ID.
/*
 * GET :: /logs/rebalanceReport?reportID=<report-id>.
 * docs.couchbase.com/server/current/rest-api/rest-get-cluster-tasks.html.
 * Unmarshal into the map[string]interface{}.
 */
func RebalanceReport(hostname, reportID string) *requestutils.Request {
	return &requestutils.Request{
		Host:   hostname,
		Path:   "/logs/rebalanceReport?reportID=" + reportID,
		Method: "GET",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}
}

// RebalanceStop stops an ongoing rebalance.
/*
 * POST :: /controller/stopRebalance.
 * CB Documentation not available.
 * Success gives 200 OK. Failure to authenticate gives 401 Unauthorized. A malformed URI gives 404 Object Not Found.
 */
func RebalanceStop(hostname string, port string) *requestutils.Request {
	if port == "" {
		port = "8091"
	}
	return &requestutils.Request{
		Host:   hostname,
		Port:   port,
		Path:   "/controller/stopRebalance",
		Method: "POST",
		Body:   nil,
	}
}

type RebalanceRetryStruct struct {
	Enabled         bool `json:"enabled"`
	AfterTimePeriod int  `json:"afterTimePeriod"` // Time period in seconds. Values from 5 to 3600.
	MaxAttempts     int  `json:"maxAttempts"`     // Values between 1 and 3.
}

// RebalanceRetry gets or sets the rebalance retry settings.
/*
 * GET / POST :: /settings/retryRebalance
 * docs.couchbase.com/server/current/rest-api/rest-configure-rebalance-retry.html.
 * Unmarshal into RebalanceRetryStruct for GET / POST.
 * For POST request, the request body has x-www-form-urlencoded data based on RebalanceRetryStruct.
 */
func RebalanceRetry(hostname string, port string, get bool, enabled bool, afterTimePeriod, maxAttempts int) *requestutils.Request {
	// Performs GET request if enabled = true as we want to set the rebalance retry settings.
	if port == "" {
		port = "8091"
	}
	if get {
		return &requestutils.Request{
			Host:   hostname,
			Port:   port,
			Path:   "/settings/retryRebalance",
			Method: "GET",
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
		}
	}

	// Else, POST request is made with the body having x-www-form-urlencoded data.
	formData := url.Values{}

	formData.Set("enabled", strconv.FormatBool(enabled))
	formData.Set("afterTimePeriod", strconv.Itoa(afterTimePeriod))
	formData.Set("maxAttempts", strconv.Itoa(maxAttempts))

	return &requestutils.Request{
		Host:   hostname,
		Port:   port,
		Path:   "/settings/retryRebalance",
		Method: "POST",
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
		Body: formData.Encode(),
	}
}

// RebalancePendingRetry retrieves the pending rebalance retries.
/*
 * GET :: /pools/default/pendingRetryRebalance.
 * docs.couchbase.com/server/current/rest-api/rest-get-rebalance-retry.html.
 * Unmarshal into map[string]interface{}.
 */
func RebalancePendingRetry(hostname string) *requestutils.Request {
	return &requestutils.Request{
		Host:   hostname,
		Path:   "/pools/default/pendingRetryRebalance",
		Method: "GET",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}
}

// RebalanceCancelRetry cancels the rebalance retries.
/*
 * POST :: /controller/cancelRebalanceRetry/<rebalance-id>.
 * docs.couchbase.com/server/current/rest-api/rest-cancel-rebalance-retry.html.
 * Prior to using this, retrieve the rebalanceID using RebalancePendingRetry().
 * Success gives 200 OK. Failure to authenticate gives 401 Unauthorized. A malformed URI gives 404 Object Not Found.
 */
func RebalanceCancelRetry(hostname, rebalanceID string) *requestutils.Request {
	return &requestutils.Request{
		Host:   hostname,
		Path:   "/controller/cancelRebalanceRetry/" + rebalanceID,
		Method: "POST",
	}
}
