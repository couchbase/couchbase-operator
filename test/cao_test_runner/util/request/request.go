package requestutils

import (
	"fmt"
	"io"
	"net/http"
	"os"

	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
)

type RequestParams struct {
	URL string
}

func NewRequestParams(url string) *RequestParams {
	return &RequestParams{
		URL: url,
	}
}

func (requestParams *RequestParams) DownloadFile(outputFile *fileutils.File) error {
	err := outputFile.OpenFile(os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("error opening file: %w", err)
	}

	defer outputFile.CloseFile()

	resp, err := http.Get(requestParams.URL)
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
