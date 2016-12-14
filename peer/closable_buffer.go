package peer

import (
	"bytes"
	"io"
)

type ClosableBuffer struct {
	io.ReadWriteCloser
	buffer *bytes.Buffer
}

func NewClosableBuffer(d []byte) *ClosableBuffer {
	return &ClosableBuffer{
		buffer: bytes.NewBuffer(d),
	}
}

func (self *ClosableBuffer) Read(p []byte) (int, error) {
	return self.buffer.Read(p)
}

func (self *ClosableBuffer) Write(p []byte) (int, error) {
	return self.buffer.Write(p)
}

func (self *ClosableBuffer) Close() error {
	return nil
}
