package encryption

import (
	"bytes"
	"crypto/rand"
	"io"
	"io/ioutil"
	"testing"
)

func benchmarkEncDecValue(b *testing.B, value []byte, encrypting bool, parallel bool) {
	encrypter, decrypter := makeTestEncDecPair()
	encryptedTestData := bytes.NewBuffer(nil)
	var reader *bytes.Reader

	if encrypting {
		encrypter.SetTarget(ioutil.Discard)
	} else {
		// populate encrypted data once
		encrypter.SetTarget(encryptedTestData)
		encrypter.Write(value)

		// make the decrypter aware of the encrypted data
		reader = bytes.NewReader(encryptedTestData.Bytes())
		decrypter.SetSource(reader)
	}

	fn := func() {
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
	}

	if parallel {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				fn()
			}
		})
	} else {
		for n := 0; n < b.N; n++ {
			fn()
		}
	}
}

func BenchmarkEncrypt_16B(b *testing.B) {
	value := `This is a test!!`
	benchmarkEncDecValue(b, []byte(value[:]), true, false)
}

func BenchmarkEncrypt_32K(b *testing.B) {
	value := make([]byte, 32768)
	rand.Read(value)
	benchmarkEncDecValue(b, value, true, false)
}

func BenchmarkEncrypt_1MB(b *testing.B) {
	value := make([]byte, (1024 * 1024 * 1))
	rand.Read(value)
	benchmarkEncDecValue(b, value, true, false)
}

func BenchmarkDecrypt_16B(b *testing.B) {
	value := `This is a test!!`
	benchmarkEncDecValue(b, []byte(value[:]), false, false)
}

func BenchmarkDecrypt_32K(b *testing.B) {
	value := make([]byte, 32768)
	rand.Read(value)
	benchmarkEncDecValue(b, value, false, false)
}

func BenchmarkDecrypt_1MB(b *testing.B) {
	value := make([]byte, (1024 * 1024 * 1))
	rand.Read(value)
	benchmarkEncDecValue(b, value, false, false)
}

func BenchmarkEncryptParallel_16B(b *testing.B) {
	value := `This is a test!!`
	benchmarkEncDecValue(b, []byte(value[:]), true, true)
}

func BenchmarkEncryptParallel_32K(b *testing.B) {
	value := make([]byte, 32768)
	rand.Read(value)
	benchmarkEncDecValue(b, value, true, true)
}

func BenchmarkEncryptParallel_1MB(b *testing.B) {
	value := make([]byte, (1024 * 1024 * 1))
	rand.Read(value)
	benchmarkEncDecValue(b, value, true, true)
}
