package encryption

import (
	"bytes"
	"crypto/rand"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/nacl/box"
)

func makeTestEncDecPair() (Encrypter, Decrypter) {
	if publicKey, privateKey, err := box.GenerateKey(rand.Reader); err == nil {
		encdec := NewCryptoboxEncryption([]byte(publicKey[:]), []byte(privateKey[:]), nil, nil)
		return encdec, encdec
	} else {
		panic(err)
	}
}

func encryptionIdentityTest(t *testing.T, value []byte) {
	assert := require.New(t)
	encrypter, decrypter := makeTestEncDecPair()
	buffer := bytes.NewBuffer(nil)

	encrypter.SetTarget(buffer)
	decrypter.SetSource(buffer)

	// write cleartext
	n, err := io.Copy(encrypter, bytes.NewBuffer([]byte(value[:])))
	assert.Nil(err)
	assert.Equal(int64(len(value)), n)
	assert.Equal(buffer.Len(), int(n)+secureMessageOverhead)

	decrypted := make([]byte, n)

	m, err := decrypter.Read(decrypted)
	assert.Nil(err)
	assert.Equal(len(value), m)
	assert.Equal(value, decrypted[:m])
}

func TestSingleString(t *testing.T) {
	value := `This is a test`
	encryptionIdentityTest(t, []byte(value[:]))
}

func TestLongDataRun(t *testing.T) {
	value := make([]byte, 65536)
	rand.Read(value)

	encryptionIdentityTest(t, value)
}

func TestLongerDataRun(t *testing.T) {
	value := make([]byte, 5242880)
	rand.Read(value)

	encryptionIdentityTest(t, value)
}
