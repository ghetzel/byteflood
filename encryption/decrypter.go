package encryption

import (
	"bytes"
	"fmt"
	"golang.org/x/crypto/nacl/box"
	"io"
)

type Decrypter struct {
	publicKey []byte
	sharedKey [32]byte
	source    io.Reader
}

func NewDecrypter(publicKey []byte, privateKey []byte, source io.Reader) *Decrypter {
	var sharedKey [32]byte
	var boundedPublicKey [32]byte
	var boundedPrivateKey [32]byte

	copy(boundedPublicKey[:], publicKey)
	copy(boundedPrivateKey[:], privateKey)

	box.Precompute(
		&sharedKey,
		&boundedPublicKey,
		&boundedPrivateKey,
	)

	return &Decrypter{
		publicKey: publicKey,
		sharedKey: sharedKey,
		source:    source,
	}
}

// Sets the reader from which ciphertext will be read from.
func (self *Decrypter) SetSource(r io.Reader) {
	self.source = r
}

func (self *Decrypter) Read(p []byte) (int, error) {
	if data, err := self.ReadPacket(); err == nil {
		return copy(p, data), nil
	} else {
		return 0, err
	}
}

func (self *Decrypter) ReadPacket() ([]byte, error) {
	if self.source == nil {
		return nil, io.EOF
	}

	lengthPrefix := make([]byte, lengthPrefixByteSize)

	// read the length prefix
	if _, err := self.source.Read(lengthPrefix); err == nil {
		// decode the length prefix into a number
		if length, err := decodeLengthPrefix(lengthPrefix); err == nil {
			dataToDecrypt := bytes.NewBuffer(nil)

			// read the next <length> bytes, which should be our secure message
			if n, err := io.CopyN(dataToDecrypt, self.source, int64(length)); err == nil {
				if n != int64(length) {
					return nil, fmt.Errorf("short read (expected: %d, got: %d)", length, n)
				}

				// decode and decrypt the secure message
				if secureMessage, err := decodeSecureMessage(dataToDecrypt.Bytes()); err == nil {
					if decryptedPayload, ok := box.OpenAfterPrecomputation(
						nil,
						secureMessage.Payload,
						&secureMessage.Nonce,
						&self.sharedKey,
					); ok {
						return decryptedPayload, nil
					} else {
						return nil, fmt.Errorf("failed to decrypt message")
					}
				} else {
					return nil, err
				}
			} else {
				return nil, err
			}
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}
