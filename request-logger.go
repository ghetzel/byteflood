package byteflood

import (
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
	for _, method := range self.Methods {
		method = strings.ToUpper(method)

		if strings.HasPrefix(method, `-`) {
			method = strings.TrimPrefix(method, `-`)

			if req.Method == method {
				return
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

	if status < 400 {
		log.Debugf("[HTTP %d] %s to %v took %v", status, req.Method, req.URL, duration)
	} else {
		log.Debugf("[HTTP %d] %s to %v took %v", status, req.Method, req.URL, duration)
	}
}
