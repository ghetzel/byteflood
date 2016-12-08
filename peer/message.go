package peer

import (
	"fmt"
	"github.com/vmihailenco/msgpack"
)

type MessageType int

const (
	CommandType MessageType = iota
	DataType
)

type Message struct {
	Type MessageType
	Data []byte
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

func DecodeMessage(data []byte) (*Message, error) {
	message := Message{}

	if err := msgpack.Unmarshal(data, &message); err == nil {
		return &message, nil
	} else {
		return nil, fmt.Errorf("failed to decode message: %v", err)
	}
}
