package requestutils

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
)

var (
	ErrHostNotProvided = errors.New("host name not provided")
	ErrPortNotProvided = errors.New("port number not provided")
)

// Request holds the details for an API request.
type Request struct {
	Host    string
	Path    string
	Port    string
	Method  string
	Body    interface{}
	Headers map[string]string
}

func NewRequestParams(url string) *Request {
	return &Request{
		Host: url,
	}
}

// DownloadFile TODO add this functionality handleResponse function.
func (req *Request) DownloadFile(outputFile *fileutils.File) error {
	url := fmt.Sprintf("%s%s", req.Host, req.Path)

	err := outputFile.OpenFile(os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("error opening file: %w", err)
	}

	defer outputFile.CloseFile()

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("error downloading file: %w", err)
	}

	defer resp.Body.Close()

	_, err = io.Copy(outputFile.OsFile, resp.Body)
	if err != nil {
		return fmt.Errorf("error saving file: %w", err)
	}

	return nil
}

func GetHTTPHostname(host string, port int64) (string, error) {
	if host == "" {
		return "", fmt.Errorf("get hostname: %w", ErrHostNotProvided)
	}

	if port == 0 {
		return "", fmt.Errorf("get hostname: %w", ErrPortNotProvided)
	}

	if !strings.HasPrefix(host, "http://") {
		host = "http://" + host
	}

	return fmt.Sprintf("%s:%d", host, port), nil
}
