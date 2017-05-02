package encryption

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"golang.org/x/crypto/nacl/box"
)

// Overhead:
// [0:4)  (4 bytes): 32-bit LE uint length of remaining message
// [4:28) (24 bytes) 192-bit nonce
// [28:)  cryptobox encryption overhead + arbitrary binary payload
//
var secureMessageOverhead int = (4 + 24 + box.Overhead)
var lengthPrefixByteSize = 4

type SecureMessage struct {
	Nonce   [24]byte
	Payload []byte
}

func DecodeLengthPrefix(data []byte) (int, error) {
	if len(data) >= lengthPrefixByteSize {
		return int(binary.LittleEndian.Uint32(data[0:lengthPrefixByteSize])), nil
	} else {
		return 0, fmt.Errorf("message too short")
	}
}

func DecodeSecureMessage(data []byte) (*SecureMessage, error) {
	message := new(SecureMessage)
	nonceLen := len(message.Nonce)

	if len(data) >= nonceLen {
		if copy(message.Nonce[:], data[0:nonceLen]) == nonceLen {
			// only process payload of the incoming data size is strictly larger
			// than just the nonce
			if len(data) > nonceLen {
				message.Payload = data[nonceLen:]
			}

			return message, nil
		} else {
			return nil, fmt.Errorf("secure message: malformed nonce")
		}
	} else {
		return nil, fmt.Errorf("secure message too short")
	}
}

func (self *SecureMessage) Encode() []byte {
	buffer := bytes.NewBuffer(nil)
	length := make([]byte, lengthPrefixByteSize)

	// first 4 bytes are uint32 length of the nonce+payload
	binary.LittleEndian.PutUint32(length, uint32(len(self.Nonce)+len(self.Payload)))
	buffer.Write(length)

	// next 24 bytes are the nonce
	buffer.Write([]byte(self.Nonce[:]))

	// remaining bytes are the payload
	buffer.Write(self.Payload)

	return buffer.Bytes()
}

func (self *SecureMessage) Len() int {
	return len(self.Nonce) + len(self.Payload)
}
