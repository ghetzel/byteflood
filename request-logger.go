package byteflood

import (
	"fmt"
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

	stats.Elapsed(
		fmt.Sprintf(
			"byteflood.api.request_time,method=%s,status=%d,error=%v",
			req.Method,
			status,
			isError,
		),
		duration,
	)

	stats.Increment(
		fmt.Sprintf(
			"byteflood.api.requests,method=%s,status=%d,error=%v",
			req.Method,
			status,
			isError,
		),
	)
}
