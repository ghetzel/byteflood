package encryption

import (
	"crypto/rand"
	"golang.org/x/crypto/nacl/box"
	"io"
	"sync"
)

type Encrypter struct {
	publicKey []byte
	sharedKey [32]byte
	target    io.Writer
	writeLock sync.Mutex
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
	}
}

// Sets the writer to which ciphertext will be written to.
func (self *Encrypter) SetTarget(w io.Writer) {
	self.target = w
}

// Locks the encrypter for exclusive writing.
func (self *Encrypter) Lock() {
	self.writeLock.Lock()
}

// Releases a previously-acquired lock.
func (self *Encrypter) Unlock() {
	self.writeLock.Unlock()
}

func (self *Encrypter) Write(p []byte) (int, error) {
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
