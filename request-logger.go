package byteflood

import (
	"github.com/ghetzel/byteflood/stats"
	"github.com/urfave/negroni"
	"net/http"
	"strings"
	"time"
)

type RequestLogger struct {
	Methods []string
}

func NewRequestLogger() *RequestLogger {
	return &RequestLogger{}
}

func (self *RequestLogger) ServeHTTP(rw http.ResponseWriter, req *http.Request, next http.HandlerFunc) {
	emitLog := true

	for _, method := range self.Methods {
		method = strings.ToUpper(method)

		if strings.HasPrefix(method, `-`) {
			method = strings.TrimPrefix(method, `-`)

			if req.Method == method {
				emitLog = false
				break
			}
		} else if req.Method == method {
			break
		}
	}

	start := time.Now()

	next(rw, req)

	response := rw.(negroni.ResponseWriter)
	status := response.Status()
	duration := time.Since(start)
	isError := false

	if status < 400 {
		if emitLog {
			log.Debugf("[HTTP %d] %s to %v took %v", status, req.Method, req.URL, duration)
		}
	} else {
		isError = true

		if emitLog {
			log.Debugf("[HTTP %d] %s to %v took %v", status, req.Method, req.URL, duration)
		}
	}

	tags := map[string]interface{}{
		`method`: req.Method,
		`status`: status,
		`error`:  isError,
	}

	stats.Elapsed(`byteflood.api.request_time`, duration, tags)
	stats.Increment(`byteflood.api.requests`, tags)
}
