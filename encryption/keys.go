package encryption

import (
	"crypto/rand"
	"encoding/pem"
	"github.com/ghetzel/go-stockutil/pathutil"
	"github.com/op/go-logging"
	"golang.org/x/crypto/nacl/box"
	"io/ioutil"
	"os"
)

var log = logging.MustGetLogger(`byteflood.codecs`)

func LoadKeyfiles(publicKeyPath string, privateKeyPath string) ([]byte, []byte, error) {
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

func GenerateKeypair(publicKeyPath string, privateKeyPath string) error {
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
