package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/op/go-logging"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"time"
)

var log = logging.MustGetLogger(`byteflood/client`)

var DefaultRequestTimeout = 10 * time.Second
var DefaultRequestScheme = `http`
var DefaultAddress = `127.0.0.1:11984`

type Client struct {
	Address string
	Scheme  string
	Timeout time.Duration
}

func NewClient() *Client {
	return &Client{
		Address: DefaultAddress,
		Scheme:  DefaultRequestScheme,
		Timeout: DefaultRequestTimeout,
	}
}

func (self *Client) Request(method string, path string, params map[string]string, headers map[string]string, body io.Reader) (*http.Response, error) {
	client := &http.Client{
		Timeout: self.Timeout,
	}

	log.Debugf("%+v", body)

	if request, err := http.NewRequest(
		strings.ToUpper(method),
		fmt.Sprintf("%s://%s%s", self.Scheme, self.Address, path),
		body,
	); err == nil {
		// add request headers
		for k, v := range headers {
			request.Header.Set(k, v)
		}

		// encode URL query string parameters
		qs := request.URL.Query()

		for k, v := range params {
			qs.Set(k, v)
		}

		request.URL.RawQuery = qs.Encode()

		log.Debugf("Request: %s %s", request.Method, request.URL.String())

		// perform request
		return client.Do(request)
	} else {
		return nil, err
	}
}

func (self *Client) getResponseError(response *http.Response) error {
	msg := `Unknown Error`

	if body, err := ioutil.ReadAll(response.Body); err == nil {
		msg = fmt.Sprintf("%v", string(body[:]))
	}

	return fmt.Errorf("%s: %v", response.Status, msg)
}

func ParseResponse(response *http.Response) (map[string]interface{}, error) {
	rv := make(map[string]interface{})

	if response.ContentLength == 0 || response.StatusCode == 204 {
		return nil, nil
	}

	body, _ := ioutil.ReadAll(response.Body)

	if err := json.NewDecoder(bytes.NewBuffer(body)).Decode(&rv); err == nil {
		return rv, nil
	} else {
		return map[string]interface{}{
			`body`:  strings.TrimSpace(string(body[:])),
			`error`: err.Error(),
		}, err
	}
}

func IsBadRequest(err error) bool {
	return strings.HasPrefix(err.Error(), `400 Bad Request`)
}

func IsForbidden(err error) bool {
	return strings.HasPrefix(err.Error(), `403 Forbidden`)
}

func IsNotFound(err error) bool {
	return strings.HasPrefix(err.Error(), `404 Not Found`)
}
