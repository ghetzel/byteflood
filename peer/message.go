package peer

import (
	"bytes"
	"encoding/binary"
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

type SecureMessage struct {
	Nonce   [24]byte
	Payload []byte
}

func DecodeLengthPrefix(data []byte) (int, error) {
	if len(data) >= 4 {
		return int(binary.LittleEndian.Uint32(data[0:4])), nil
	} else {
		return 0, fmt.Errorf("message too short")
	}
}

func DecodeSecureMessage(data []byte) (*SecureMessage, error) {
	message := SecureMessage{}

	if err := msgpack.Unmarshal(data, &message); err == nil {
		return &message, nil
	} else {
		return nil, fmt.Errorf("failed to decode secure message: %v", err)
	}
}

func (self *SecureMessage) Encode() ([]byte, error) {
	if data, err := msgpack.Marshal(self); err == nil {
		buffer := bytes.NewBuffer(nil)
		length := make([]byte, 4)

		// first 4 bytes are uint32 length of the following payload
		binary.LittleEndian.PutUint32(length, uint32(len(data)))

		// write payload
		buffer.Write(length)

		if _, err := io.Copy(buffer, bytes.NewBuffer(data)); err == nil {
			return buffer.Bytes(), nil
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}

func (self *SecureMessage) Len() int {
	return len(self.Nonce) + len(self.Payload)
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
