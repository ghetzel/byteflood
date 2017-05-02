package encryption

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/nacl/box"
)

type CryptoboxEncryption struct {
	EncrypterDecrypter
	publicKey []byte
	sharedKey [32]byte
	source    io.Reader
	target    io.Writer
	writeLock sync.Mutex
}

func NewCryptoboxEncryption(publicKey []byte, privateKey []byte, source io.Reader, target io.Writer) *CryptoboxEncryption {
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

	return &CryptoboxEncryption{
		publicKey: publicKey,
		sharedKey: sharedKey,
		source:    source,
		target:    target,
	}
}

// Encrypter
// ------------------------------------------------------------------------------------------------

// Sets the writer to which ciphertext will be written to.
func (self *CryptoboxEncryption) SetTarget(w io.Writer) {
	self.target = w
}

// Locks the encrypter for exclusive writing.
func (self *CryptoboxEncryption) Lock() {
	self.writeLock.Lock()
}

// Releases a previously-acquired lock.
func (self *CryptoboxEncryption) Unlock() {
	self.writeLock.Unlock()
}

func (self *CryptoboxEncryption) Write(p []byte) (int, error) {
	if self.target == nil {
		return 0, io.EOF
	}

	// this must be unique EVERY TIME!
	// otherwise, breaking the encryption is trivial
	var nonce [24]byte
	rand.Read(nonce[:])

	// build our secure message
	secureMessage := SecureMessage{
		Nonce: nonce,
		Payload: box.SealAfterPrecomputation(
			nil,
			p,
			&nonce,
			&self.sharedKey,
		),
	}

	// write the encoded secure message into the target writer
	if _, err := self.target.Write(secureMessage.Encode()); err == nil {
		// we return the length of the original write we were asked to perform here
		// because things don't expect us to have written *more* data than we were asked to,
		// even though that's exactly what we've done.
		return len(p), err
	} else {
		return 0, err
	}
}

// Decrypter
// ------------------------------------------------------------------------------------------------

// Sets the reader from which ciphertext will be read from.
func (self *CryptoboxEncryption) SetSource(r io.Reader) {
	self.source = r
}

func (self *CryptoboxEncryption) Read(p []byte) (int, error) {
	if data, err := self.ReadNext(); err == nil {
		return copy(p, data), nil
	} else {
		return 0, err
	}
}

// read a secure message from the source Reader and return it
func (self *CryptoboxEncryption) ReadNextMessage() (*SecureMessage, error) {
	if self.source == nil {
		return nil, io.EOF
	}

	lengthPrefix := make([]byte, lengthPrefixByteSize)

	// read the length prefix
	if _, err := self.source.Read(lengthPrefix); err == nil {
		// decode the length prefix into a number
		if length, err := DecodeLengthPrefix(lengthPrefix); err == nil {
			dataToDecrypt := bytes.NewBuffer(nil)

			// read the next <length> bytes, which should be our secure message
			if n, err := io.CopyN(dataToDecrypt, self.source, int64(length)); err == nil {
				if n != int64(length) {
					return nil, fmt.Errorf("short read (expected: %d, got: %d)", length, n)
				}

				// decode and decrypt the secure message
				if secureMessage, err := DecodeSecureMessage(dataToDecrypt.Bytes()); err == nil {
					return secureMessage, nil
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

// read a secure message from the source Reader and return the decrypted contents
func (self *CryptoboxEncryption) ReadNext() ([]byte, error) {
	if secureMessage, err := self.ReadNextMessage(); err == nil {
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
}
