package encryption

import (
	"bytes"
	"crypto/rand"
	"io"
	"testing"
)

var lastErr error

func benchmarkEncDecValue(b *testing.B, value []byte, encrypting bool) {
	buffer := bytes.NewBuffer(nil)
	encrypter, decrypter := makeTestEncDecPair()

	encrypter.SetTarget(buffer)
	encrypter.Write(value)
	reader := bytes.NewReader(buffer.Bytes())

	if !encrypting {
		decrypter.SetSource(reader)
	}

	for n := 0; n < b.N; n++ {
		var err error
		if encrypting {
			_, err = encrypter.Write(value)
		} else {
			dest := make([]byte, len(value))
			_, err = decrypter.Read(dest)
			reader.Seek(0, io.SeekStart)
		}

		if err != nil {
			panic(err)
		}

		lastErr = err
	}
}

func BenchmarkEncrypt_16B(b *testing.B) {
	value := `This is a test!!`
	benchmarkEncDecValue(b, []byte(value[:]), true)
}

func BenchmarkEncrypt_32K(b *testing.B) {
	value := make([]byte, 32768)
	rand.Read(value)
	benchmarkEncDecValue(b, value, true)
}

func BenchmarkEncrypt_1MB(b *testing.B) {
	value := make([]byte, (1024 * 1024 * 1))
	rand.Read(value)
	benchmarkEncDecValue(b, value, true)
}

func BenchmarkDecrypt_16B(b *testing.B) {
	value := `This is a test!!`
	benchmarkEncDecValue(b, []byte(value[:]), false)
}

func BenchmarkDecrypt_32K(b *testing.B) {
	value := make([]byte, 32768)
	rand.Read(value)
	benchmarkEncDecValue(b, value, false)
}

func BenchmarkDecrypt_1MB(b *testing.B) {
	value := make([]byte, (1024 * 1024 * 1))
	rand.Read(value)
	benchmarkEncDecValue(b, value, false)
}
