package codecs

import (
	"github.com/op/go-logging"
	"io"
)

var log = logging.MustGetLogger(`byteflood.codecs`)

type Codec interface {
	Encode(io.Writer, []byte) (int, error)
	Decode(io.Reader) ([]byte, error)
}
