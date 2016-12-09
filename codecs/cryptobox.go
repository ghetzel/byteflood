package codecs

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"github.com/ghetzel/go-stockutil/pathutil"
	"github.com/vmihailenco/msgpack"
	"golang.org/x/crypto/nacl/box"
	"io"
	"io/ioutil"
	"os"
)

const (
	DEFAULT_MESSAGE_SIZE = 32768
)

// Poly1305 tag size + nonce size + shared key size + pointer + 3x field separator
var secureMessageOverhead int = box.Overhead + 24 + 32 + 8 + 3
var lengthPrefixByteSize = 4

func CryptoboxLoadKeyfiles(publicKeyPath string, privateKeyPath string) ([]byte, []byte, error) {
	var publicKey []byte
	var privateKey []byte

	if path, err := pathutil.ExpandUser(publicKeyPath); err == nil {
		if data, err := pemDecodeFileName(path); err == nil {
			log.Infof("Loaded public key at %s", path)
			publicKey = data
		} else {
			return nil, nil, err
		}
	} else {
		return nil, nil, err
	}

	if path, err := pathutil.ExpandUser(privateKeyPath); err == nil {
		if data, err := pemDecodeFileName(path); err == nil {
			log.Infof("Loaded private key at %s", path)
			privateKey = data
		} else {
			return nil, nil, err
		}
	} else {
		return nil, nil, err
	}

	return publicKey, privateKey, nil
}

func pemDecodeFileName(filename string) ([]byte, error) {
	if file, err := os.Open(filename); err == nil {
		if data, err := ioutil.ReadAll(file); err == nil {
			if block, _ := pem.Decode(data); block != nil {
				return block.Bytes, nil
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

func CryptoboxGenerateKeypair(publicKeyPath string, privateKeyPath string) error {
	if publicKeyFile, err := os.OpenFile(
		publicKeyPath,
		(os.O_WRONLY | os.O_CREATE | os.O_EXCL),
		0644,
	); err == nil {
		if privateKeyFile, err := os.OpenFile(
			privateKeyPath,
			(os.O_WRONLY | os.O_CREATE | os.O_EXCL),
			0600,
		); err == nil {
			var genError error

			// actually generate keys
			if publicKey, privateKey, err := box.GenerateKey(rand.Reader); err == nil {
				headers := map[string]string{
					`Cryptosystem`:   `NaCl cryptobox-compatible`,
					`Encryption`:     `XSalsa20 stream cipher`,
					`KeyExchange`:    `Diffie-Hellman ECDH (Curve25519)`,
					`Authentication`: `Poly1305 MAC`,
				}

				// encode and write public key
				if err := pem.Encode(publicKeyFile, &pem.Block{
					Type:    `NACL CRYPTOBOX PUBLIC KEY`,
					Headers: headers,
					Bytes:   []byte(publicKey[:]),
				}); err == nil {
					// encode and write private key
					if err := pem.Encode(privateKeyFile, &pem.Block{
						Type:    `NACL CRYPTOBOX PRIVATE KEY`,
						Headers: headers,
						Bytes:   []byte(privateKey[:]),
					}); err != nil {
						genError = err
					}
				} else {
					genError = err
				}
			} else {
				genError = err
			}

			// an error occurred during key generation, remove the files
			if genError != nil {
				defer os.Remove(privateKeyFile.Name())
				defer os.Remove(publicKeyFile.Name())
			}

			return genError
		} else {
			defer os.Remove(publicKeyFile.Name())
			return err
		}
	} else {
		return err
	}
}

type SecureMessage struct {
	Nonce   [24]byte
	Payload []byte
}

func decodeLengthPrefix(data []byte) (int, error) {
	if len(data) >= lengthPrefixByteSize {
		return int(binary.LittleEndian.Uint32(data[0:lengthPrefixByteSize])), nil
	} else {
		return 0, fmt.Errorf("message too short")
	}
}

func decodeSecureMessage(data []byte) (*SecureMessage, error) {
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
		length := make([]byte, lengthPrefixByteSize)

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

type CryptoboxCodec struct {
	Codec
	MessageSize int
	publicKey   []byte
	sharedKey   [32]byte
}

func NewCryptoboxCodec(publicKey []byte, privateKey []byte) *CryptoboxCodec {
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

	return &CryptoboxCodec{
		MessageSize: DEFAULT_MESSAGE_SIZE,
		publicKey:   publicKey,
		sharedKey:   sharedKey,
	}
}

func (self *CryptoboxCodec) Decode(reader io.Reader) ([]byte, error) {
	lengthPrefix := make([]byte, lengthPrefixByteSize)

	// read the length prefix
	if _, err := reader.Read(lengthPrefix); err == nil {
		// decode the length prefix into a number
		if length, err := decodeLengthPrefix(lengthPrefix); err == nil {
			if length > (self.MessageSize + secureMessageOverhead) {
				return nil, fmt.Errorf("specified length is too long")
			}

			payload := bytes.NewBuffer(nil)

			// read the next <length> bytes, which should be our secure message
			if n, err := io.CopyN(payload, reader, int64(length)); err == nil {
				if n != int64(length) {
					return nil, fmt.Errorf("short read (expected: %d, got: %d)", length, n)
				}

				// decode and decrypt the secure message
				if secureMessage, err := decodeSecureMessage(payload.Bytes()); err == nil {
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

func (self *CryptoboxCodec) Encode(w io.Writer, payload []byte) (int, error) {
	// General Message Structure
	//
	//  Length (4 bytes)
	//  SecureMessage[
	//    Nonce,
	//    cryptobox[
	//      payload
	//    ]
	//  ]
	// ]

	var nonce [24]byte
	rand.Read(nonce[:])

	secureMessage := SecureMessage{
		Nonce: nonce,
		Payload: box.SealAfterPrecomputation(
			nil,
			payload,
			&nonce,
			&self.sharedKey,
		),
	}

	if securePayload, err := secureMessage.Encode(); err == nil {
		return w.Write(securePayload)
	} else {
		return -1, err
	}
}
