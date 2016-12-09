package peer

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"github.com/satori/go.uuid"
	"golang.org/x/crypto/nacl/box"
	"hash"
	"io"
	"net"
)

type RemotePeer struct {
	io.ReadWriteCloser
	Peer
	MessageSize         int
	publicKey           []byte
	id                  uuid.UUID
	sharedKey           [32]byte
	secureReady         bool
	connection          net.Conn
	messages            chan *Message
	isWritingDataStream bool
	readStreamHasher    hash.Hash
	writeStreamHasher   hash.Hash
}

func NewRemotePeerFromRequest(request *PeeringRequest, connection net.Conn) (*RemotePeer, error) {
	if err := request.Validate(); err == nil {
		if id, err := uuid.FromBytes(request.ID); err == nil {
			return &RemotePeer{
				MessageSize: request.MessageSize,
				publicKey:   request.PublicKey,
				id:          id,
				connection:  connection,
				messages:    make(chan *Message),
			}, nil
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}

func (self *RemotePeer) Start(localPeer *LocalPeer) {
	log.Debugf("[%s] Waiting for messages...", self.String())

	for {
		if message, err := self.ReceiveMessage(); err == nil {
			switch message.Type {
			case CommandType:
				log.Debugf("[%s] COMMAND: %s", self.String(), string(message.Data[:]))
			case MultipartStart:
				log.Debugf("[%s] MPSTART: Starting multipart receive...", self.String())
				self.readStreamHasher = sha256.New()

			case DataType:
				log.Debugf("[%s]    DATA: %d bytes", self.String(), len(message.Data))

				if self.readStreamHasher != nil {
					io.Copy(self.readStreamHasher, message)
				}

			case MultipartEnd:
				log.Debugf("[%s]   MPEND: Multipart receive complete: expected checksum: %X", self.String(), message.Data)
				log.Debugf("[%s]   MPEND:                                        actual: %X", self.String(), self.readStreamHasher.Sum(nil))

				self.readStreamHasher = nil
			default:
				log.Debugf("[%s] INVALID: %d bytes", self.String(), len(message.Data))
				continue
			}

			// self.messages <- message
		} else if err == io.EOF {
			log.Errorf("[%s] remote peer disconnected", self.String())
			defer localPeer.RemovePeer(self.id)
			return
		} else {
			log.Errorf("[%s] error receiving message: %v", self.String(), err)
		}
	}
}

func (self *RemotePeer) GetPublicKey() []byte {
	return self.publicKey
}

func (self *RemotePeer) String() string {
	return self.id.String()
}

func (self *RemotePeer) ID() []byte {
	return self.id.Bytes()
}

func (self *RemotePeer) UUID() uuid.UUID {
	return self.id
}

func (self *RemotePeer) Disconnect() error {
	self.secureReady = false
	return self.connection.Close()
}

func (self *RemotePeer) ReceiveMessage() (*Message, error) {
	if self.secureReady {
		payload := make([]byte, self.MessageSize+SecureMessageOverhead)

		if _, err := self.connection.Read(payload); err == nil {
			if secureMessage, err := DecodeSecureMessage(payload); err == nil {
				if decryptedPayload, ok := box.OpenAfterPrecomputation(
					nil,
					secureMessage.Payload,
					&secureMessage.Nonce,
					&self.sharedKey,
				); ok {
					if message, err := DecodeMessage(decryptedPayload); err == nil {
						return message, nil
					} else {
						return nil, err
					}
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
		return nil, fmt.Errorf("secure communication not established")
	}
}

func (self *RemotePeer) SendMessage(mType MessageType, p []byte) (int, error) {
	if self.secureReady {
		message := NewMessage(mType, p)

		if encodedMessage, err := message.Encode(); err == nil {
			var nonce [24]byte
			rand.Read(nonce[:])

			secureMessage := SecureMessage{
				Nonce: nonce,
				Payload: box.SealAfterPrecomputation(
					nil,
					encodedMessage,
					&nonce,
					&self.sharedKey,
				),
			}

			if securePayload, err := secureMessage.Encode(); err == nil {
				n, err := self.connection.Write(securePayload)
				log.Debugf("Wrote %d bytes (%d encrypted, %d encoded, %d raw) to peer", n, len(securePayload), len(encodedMessage), len(p))
				return len(p), err
			} else {
				return -1, err
			}
		} else {
			return -1, err
		}
	} else {
		return -1, fmt.Errorf("secure communication not established")
	}
}

func (self *RemotePeer) Read(p []byte) (int, error) {
	message := <-self.messages

	switch message.Type {
	case DataType:
		return copy(p, message.Data), nil
	default:
		return -1, fmt.Errorf("Reader interface only implemented for DataType messages")
	}
}

func (self *RemotePeer) Write(p []byte) (int, error) {
	if !self.isWritingDataStream {
		if _, err := self.SendMessage(MultipartStart, nil); err == nil {
			self.isWritingDataStream = true
			self.writeStreamHasher = sha256.New()
		} else {
			return -1, err
		}
	}

	n, err := self.SendMessage(DataType, p)

	// append data to running hash
	if self.writeStreamHasher != nil {
		io.Copy(self.writeStreamHasher, bytes.NewBuffer(p))
	}

	return n, err
}

func (self *RemotePeer) Close() error {
	sum := self.writeStreamHasher.Sum(nil)
	self.isWritingDataStream = false
	self.writeStreamHasher = nil

	log.Debugf("Sending multipart close: %X", sum)
	_, err := self.SendMessage(MultipartEnd, sum)

	if err != nil {
		log.Errorf("Failed to send multipart message termination")
	}

	return err
}
