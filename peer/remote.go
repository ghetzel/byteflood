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
	"time"
)

var RemotePeerConnectionLingerSeconds = 3
var RemotePeerMultipartEndAckTimeout = 2000 * time.Millisecond

type RemotePeer struct {
	io.ReadWriteCloser
	Peer
	MessageSize         int
	publicKey           []byte
	id                  uuid.UUID
	sharedKey           [32]byte
	secureReady         bool
	connection          *net.TCPConn
	messages            chan *Message
	isWritingDataStream bool
	readStreamHasher    hash.Hash
	writeStreamHasher   hash.Hash
	dataMessageCounter  int
	dataMessageBytes    int
}

func NewRemotePeerFromRequest(request *PeeringRequest, connection *net.TCPConn) (*RemotePeer, error) {
	if err := request.Validate(); err == nil {
		if id, err := uuid.FromBytes(request.ID); err == nil {
			if err := connection.SetLinger(RemotePeerConnectionLingerSeconds); err != nil {
				return nil, err
			}

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
		if message, err := self.ReceiveMessage(self.connection); err == nil {
			switch message.Type {
			case CommandType:
				log.Debugf("[%s] COMMAND: %s", self.String(), string(message.Data[:]))
			case MultipartStart:
				log.Debugf("[%s] MPSTART: Starting multipart receive...", self.String())
				self.readStreamHasher = sha256.New()
				self.dataMessageCounter = 0
				self.dataMessageBytes = 0

			case DataType:
				log.Debugf("[%s]    DATA: %d bytes", self.String(), len(message.Data))

				if message.Data != nil {
					self.dataMessageCounter += 1
					self.dataMessageBytes += len(message.Data)

					if self.readStreamHasher != nil {
						self.readStreamHasher.Write(message.Data)
					}
				}

			case MultipartEnd:
				expected := message.Data
				actual := self.readStreamHasher.Sum(nil)

				log.Debugf("[%s]   MPEND: Multipart receive complete (saw %d data packets, %d bytes): ",
					self.String(),
					self.dataMessageCounter,
					self.dataMessageBytes)

				log.Debugf("[%s]   MPEND:   expected: %x", self.String(), expected)
				log.Debugf("[%s]   MPEND:     actual: %x", self.String(), actual)

				if bytes.Compare(expected, actual) == 0 {
					log.Debugf("[%s]   MPEND: Transfer successful", self.String())
					self.SendMessage(self.connection, Acknowledgement, []byte{1})
				} else {
					log.Errorf("[%s]   MPEND: Transfer failed", self.String())
					self.SendMessage(self.connection, Acknowledgement, []byte{0})
				}

				self.readStreamHasher = nil

			case Acknowledgement:
				log.Debugf("[%s]     ACK: Acknowledge %x", self.String(), message.Data)

			default:
				log.Debugf("[%s] INVALID: Unknown message type, data: %d bytes", self.String(), len(message.Data))
				continue
			}

			select {
			case self.messages <- message:
				log.Debugf("Message dispatched")
			default:
				continue
			}
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

	log.Infof("Disconnecting from %s", self.String())
	return self.connection.Close()
}

func (self *RemotePeer) ReceiveMessage(reader io.Reader) (*Message, error) {
	if self.secureReady {
		lengthPrefix := make([]byte, 4)

		if n, err := reader.Read(lengthPrefix); err == nil {
			log.Debugf("Read %d bytes (prefix)", n)

			if length, err := DecodeLengthPrefix(lengthPrefix); err == nil {
				if length > (self.MessageSize + SecureMessageOverhead) {
					return nil, fmt.Errorf("specified length is too long")
				}

				payload := make([]byte, length)
				log.Debugf("Reading %d bytes", length)

				if n, err := reader.Read(payload); err == nil {
					if n != length {
						return nil, fmt.Errorf("short read")
					}

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
				return nil, err
			}
		} else {
			return nil, err
		}

	} else {
		return nil, fmt.Errorf("secure communication not established")
	}
}

func (self *RemotePeer) SendMessage(w io.Writer, mType MessageType, p []byte) (int, error) {
	if self.secureReady {
		// cryptobox[ SecureMessage[ Nonce, Message[ Type, Data ]]]

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
				n, err := w.Write(securePayload)

				if err == nil {
					log.Debugf("[%s] Wrote %d bytes (%d encrypted, %d encoded, %d data)",
						mType.String(),
						n,
						len(securePayload),
						len(encodedMessage),
						len(p))
				} else {
					log.Debugf("[%s] write error: %v", mType.String(), err)
				}

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
		if _, err := self.SendMessage(self.connection, MultipartStart, nil); err == nil {
			self.isWritingDataStream = true
			self.writeStreamHasher = sha256.New()
		} else {
			return -1, err
		}
	}

	n, err := self.SendMessage(self.connection, DataType, p)

	// append data to running hash
	if self.writeStreamHasher != nil {
		io.Copy(self.writeStreamHasher, bytes.NewBuffer(p))
	}

	return n, err
}

func (self *RemotePeer) Close() error {
	if self.isWritingDataStream {
		sum := []byte{}

		if self.writeStreamHasher != nil {
			sum = self.writeStreamHasher.Sum(nil)
		}

		self.isWritingDataStream = false
		self.writeStreamHasher = nil

		log.Debugf("Sending multipart close: %x", sum)
		_, err := self.SendMessage(self.connection, MultipartEnd, sum)

		if err != nil {
			log.Errorf("Failed to send multipart message termination")
		}

		// wait for acknowledgement that the message termination was received
		select {
		case ack := <-self.messages:
			if ack.Type != Acknowledgement {
				return fmt.Errorf("Invalid acknowledgement message type %d", ack.Type)
			}

			if ack.Data != nil && ack.Data[0] == 1 {
				log.Debugf("Remote peer acknowledges successful transfer")
			} else {
				log.Warningf("Remote peer acknowledges unsuccessful transfer")
			}
		case <-time.After(RemotePeerMultipartEndAckTimeout):
			return fmt.Errorf("Timed out waiting for multipart acknowledgement, waited %s", RemotePeerMultipartEndAckTimeout)
		}

		return err
	} else {
		return nil
	}
}
