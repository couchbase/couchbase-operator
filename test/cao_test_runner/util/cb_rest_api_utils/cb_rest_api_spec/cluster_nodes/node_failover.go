package clusternodes

import (
	"net/url"
	"strconv"

	requestutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/request"
)

// PerformHardFailover initiates a hard failover for the specified OTP nodes.
/*
 * POST :: /controller/failOver.
 * docs.couchbase.com/server/current/rest-api/rest-node-failover.html.
 * NOTE: RebalanceStart to be performed after this with the failed over node in ejectedNodes and knownNodes both.
 */
func PerformHardFailover(hostname, port string, otpNodes []string, allowUnsafe bool) *requestutils.Request {
	if port == "" {
		port = "8091"
	}

	formData := url.Values{}

	otpNodes = ConvertToOTPNames(otpNodes)
	for _, otpNode := range otpNodes {
		formData.Add("otpNode", otpNode)
	}

	if allowUnsafe {
		formData.Set("allowUnsafe", strconv.FormatBool(allowUnsafe))
	}

	return &requestutils.Request{
		Host:   hostname,
		Port:   port,
		Path:   "/controller/failOver",
		Method: "POST",
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
		Body: formData.Encode(),
	}
}

// HardResetNode is used to reinitialize a node after an unsafe failover.
/*
 * POST :: /controller/hardResetNode.
 * docs.couchbase.com/server/current/rest-api/rest-reinitialize-node.html.
 * NOTE: All configuration settings and all data are lost.
 * NOTE: Full Admin role is required.
 */
func HardResetNode(hostname, port string) *requestutils.Request {
	if port == "" {
		port = "8091"
	}
	return &requestutils.Request{
		Host:   hostname,
		Port:   port,
		Path:   "/controller/hardResetNode",
		Method: "POST",
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
	}
}

// PerformGracefulFailover initiates a graceful failover for the specified OTP nodes.
/*
 * POST :: /controller/startGracefulFailover.
 * docs.couchbase.com/server/current/rest-api/rest-failover-graceful.html.
 * NOTE: After this perform a rebalance, specifying all nodes in the cluster (including those gracefully failed over) as knownNodes.
 * NOTE: And, additionally specifying those gracefully failed over as ejectedNodes.
 */
func PerformGracefulFailover(hostname, port string, otpNodes []string) *requestutils.Request {
	if port == "" {
		port = "8091"
	}

	formData := url.Values{}

	otpNodes = ConvertToOTPNames(otpNodes)
	for _, otpNode := range otpNodes {
		formData.Add("otpNode", otpNode)
	}

	return &requestutils.Request{
		Host:   hostname,
		Port:   port,
		Path:   "/controller/startGracefulFailover",
		Method: "POST",
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
		Body: formData.Encode(),
	}
}

// SetFailoverRecoveryType sets the recovery type for the specified OTP node.
/*
 * POST :: /controller/setRecoveryType.
 * docs.couchbase.com/server/current/rest-api/rest-node-recovery-incremental.html.
 * NOTE: The recovery type for a node is set after the node is failed over and before rebalance.
 */
func SetFailoverRecoveryType(hostname, port, otpNode, recoveryType string) *requestutils.Request {
	if port == "" {
		port = "8091"
	}

	formData := url.Values{}

	otpNodes := ConvertToOTPNames([]string{otpNode})

	formData.Set("otpNode", otpNodes[0])
	formData.Set("recoveryType", recoveryType)

	return &requestutils.Request{
		Host:   hostname,
		Port:   port,
		Path:   "/controller/setRecoveryType",
		Method: "POST",
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
		Body: formData.Encode(),
	}
}
