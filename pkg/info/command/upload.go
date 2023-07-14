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

	var client *http.Client

	if proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err != nil {
			return err
		}

		client = &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	} else {
		client = &http.Client{}
	}

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
