package peer

import (
	"encoding/binary"
	"fmt"
	"github.com/vmihailenco/msgpack"
	"io"
	"reflect"
)

type MessageType int

const (
	Acknowledgment MessageType = iota
	DataStart
	DataProceed
	DataBlock
	DataTerminate
	DataFinalize
	DataFailed
	ServiceRequest
	ServiceResponse
)

type MessageEncoding int

const (
	RawEncoding MessageEncoding = iota
	StringEncoding
	MsgpackEncoding
	BinaryLEUint16
	BinaryLEUint32
	BinaryLEUint64
	BinaryBEUint16
	BinaryBEUint32
	BinaryBEUint64
)

func (self MessageType) String() string {
	switch self {
	case DataStart:
		return `transfer-start`
	case DataProceed:
		return `transfer-proceed`
	case DataBlock:
		return `data-block`
	case DataTerminate:
		return `transfer-terminate`
	case DataFinalize:
		return `transfer-final`
	case DataFailed:
		return `transfer-failed`
	case Acknowledgment:
		return `acknowledgement`
	case ServiceRequest:
		return `service-request`
	case ServiceResponse:
		return `service-response`
	default:
		return `invalid`
	}
}

type Message struct {
	io.Reader
	Type           MessageType
	Encoding       MessageEncoding
	Data           []byte
	actualSize     int
	actualChecksum []byte
}

func NewMessage(mt MessageType, data []byte) *Message {
	return &Message{
		Type: mt,
		Data: data,
	}
}

func NewMessageEncoded(mt MessageType, value interface{}, encoding MessageEncoding) (*Message, error) {
	message := &Message{
		Type:     mt,
		Encoding: encoding,
	}

	if err := message.EncodeData(value); err == nil {
		return message, nil
	} else {
		return nil, err
	}
}

func (self *Message) EncodeData(value interface{}) error {
	if value == nil {
		self.Data = nil
	}

	switch self.Encoding {
	case StringEncoding:
		v := fmt.Sprintf("%v", value)
		self.Data = []byte(v[:])
		return nil

	case MsgpackEncoding:
		data, err := msgpack.Marshal(self.Data)
		self.Data = data
		return err

	case BinaryLEUint16:
		toType := reflect.TypeOf(uint16(0))

		if vT := reflect.TypeOf(value); vT.ConvertibleTo(toType) {
			self.Data = make([]byte, int(toType.Size()))
			binary.LittleEndian.PutUint16(
				self.Data,
				uint16(reflect.ValueOf(value).Convert(toType).Uint()),
			)
		} else {
			return fmt.Errorf("Cannot convert %T to uint16", value)
		}

	case BinaryLEUint32:
		toType := reflect.TypeOf(uint32(0))

		if vT := reflect.TypeOf(value); vT.ConvertibleTo(toType) {
			self.Data = make([]byte, int(toType.Size()))
			binary.LittleEndian.PutUint32(
				self.Data,
				uint32(reflect.ValueOf(value).Convert(toType).Uint()),
			)
		} else {
			return fmt.Errorf("Cannot convert %T to uint32", value)
		}

	case BinaryLEUint64:
		toType := reflect.TypeOf(uint64(0))

		if vT := reflect.TypeOf(value); vT.ConvertibleTo(toType) {
			self.Data = make([]byte, int(toType.Size()))
			binary.LittleEndian.PutUint64(
				self.Data,
				uint64(reflect.ValueOf(value).Convert(toType).Uint()),
			)
		} else {
			return fmt.Errorf("Cannot convert %T to uint64", value)
		}

	case BinaryBEUint16:
		toType := reflect.TypeOf(uint16(0))

		if vT := reflect.TypeOf(value); vT.ConvertibleTo(toType) {
			self.Data = make([]byte, int(toType.Size()))
			binary.BigEndian.PutUint16(
				self.Data,
				uint16(reflect.ValueOf(value).Convert(toType).Uint()),
			)
		} else {
			return fmt.Errorf("Cannot convert %T to uint16", value)
		}

	case BinaryBEUint32:
		toType := reflect.TypeOf(uint32(0))

		if vT := reflect.TypeOf(value); vT.ConvertibleTo(toType) {
			self.Data = make([]byte, int(toType.Size()))
			binary.BigEndian.PutUint32(
				self.Data,
				uint32(reflect.ValueOf(value).Convert(toType).Uint()),
			)
		} else {
			return fmt.Errorf("Cannot convert %T to uint32", value)
		}

	case BinaryBEUint64:
		toType := reflect.TypeOf(uint64(0))

		if vT := reflect.TypeOf(value); vT.ConvertibleTo(toType) {
			self.Data = make([]byte, int(toType.Size()))
			binary.BigEndian.PutUint64(
				self.Data,
				uint64(reflect.ValueOf(value).Convert(toType).Uint()),
			)
		} else {
			return fmt.Errorf("Cannot convert %T to uint64", value)
		}

	default:
		if vValue := reflect.ValueOf(value); vValue.Kind() == reflect.Slice {
			if vValue.Type().Elem().Kind() == reflect.Uint8 {
				self.Data = vValue.Bytes()
				return nil
			}
		}

		return fmt.Errorf("Cannot convert %T to byte slice", value)
	}

	return nil
}

func (self *Message) DecodeData(data []byte) (interface{}, error) {
	var rv interface{}

	switch self.Encoding {
	case StringEncoding:
		return string(data[:]), nil

	case MsgpackEncoding:
		err := msgpack.Unmarshal(data, &rv)
		return rv, err

	case BinaryLEUint16:
		return binary.LittleEndian.Uint16(data), nil
	case BinaryLEUint32:
		return binary.LittleEndian.Uint32(data), nil
	case BinaryLEUint64:
		return binary.LittleEndian.Uint64(data), nil
	case BinaryBEUint16:
		return binary.BigEndian.Uint16(data), nil
	case BinaryBEUint32:
		return binary.BigEndian.Uint32(data), nil
	case BinaryBEUint64:
		return binary.BigEndian.Uint64(data), nil
	default:
		return data, nil
	}
}

func (self *Message) Value() interface{} {
	if value, err := self.DecodeData(self.Data); err == nil {
		return value
	} else {
		return nil
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
