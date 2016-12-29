package client

import (
    "net/http"
    "fmt"
    "time"
    "strings"
    "io"
    "io/ioutil"
    "bytes"
    "encoding/json"
    "github.com/op/go-logging"
)

var log = logging.MustGetLogger(`byteflood/client`)

var DefaultRequestTimeout = 10 * time.Second
var DefaultRequestScheme  = `http`
var DefaultAddress = `127.0.0.1:11984`

type Client struct {
    Address string
    Scheme string
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
    }else{
        return nil, err
    }
}


func ParseResponse(response *http.Response) (map[string]interface{}, error) {
    rv := make(map[string]interface{})

    if response.ContentLength == 0 || response.StatusCode == 204 {
        return nil, nil
    }

    body, _ := ioutil.ReadAll(response.Body)

    if err := json.NewDecoder(bytes.NewBuffer(body)).Decode(&rv); err == nil {
        return rv, nil
    }else{
        return map[string]interface{}{
            `body`: strings.TrimSpace(string(body[:])),
            `error`: err.Error(),
        }, err
    }
}