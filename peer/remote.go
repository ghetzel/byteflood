package peer

import (
	"crypto/rand"
	"fmt"
	"github.com/satori/go.uuid"
	"golang.org/x/crypto/nacl/box"
	"io"
	"net"
)

type RemotePeer struct {
	io.ReadWriter
	Peer
	publicKey   []byte
	id          uuid.UUID
	sharedKey   [32]byte
	secureReady bool
	connection  net.Conn
	messages    chan *Message
}

func NewRemotePeerFromRequest(request *PeeringRequest, connection net.Conn) (*RemotePeer, error) {
	if err := request.Validate(); err == nil {
		if id, err := uuid.FromBytes(request.ID); err == nil {
			return &RemotePeer{
				publicKey:  request.PublicKey,
				id:         id,
				connection: connection,
				messages:   make(chan *Message),
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
			case DataType:
				log.Debugf("[%s]    DATA: %d bytes", self.String(), len(message.Data))
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
		payload := make([]byte, 16384)

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
		log.Debugf("[%s] Preparing message", self.String())

		message := NewMessage(mType, p)

		if data, err := message.Encode(); err == nil {
			var nonce [24]byte
			rand.Read(nonce[:])

			secureMessage := SecureMessage{
				Nonce: nonce,
				Payload: box.SealAfterPrecomputation(
					nil,
					data,
					&nonce,
					&self.sharedKey,
				),
			}

			if securePayload, err := secureMessage.Encode(); err == nil {
				log.Debugf("Writing %d bytes to peer", len(securePayload))
				return self.connection.Write(securePayload)
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

func (self *RemotePeer) Read(p []byte) (n int, err error) {
	message := <-self.messages

	switch message.Type {
	case DataType:
		return copy(p, message.Data), nil
	default:
		return -1, fmt.Errorf("Reader interface only implemented for DataType messages")
	}
}

func (self *RemotePeer) Write(p []byte) (n int, err error) {
	return self.SendMessage(DataType, p)
}
