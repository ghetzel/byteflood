package util

import (
	"fmt"
	"io"
	"time"

	"golang.org/x/time/rate"
)

type RateLimitingReadWriter struct {
	io.ReadWriter
	reader  io.Reader
	writer  io.Writer
	limiter *rate.Limiter
}

func NewRateLimitingReadWriter(eventsPerSecond float64, burst int) *RateLimitingReadWriter {
	rll := RateLimitingReadWriter{}
	rll.SetLimit(eventsPerSecond, burst)

	return &rll
}

func (self *RateLimitingReadWriter) SetReader(r io.Reader) {
	self.reader = r
}

func (self *RateLimitingReadWriter) SetWriter(w io.Writer) {
	self.writer = w
}

func (self *RateLimitingReadWriter) SetLimit(eventsPerSecond float64, burst int) {
	if self.limiter == nil {
		if eventsPerSecond >= 0 {
			self.limiter = rate.NewLimiter(
				rate.Limit(eventsPerSecond),
				burst,
			)
		}
	} else {
		if eventsPerSecond >= 0 {
			self.limiter.SetLimit(rate.Limit(eventsPerSecond))
		} else {
			self.limiter = nil
		}
	}
}

func (self *RateLimitingReadWriter) Read(p []byte) (int, error) {
	return self.readWrite(true, p)
}

func (self *RateLimitingReadWriter) Write(p []byte) (int, error) {
	return self.readWrite(false, p)
}

func (self *RateLimitingReadWriter) readWrite(reading bool, p []byte) (int, error) {
	var n int
	var opErr error

	if reading {
		if self.reader == nil {
			return -1, fmt.Errorf("reader not set on rate limiter")
		}

		n, opErr = self.reader.Read(p)
	} else {
		if self.writer == nil {
			return -1, fmt.Errorf("writer not set on rate limiter")
		}

		n, opErr = self.writer.Write(p)
	}

	if self.limiter == nil || opErr != nil {
		return n, opErr
	} else {
		now := time.Now()
		rv := self.limiter.ReserveN(now, n)

		if rv.OK() {
			delay := rv.DelayFrom(now)
			time.Sleep(delay)
			return n, opErr
		} else {
			return 0, fmt.Errorf(
				"rate limit (%02f bytes/sec, burstable to %d bytes)",
				self.limiter.Limit(),
				self.limiter.Burst(),
			)
		}
	}
}
