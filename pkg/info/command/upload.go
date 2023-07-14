package command

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
)

func upload(address string, fileName string, proxy string) {
	fileDir, _ := os.Getwd()
	filePath := path.Join(fileDir, fileName)

	file, _ := os.Open(filePath)
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", filepath.Base(file.Name()))

	_, err := io.Copy(part, file)

	if err != nil {
		fmt.Println("Error copying file: ", err)
	}

	writer.Close()

	r, _ := http.NewRequest("PUT", address, body)
	r.Header.Add("Content-Type", writer.FormDataContentType())

	var client = new(http.Client)

	if proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err != nil {
			log.Fatal(err)
		}

		client = &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	} else {
		client = &http.Client{}
	}

	fmt.Println("Uploading " + fileName + " to " + address)

	res, err := client.Do(r)
	if err != nil {
		log.Fatal(err)
	}

	if res.StatusCode != http.StatusOK {
		fmt.Println("Error uploading the file: ", res)
	} else {
		fmt.Println("File successfully uploaded")
	}
}
