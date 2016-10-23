package util

import (
	"github.com/op/go-logging"
	"io"
	"strings"
)

type LogProxy struct {
	io.Writer
	logger       *logging.Logger
	defaultLevel string
}

func NewLogProxy(loggerName string, defaultLevel string) *LogProxy {
	return &LogProxy{
		logger:       logging.MustGetLogger(loggerName),
		defaultLevel: defaultLevel,
	}
}

func (self *LogProxy) Write(p []byte) (n int, err error) {
	if self.logger == nil {
		self.logger = logging.MustGetLogger(`logproxy`)
	}

	message := string(p[:])

	switch strings.ToLower(self.defaultLevel) {
	case `debug`:
		self.logger.Debug(message)
	case `info`:
		self.logger.Info(message)
	case `notice`:
		self.logger.Notice(message)
	case `warning`:
		self.logger.Warning(message)
	case `error`:
		self.logger.Error(message)
	case `critical`:
		self.logger.Critical(message)
	case `fatal`:
		self.logger.Fatal(message)
	case `panic`:
		self.logger.Panic(message)
	default:
		self.logger.Info(message)
	}

	return len(p), nil
}
