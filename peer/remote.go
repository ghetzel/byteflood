package peer

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"github.com/ghetzel/byteflood/codecs"
	"github.com/satori/go.uuid"
	"hash"
	"io"
	"net"
	"time"
)

var RemotePeerMultipartEndAckTimeout = 2000 * time.Millisecond

type RemotePeer struct {
	io.ReadWriteCloser
	Peer
	MessageSize         int
	EncryptionCodec     codecs.Codec
	Messages                   chan *Message
    messageCodecs       []codecs.Codec
    publicKey           []byte
    id                  uuid.UUID
    connection          *net.TCPConn
    messages            chan *Message
	isWritingDataStream bool
	readStreamHasher    hash.Hash
	writeStreamHasher   hash.Hash
	dataMessageCounter  int
	dataMessageBytes    int
}

type PeerMessage struct {
    Peer *RemotePeer
    Message *Message
}

func NewRemotePeerFromRequest(request *PeeringRequest, connection *net.TCPConn) (*RemotePeer, error) {
	if err := request.Validate(); err == nil {
		if id, err := uuid.FromBytes(request.ID); err == nil {
			return &RemotePeer{
				MessageSize:   request.MessageSize,
				publicKey:     request.PublicKey,
				messageCodecs: make([]codecs.Codec, 0),
				id:            id,
				connection:    connection,
				messages:      make(chan *Message),
				Messages:             make(chan *Message),
			}, nil
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}

func (self *RemotePeer) AddMessageCodec(codec codecs.Codec) {
	self.messageCodecs = append(self.messageCodecs, codec)
}

func (self *RemotePeer) Start(localPeer *LocalPeer) {
	log.Debugf("[%s] Waiting for messages...", self.String())

	for {
		if message, err := self.ReadMessage(self.connection); err == nil {
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
					self.WriteMessage(self.connection, Acknowledgement, []byte{1})
				} else {
					log.Errorf("[%s]   MPEND: Transfer failed", self.String())
					self.WriteMessage(self.connection, Acknowledgement, []byte{0})
				}

				self.readStreamHasher = nil

			case Acknowledgement:
				log.Debugf("[%s]     ACK: Acknowledge %x", self.String(), message.Data)

			default:
				log.Debugf("[%s] INVALID: Unknown message type, data: %d bytes", self.String(), len(message.Data))
				continue
			}

			// send message to internal message chan (non-blocking)
			select {
			case self.messages <- message:
				break
			default:
				break
			}

			// ALSO send message to external message chan (non-blocking)
			select {
			case localPeer.Messages <- PeerMessage{
                Peer: self,
                Message: message,
            }:
				break
			default:
				break
			}
		} else {
			if err == io.EOF {
				log.Errorf("[%s] remote peer disconnected", self.String())
			} else {
				log.Errorf("[%s] error receiving message: %v", self.String(), err)
			}

			defer localPeer.RemovePeer(self.id)
			return
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
	log.Infof("Disconnecting from %s", self.String())
	return self.connection.Close()
}

func (self *RemotePeer) ReadMessage(reader io.Reader) (*Message, error) {
	if decryptedPayload, err := self.EncryptionCodec.Decode(reader); err == nil {
		// decode the decrypted message payload
		if message, err := DecodeMessage(decryptedPayload); err == nil {
			if message.Data != nil && len(self.messageCodecs) > 0 {
				buffer := bytes.NewBuffer(message.Data)

				// decoding iterates through codecs in reverse order
				for i := range self.messageCodecs {
					codec := self.messageCodecs[len(self.messageCodecs)-1-i]

					if post, err := codec.Decode(buffer); err == nil {
						message.Data = post
						buffer = bytes.NewBuffer(message.Data)
					} else {
						return nil, err
					}
				}
			}

			return message, nil
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}

func (self *RemotePeer) ReceiveMessage() (*Message, error) {
	return self.ReadMessage(self.connection)
}

func (self *RemotePeer) WriteMessage(w io.Writer, mType MessageType, p []byte) (int, error) {
	var message *Message

	if p != nil && len(self.messageCodecs) > 0 {
		preProcessed := bytes.NewBuffer(p)
		postProcessed := bytes.NewBuffer(nil)

		// encoding iterates through codecs in forward order
		for _, codec := range self.messageCodecs {
			if _, err := codec.Encode(postProcessed, preProcessed.Bytes()); err == nil {
				preProcessed = bytes.NewBuffer(postProcessed.Bytes())
			} else {
				return -1, err
			}
		}

		message = NewMessage(mType, postProcessed.Bytes())
	} else {
		message = NewMessage(mType, p)
	}

	if encodedMessage, err := message.Encode(); err == nil {
		if n, err := self.EncryptionCodec.Encode(w, encodedMessage); err == nil {
			if err == nil {
				log.Debugf("[%s] Wrote %d bytes (%d encoded, %d data)",
					mType.String(),
					n,
					len(encodedMessage),
					len(message.Data))
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
}

func (self *RemotePeer) SendMessage(mType MessageType, p []byte) (int, error) {
	return self.WriteMessage(self.connection, mType, p)
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
		if _, err := self.WriteMessage(self.connection, MultipartStart, nil); err == nil {
			self.isWritingDataStream = true
			self.writeStreamHasher = sha256.New()
		} else {
			return -1, err
		}
	}

	n, err := self.WriteMessage(self.connection, DataType, p)

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
		_, err := self.WriteMessage(self.connection, MultipartEnd, sum)

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
