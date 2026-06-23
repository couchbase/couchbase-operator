/*
Copyright 2023-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package command

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"

	"github.com/couchbase/couchbase-operator/pkg/util/netutil"
)

func upload(address string, fileName string, proxy string) error {
	fileDir, _ := os.Getwd()
	filePath := path.Join(fileDir, fileName)

	file, _ := os.Open(filePath)
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", filepath.Base(file.Name()))

	_, err := io.Copy(part, file)

	if err != nil {
		return err
	}

	writer.Close()

	r, _ := http.NewRequest(http.MethodPut, address, body)

	r.Header.Add("Content-Type", writer.FormDataContentType())

	// The upload address comes from config, so guard against it
	// pointing at a cloud metadata endpoint.
	transport := &http.Transport{
		DialContext: netutil.SafeDialer().DialContext,
	}

	if proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err != nil {
			return err
		}

		transport.Proxy = http.ProxyURL(proxyURL)
	}

	client := &http.Client{Transport: transport}

	fmt.Println("Uploading " + fileName + " to " + address)

	res, err := client.Do(r)

	if err != nil {
		return err
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		fmt.Println("Error uploading the file: ", res)
	} else {
		fmt.Println("File successfully uploaded")
	}

	return nil
}
