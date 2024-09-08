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
	"time"

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
func (c *Client) Do(req *Request, result interface{}, timeout time.Duration) error {
	if req == nil {
		return fmt.Errorf("perform request: %w", ErrHTTPRequestIsNil)
	}

	err := ValidateHTTPRequest(req)
	if err != nil {
		return fmt.Errorf("perform request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var resp *http.Response

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("perform request timed out after %v: %w", timeout, err)
		default:
			resp, err = c.makeRequest(req)
			if err == nil {
				return c.handleResponse(resp, result)
			}

			time.Sleep(time.Second) // Wait before retrying
		}
	}
}

// makeRequest creates and sends an HTTP request.
func (c *Client) makeRequest(req *Request) (*http.Response, error) {
	url := fmt.Sprintf("%s%s", req.Host, req.Path)

	var bodyReader *bytes.Reader

	if req.Body != nil {
		bodyBytes, err := json.Marshal(req.Body)
		if err != nil {
			return nil, fmt.Errorf("make http request: marshal request body: %w", err)
		}

		bodyReader = bytes.NewReader(bodyBytes)
	} else {
		bodyReader = bytes.NewReader([]byte{})
	}

	httpReq, err := http.NewRequest(req.Method, url, bodyReader)
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
	case "application/json":
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
