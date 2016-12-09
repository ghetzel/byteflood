package peer

import (
	"bytes"
	"fmt"
	"github.com/vmihailenco/msgpack"
	"golang.org/x/crypto/nacl/box"
	"io"
)

// Poly1305 tag size + nonce size + shared key size + pointer + 3x field separator
var SecureMessageOverhead int = box.Overhead + 24 + 32 + 8 + 3

type MessageType int

const (
	CommandType MessageType = iota
	DataType
	MultipartStart
	MultipartEnd
)

type Message struct {
	io.Reader
	Type   MessageType
	Data   []byte
	buffer *bytes.Buffer
}

type SecureMessage struct {
	Nonce   [24]byte
	Payload []byte
}

func DecodeSecureMessage(data []byte) (*SecureMessage, error) {
	message := SecureMessage{}

	if err := msgpack.Unmarshal(data, &message); err == nil {
		return &message, nil
	} else {
		return nil, fmt.Errorf("failed to decode secure message wrapper: %v", err)
	}
}

func (self *SecureMessage) Encode() ([]byte, error) {
	return msgpack.Marshal(self)
}

func NewMessage(mt MessageType, data []byte) *Message {
	return &Message{
		Type: mt,
		Data: data,
	}
}

func (self *Message) Encode() ([]byte, error) {
	return msgpack.Marshal(self)
}

func (self *Message) Read(p []byte) (int, error) {
	if self.buffer == nil {
		self.buffer = bytes.NewBuffer(self.Data)
	}

	return self.buffer.Read(p)
}

func DecodeMessage(data []byte) (*Message, error) {
	message := Message{}

	if err := msgpack.Unmarshal(data, &message); err == nil {
		return &message, nil
	} else {
		return nil, fmt.Errorf("failed to decode message: %v", err)
	}
}
