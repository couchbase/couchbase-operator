package couchbaseutil

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"reflect"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/version"
)

const (
	ContentTypeURLEncoded string = "application/x-www-form-urlencoded"
	ContentTypeJSON       string = "application/json"

	HeaderContentType string = "Content-Type"
	HeaderUserAgent   string = "User-Agent"

	tcpConnectTimeout = 5 * time.Second
)

// newClient creates a new HTTP client which offers connection persistence and
// also checks that the UUID of a host is what we expect when dialing before
// allowing further HTTP requests.
//
// Here be dragons!  You have been warned...
func (c *Client) makeClient() {
	// uuidCheck is a closure which binds basic HTTP authorization and cluster
	// UUID to the configuration.  It is responsible for doing a HTTP GET from
	// a new network connection and verifying that the UUID matches what we
	// expect before allowing the http.Client to be used.
	uuidCheck := func(addr string, conn net.Conn) error {
		// Checks not enabled yet i.e. cluster initialization
		if c.uuid == "" {
			return nil
		}

		// Construct a HTTP request
		req, err := http.NewRequest("GET", "/pools", nil)
		if err != nil {
			return fmt.Errorf("uuid check: %s", err.Error())
		}

		req.URL.Host = addr
		req.Header.Set("Accept-Encoding", "application/json")
		req.SetBasicAuth(c.username, c.password)

		// Perform the transaction
		if err = req.Write(conn); err != nil {
			return fmt.Errorf("uuid check: %s", err.Error())
		}

		resp, err := http.ReadResponse(bufio.NewReader(conn), req)
		if err != nil {
			return fmt.Errorf("uuid check: %s", err.Error())
		}

		// Check the status code was 2XX
		if resp.StatusCode/100 != 2 {
			return fmt.Errorf("uuid check: unexpected status code '%s' from %s", resp.Status, addr)
		}

		defer resp.Body.Close()

		// Read the body
		buffer, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("uuid check: %s", err.Error())
		}

		// Parse the JSON body into our anonymous struct, we only care about the UUID
		var body struct {
			UUID interface{}
		}

		if err = json.Unmarshal(buffer, &body); err != nil {
			return fmt.Errorf("uuid check: json error '%s' from %s", err.Error(), addr)
		}

		// UUID is a string if set or an empty array otherwise :/
		var uuid string

		switch t := body.UUID.(type) {
		case string:
			uuid = t
		case []interface{}:
			return fmt.Errorf("uuid is unset")
		default:
			return fmt.Errorf("uuid is unexpected type: %s", reflect.TypeOf(t))
		}

		// Finally check the UUID is as we expect.  Will be empty if no body was found
		if uuid != c.uuid {
			return fmt.Errorf("uuid check: wanted %s got %s from %s", c.uuid, uuid, addr)
		}

		return nil
	}

	// dialContext is a closure which binds to the uuidCheck closure which
	// is specific to the username/password/uuid of the cluster.  It is called
	// when a HTTP client first dials a host and verifies the UUID is as expected.
	dialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
		// Establish a TCP connection
		dialer := &net.Dialer{
			Timeout:   tcpConnectTimeout,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}

		conn, err := dialer.DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}

		// Check the UUID of the host matches our configuration before
		// allowing use of this connection
		if err = uuidCheck(addr, conn); err != nil {
			return nil, err
		}

		return conn, nil
	}

	// dialTLS is a closure which binds to the uuidCheck closure which
	// is specific to the username/password/uuid of the cluster.  It is called
	// when a HTTPS client first dials a host and verifies the UUID is as expected.
	dialTLS := func(network, addr string) (net.Conn, error) {
		// If the TLS configuration is explicitly set use that, otherwise
		// use a basic configuration (which won't ever work unless your cluster
		// is signed by a CA defined in the ca-certificates package)
		var tlsClientConfig *tls.Config = nil

		if c.tls != nil {
			tlsClientConfig = &tls.Config{
				RootCAs: x509.NewCertPool(),
			}

			// At the very least we need a CA certificate to attain trust in the remote end
			if ok := tlsClientConfig.RootCAs.AppendCertsFromPEM(c.tls.CACert); !ok {
				return nil, fmt.Errorf("failed to append CA certificate")
			}

			// If the remote end needs to trust us too we add a client certificate and key pair
			if c.tls.ClientAuth != nil {
				cert, err := tls.X509KeyPair(c.tls.ClientAuth.Cert, c.tls.ClientAuth.Key)
				if err != nil {
					return nil, err
				}

				tlsClientConfig.Certificates = append(tlsClientConfig.Certificates, cert)
			}
		}

		// Establish a TCP connection with TLS transport
		dialer := &net.Dialer{
			Timeout:   tcpConnectTimeout,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}

		conn, err := tls.DialWithDialer(dialer, network, addr, tlsClientConfig)
		if err != nil {
			return nil, err
		}

		// Check the UUID of the host matches our configuration before
		// allowing use of this connection
		if err = uuidCheck(addr, conn); err != nil {
			return nil, err
		}

		return conn, nil
	}

	// Create the basic client configuration to support HTTP
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext:  dialContext,
			DialTLS:      dialTLS,
			MaxIdleConns: 100,
		},
	}

	c.client = client
}

// doRequest is the generic request handler for all client calls.
// Always ensure the body is initialized by NewRequest, rather than being
// tempted to do it here.  I made the mistake of just setting Body and that
// resulted in the client using chunked encoding.  Everything works fine,
// with the exception of XDCR, so use the library!
func (c Client) doRequest(request *http.Request, requestBody []byte, result interface{}) error {
	// Do the request recording the time taken.
	start := time.Now()

	// All requests send the username and password.
	request.SetBasicAuth(c.username, c.password)

	// Rrequest logging.  Careful here, higher levels of verbosity generate a huge
	// amount more log traffic. All requests have the following common attributes.
	logLabels := []interface{}{
		"cluster", c.cluster,
		"method", request.Method,
		"url", request.URL.String(),
	}

	if log.V(2).Enabled() {
		// Contains admin password.
		logLabels = append(logLabels, "headers", request.Header)

		// May contain passwords.
		if requestBody != nil {
			logLabels = append(logLabels, "request", string(requestBody))
		}
	}

	// Logs are always emitted regardless of status.  Context is added to the log labels
	// depending on the path taken.
	defer func() {
		delta := time.Since(start)

		logLabels = append(logLabels, "time_ms", float64(delta.Nanoseconds())/1000000.0)

		log.V(1).Info("http", logLabels...)
	}()

	// Do the request.
	response, err := c.client.Do(request)
	if err != nil {
		logLabels = append(logLabels, "error", err)
		return err
	}

	logLabels = append(logLabels, "status", response.Status)

	// Read the body so we can display it for really verbose debugging.
	defer response.Body.Close()

	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return err
	}

	if log.V(2).Enabled() {
		if string(body) != "" {
			logLabels = append(logLabels, "response", string(body))
		}
	}

	// Anything outside of a 2XX we regard as an error.
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("request failed %v %v %v: %v", request.Method, request.URL.String(), response.Status, string(body))
	}

	// Don't care about the returned data, just report success.
	if result == nil {
		return nil
	}

	// Handle the content types we expect.
	switch contentType := response.Header.Get("Content-Type"); contentType {
	case "application/json":
		if err := json.Unmarshal(body, result); err != nil {
			return err
		}
	case "text/plain":
		s, ok := result.([]byte)
		if !ok {
			return fmt.Errorf("unexpected type decode for text/plain")
		}

		copy(s, body)
	default:
		return fmt.Errorf("unexpected content type %s", contentType)
	}

	return nil
}

func (c *Client) Get(r *Request, host string) error {
	req, err := http.NewRequest("GET", host+r.Path, nil)
	if err != nil {
		return ClientError{"request creation", err}
	}

	req.Header = defaultHeaders()

	return c.doRequest(req, nil, r.Result)
}

func (c *Client) Post(r *Request, host string) error {
	req, err := http.NewRequest("POST", host+r.Path, bytes.NewReader(r.Body))
	if err != nil {
		return ClientError{"request creation", err}
	}

	req.Header = defaultHeaders()
	req.Header.Set(HeaderContentType, ContentTypeURLEncoded)

	return c.doRequest(req, r.Body, r.Result)
}

func (c *Client) PostJSON(r *Request, host string) error {
	req, err := http.NewRequest("POST", host+r.Path, bytes.NewReader(r.Body))
	if err != nil {
		return ClientError{"request creation", err}
	}

	req.Header = defaultHeaders()
	req.Header.Set(HeaderContentType, ContentTypeJSON)

	return c.doRequest(req, r.Body, r.Result)
}

func (c *Client) PostNoContentType(r *Request, host string) error {
	req, err := http.NewRequest("POST", host+r.Path, bytes.NewReader(r.Body))
	if err != nil {
		return ClientError{"request creation", err}
	}

	req.Header = defaultHeaders()

	return c.doRequest(req, r.Body, r.Result)
}

func (c *Client) Put(r *Request, host string) error {
	req, err := http.NewRequest("PUT", host+r.Path, bytes.NewReader(r.Body))
	if err != nil {
		return ClientError{"request creation", err}
	}

	req.Header = defaultHeaders()
	req.Header.Set(HeaderContentType, ContentTypeURLEncoded)

	return c.doRequest(req, r.Body, nil)
}

func (c *Client) PutJSON(r *Request, host string) error {
	req, err := http.NewRequest("PUT", host+r.Path, bytes.NewReader(r.Body))
	if err != nil {
		return ClientError{"request creation", err}
	}

	req.Header = defaultHeaders()
	req.Header.Set(HeaderContentType, ContentTypeJSON)

	return c.doRequest(req, r.Body, nil)
}

func (c *Client) Delete(r *Request, host string) error {
	req, err := http.NewRequest("DELETE", host+r.Path, nil)
	if err != nil {
		return ClientError{"request creation", err}
	}

	req.Header = defaultHeaders()

	return c.doRequest(req, nil, nil)
}

func defaultHeaders() http.Header {
	headers := http.Header{}
	headers.Set(HeaderUserAgent, version.UserAgent())
	headers.Set("Accept-Encoding", "application/json, text/plain, */*")

	return headers
}
