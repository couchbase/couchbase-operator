package requestutils

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	"github.com/sirupsen/logrus"
)

var (
	ErrHTTPRequestIsNil  = errors.New("http request is nil")
	ErrHTTPRequestFailed = errors.New("http request failed")
	ErrUnableToDecode    = errors.New("unable to decode")
)

type HTTPAuth struct {
	Username string
	Password string
}

type Client struct {
	httpClient *http.Client
	httpAuth   *HTTPAuth
}

type PortForwardConfig struct {
	PodName string
	Port    string
}

// NewClient creates a new HTTP client. httpAuth is by default nil. Set it using SetHTTPAuth.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{},
		httpAuth:   nil,
	}
}

// SetHTTPAuth sets the HTTP authentication details.
func (c *Client) SetHTTPAuth(username, password string) {
	c.httpAuth = &HTTPAuth{
		Username: username,
		Password: password,
	}
}

// Do perform the API request.
func (c *Client) Do(req *Request, result interface{}, timeout time.Duration, portForwardConfig *PortForwardConfig) error {
	if req == nil {
		return fmt.Errorf("perform request: %w", ErrHTTPRequestIsNil)
	}

	done := make(chan struct{})
	stopCh := make(chan struct{})

	if portForwardConfig != nil {
		kubectl.PortForward(portForwardConfig.PodName, portForwardConfig.Port, portForwardConfig.Port, stopCh, done)
		req.Host = "localhost"
	}

	// Checking the hostname
	// TODO split the hostname and port.
	// TODO update GetHTTPHostname to have isSecure parameter to support https.
	hostname, err := GetHTTPHostname(req.Host, req.Port)
	if err != nil {
		return fmt.Errorf("new cluster nodes api: %w", err)
	}

	req.Host = hostname

	err = ValidateHTTPRequest(req)
	if err != nil {
		return fmt.Errorf("perform %s request %s%s: %w", req.Method, req.Host, req.Path, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var resp *http.Response

	for {
		select {
		case <-ctx.Done():
			if portForwardConfig != nil {
				close(stopCh)
				<-done
			}
			return fmt.Errorf("perform %s request %s%s timed out after %v: %w", req.Method, req.Host, req.Path, timeout, err)
		default:
			resp, err = c.makeRequest(req)
			if err == nil {
				if portForwardConfig != nil {
					close(stopCh)
					<-done
				}
				return c.handleResponse(resp, result)
			}

			time.Sleep(time.Second) // Wait before retrying
		}
	}
}

// makeRequest creates and sends an HTTP request.
func (c *Client) makeRequest(req *Request) (*http.Response, error) {
	reqURL := fmt.Sprintf("%s%s", req.Host, req.Path)

	var bodyReader *bytes.Reader

	// Marshal the contents of the body based on the `Content-Type` header.
	if req.Body != nil {
		switch req.Headers["Content-Type"] {
		case "application/json":
			{
				bodyBytes, err := json.Marshal(req.Body)
				if err != nil {
					return nil, fmt.Errorf("make http request: marshal request body: %w", err)
				}

				bodyReader = bytes.NewReader(bodyBytes)
			}
		case "application/x-www-form-urlencoded":
			{
				if formData, ok := req.Body.(url.Values); ok {
					bodyReader = bytes.NewReader([]byte(formData.Encode()))
				} else if formData, ok := req.Body.(string); ok {
					bodyReader = bytes.NewReader([]byte(formData))
				} else {
					return nil, fmt.Errorf("make http request body: decode body `%v` for application/x-www-form-urlencoded: %w", req.Body, ErrUnableToDecode)
				}
			}
		default:
			return nil, fmt.Errorf("make http request with content-type=%s: %w", req.Headers["Content-Type"], ErrInvalidHeaderContentType)
		}
	} else {
		bodyReader = bytes.NewReader([]byte{})
	}

	httpReq, err := http.NewRequest(req.Method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("make http request: %w", err)
	}

	// Set the headers
	for key, value := range req.Headers {
		httpReq.Header.Set(key, value)
	}

	// Set authentication
	if c.httpAuth != nil {
		if c.httpAuth.Username != "" && c.httpAuth.Password != "" {
			httpReq.SetBasicAuth(c.httpAuth.Username, c.httpAuth.Password)
		}
	} else {
		logrus.Warnf("http request %+v does not have authentication", req)
	}

	return c.httpClient.Do(httpReq)
}

// handleResponse processes the HTTP response.
func (c *Client) handleResponse(resp *http.Response, result interface{}) error {
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("handle http response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("handle http response from url %s: body `%s` and status code %d: %w", resp.Request.URL, string(body), resp.StatusCode, ErrHTTPRequestFailed)
	}

	if result == nil {
		return nil
	}

	// Handle the content types we expect.
	switch contentType := resp.Header.Get("Content-Type"); contentType {
	case "application/json", "application/json;charset=utf-8":
		{
			if err := json.Unmarshal(body, result); err != nil {
				logrus.Errorf("handle http response for content-type %s, response:\n%s\n", contentType, string(body))
				return fmt.Errorf("handle http response for content-type %s: %w", contentType, err)
			}
		}
	case "application/xml":
		{
			if err := xml.Unmarshal(body, result); err != nil {
				logrus.Errorf("handle http response for content-type %s, response:\n%s\n", contentType, string(body))
				return fmt.Errorf("handle http response for content-type %s: %w", contentType, err)
			}
		}
	case "text/plain":
		{
			plaintext, ok := result.(*[]byte)
			if !ok {
				return fmt.Errorf("handle http response for content-type %s: %w", contentType, ErrUnableToDecode)
			}

			*plaintext = body
		}
	default:
		{
			return fmt.Errorf("handle http response for content-type %s: %w", contentType, ErrInvalidHeaderContentType)
		}
	}

	return nil
}
