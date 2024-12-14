package requestutils

import (
	"errors"
	"fmt"
	"net/http"
)

var (
	ErrInvalidHTTPMethod        = errors.New("invalid HTTP method provided")
	ErrInvalidHeader            = errors.New("invalid header type")
	ErrInvalidHeaderContentType = errors.New("invalid HTTP header content-type")
)

// ValidateHTTPRequest checks if the Request struct has valid Method and Headers.
func ValidateHTTPRequest(req *Request) error {
	// Validate Method
	if err := validateHTTPMethod(req.Method); err != nil {
		return err
	}

	// Validate Headers
	if err := validateHeaders(req.Headers); err != nil {
		return err
	}

	return nil
}

// validateHTTPMethod checks if the given method is a valid HTTP method.
func validateHTTPMethod(method string) error {
	validMethods := map[string]bool{
		http.MethodGet:    true,
		http.MethodPost:   true,
		http.MethodPut:    true,
		http.MethodDelete: true,
		http.MethodPatch:  true,
	}

	if val, ok := validMethods[method]; ok && val {
		return nil
	}

	return fmt.Errorf("validate HTTP method %s: %w", method, ErrInvalidHTTPMethod)
}

// validateHeaders checks if the headers are valid.
func validateHeaders(headers map[string]string) error {
	if headers == nil {
		headers = make(map[string]string)
	}

	for key, value := range headers {
		switch key {
		case "Content-Type":
			{
				return validateContentType(value)
			}
		default:
			{
				return fmt.Errorf("validate header %s=%s: %w", key, value, ErrInvalidHeader)
			}
		}
	}

	return nil
}

// validateContentType checks if the Content-Type header value is valid.
func validateContentType(contentType string) error {
	validContentTypes := map[string]bool{
		"application/json":                  true,
		"application/xml":                   true,
		"application/x-www-form-urlencoded": true,
		"text/plain":                        true,
	}

	if val, ok := validContentTypes[contentType]; ok && val {
		return nil
	}

	return fmt.Errorf("validate Content-Type %s: %w", contentType, ErrInvalidHeaderContentType)
}
