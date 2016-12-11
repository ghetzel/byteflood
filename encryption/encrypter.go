package encryption

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"golang.org/x/crypto/nacl/box"
	"io"
)

var DefaultEncryptBufferSize int = 32768

type Encrypter struct {
	publicKey []byte
	sharedKey [32]byte
	target    io.Writer
	buffer    *bytes.Buffer
}

func NewEncrypter(publicKey []byte, privateKey []byte, target io.Writer) *Encrypter {
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

	return &Encrypter{
		publicKey: publicKey,
		sharedKey: sharedKey,
		target:    target,
		buffer: bytes.NewBuffer(
			make([]byte, 0, DefaultEncryptBufferSize),
		),
	}
}

// Sets the writer to which ciphertext will be written to.
func (self *Encrypter) SetTarget(w io.Writer) {
	self.target = w
}

func (self *Encrypter) Write(p []byte) (int, error) {
	if self.target == nil {
		return 0, io.EOF
	}

	length := make([]byte, lengthPrefixByteSize)

	// first 4 bytes are uint32 length of the cleartext
	binary.LittleEndian.PutUint32(length, uint32(len(p)))

	// put length and payload into internal buffer
	if _, err := self.buffer.Write(length); err == nil {
		if n, err := self.buffer.Write(p); err == nil {
			return self.flushToTarget(n)
		} else {
			return 0, err
		}
	} else {
		return 0, err
	}
}

func (self *Encrypter) flushToTarget(originalLen int) (int, error) {
	dataToEncrypt := bytes.NewBuffer(nil)
	cleartextLengthB := make([]byte, lengthPrefixByteSize)

	// read how long the cleartext data packet is from the internal buffer
	if _, err := self.buffer.Read(cleartextLengthB); err == nil {
		cleartextLength := int64(binary.LittleEndian.Uint32(cleartextLengthB))

		// copy the next <length> bytes from the internal buffer into our encryption buffer
		if _, err := io.CopyN(dataToEncrypt, self.buffer, cleartextLength); err == nil {
			var nonce [24]byte
			rand.Read(nonce[:])

			// build our secure message
			secureMessage := SecureMessage{
				Nonce: nonce,
				Payload: box.SealAfterPrecomputation(
					nil,
					dataToEncrypt.Bytes(),
					&nonce,
					&self.sharedKey,
				),
			}

			// write the encoded secure message into the target writer
			if _, err := self.target.Write(secureMessage.Encode()); err == nil {
				// we return the length of the original write we were asked to perform here
				// because things don't expect us to have written *more* data than we were asked to,
				// even though that's exactly what we've done.
				return originalLen, err
			} else {
				return 0, err
			}
		} else {
			return 0, err
		}
	} else {
		return 0, err
	}
}
