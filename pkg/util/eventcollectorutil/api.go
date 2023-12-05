package eventcollectorutil

import (
	"encoding/json"
	"io"
	"net/http"
)

type Dump struct {
	Status string
	Name   string
}

type Dumps map[string]Dump

type EventCollectorClient struct {
	URL string
}

func NewLocalClient(port string) *EventCollectorClient {
	return &EventCollectorClient{
		URL: "http://127.0.0.1:" + port,
	}
}
func (e *EventCollectorClient) GetBuffer() ([]byte, error) {
	resp, err := http.Get(e.URL + "/buffer")
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	buff, err := io.ReadAll(resp.Body)

	if err != nil {
		return nil, err
	}

	return buff, err
}

func (e *EventCollectorClient) GetDumps() (Dumps, error) {
	resp, err := http.Get(e.URL + "/dumps")
	if err != nil {
		return Dumps{}, err
	}

	defer resp.Body.Close()

	buff, err := io.ReadAll(resp.Body)

	if err != nil {
		return Dumps{}, err
	}

	var dumps Dumps
	err = json.Unmarshal(buff, &dumps)

	if err != nil {
		return Dumps{}, err
	}

	return dumps, err
}

func (e *EventCollectorClient) GetDump(d Dump) ([]byte, error) {
	resp, err := http.Get(e.URL + "/dumps/" + d.Name)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	buff, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return buff, err
}
