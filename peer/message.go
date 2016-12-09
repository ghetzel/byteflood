package peer

import (
	"fmt"
	"github.com/vmihailenco/msgpack"
	"io"
)

type MessageType int

const (
	CommandType MessageType = iota
	DataType
	MultipartStart
	MultipartEnd
	Acknowledgement
)

func (self MessageType) String() string {
	switch self {
	case CommandType:
		return `command`
	case DataType:
		return `data`
	case MultipartStart:
		return `mp-start`
	case MultipartEnd:
		return `mp-end`
	case Acknowledgement:
		return `ack`
	default:
		return `invalid`
	}
}

type Message struct {
	io.Reader
	Type MessageType
	Data []byte
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
