package encryption

import (
	"io"
)

type Encrypter interface {
	SetTarget(w io.Writer)
	Lock()
	Unlock()
	Write(p []byte) (int, error)
}

type Decrypter interface {
	SetSource(r io.Reader)
	Read(p []byte) (int, error)
	ReadPacket() ([]byte, error)
}

type EncrypterDecrypter interface {
	Encrypter
	Decrypter
}
