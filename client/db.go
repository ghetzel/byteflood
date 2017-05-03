package client

import (
	"bytes"
	"encoding/json"
)

type DatabaseScanRequest struct {
	Labels   []string `json:"labels"`
	DeepScan bool     `json:"deep"`
}

func (self *Client) Scan(deep bool, labels ...string) error {
	buf := bytes.NewBuffer(nil)
	if err := json.NewEncoder(buf).Encode(&DatabaseScanRequest{
		DeepScan: deep,
		Labels:   labels,
	}); err == nil {
		_, err := self.Request(`POST`, `/api/db/actions/scan`, nil, nil, buf)
		return err
	} else {
		return err
	}
}

func (self *Client) Cleanup() error {
	_, err := self.Request(`POST`, `/api/db/actions/cleanup`, nil, nil, nil)
	return err
}
