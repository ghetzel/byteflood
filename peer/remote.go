package peer

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"github.com/ghetzel/byteflood/codecs"
	"github.com/ghetzel/byteflood/util"
	"github.com/satori/go.uuid"
	"hash"
	"io"
	"net"
	"time"
)

var RemotePeerDataFinalizeAckTimeout = 2000 * time.Millisecond

type RemotePeer struct {
	io.ReadWriteCloser
	Peer
	MessageSize         int
	Encrypter           codecs.Codec
	Messages            chan *Message
	publicKey           []byte
	id                  uuid.UUID
	connection          *net.TCPConn
	messages            chan *Message
	isWritingDataStream bool
	readStreamHasher    hash.Hash
	writeStreamHasher   hash.Hash
	dataMessageCounter  int
	dataMessageBytes    int
	readLimiter         *util.RateLimitingReadWriter
	writeLimiter        *util.RateLimitingReadWriter
}

type PeerMessage struct {
	Peer    *RemotePeer
	Message *Message
}

func NewRemotePeerFromRequest(request *PeeringRequest, connection *net.TCPConn) (*RemotePeer, error) {
	if err := request.Validate(); err == nil {
		if id, err := uuid.FromBytes(request.ID); err == nil {
			return &RemotePeer{
				MessageSize: request.MessageSize,
				publicKey:   request.PublicKey,
				id:          id,
				connection:  connection,
				messages:    make(chan *Message),
				Messages:    make(chan *Message),
			}, nil
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}

func (self *RemotePeer) SetReadRateLimit(bytesPerSecond int, burstSize int) {
	self.readLimiter = util.NewRateLimitingReadWriter(float64(bytesPerSecond), burstSize)
}

func (self *RemotePeer) GetReadRateLimiter(r io.Reader) io.Reader {
	if self.readLimiter != nil {
		self.readLimiter.SetReader(r)
		return self.readLimiter
	} else {
		return r
	}
}

func (self *RemotePeer) SetWriteRateLimit(bytesPerSecond int, burstSize int) {
	self.writeLimiter = util.NewRateLimitingReadWriter(float64(bytesPerSecond), burstSize)
}

func (self *RemotePeer) GetWriteRateLimiter(w io.Writer) io.Writer {
	if self.writeLimiter != nil {
		self.writeLimiter.SetWriter(w)
		return self.writeLimiter
	} else {
		return w
	}
}

// Starts the per-peer services a LocalPeer needs to provide to interact with a RemotePeer.
func (self *RemotePeer) Start(localPeer *LocalPeer) {
	log.Debugf("[%s] Waiting for messages...", self.String())

	for {
		if message, err := self.ReadMessage(self.connection); err == nil {
			switch message.Type {
			case DataStart:
				log.Debugf("[%s]   START: Starting multipart receive...", self.String())
				self.readStreamHasher = sha256.New()
				self.dataMessageCounter = 0
				self.dataMessageBytes = 0

			case DataBlock:
				log.Debugf("[%s]    DATA: %d bytes", self.String(), len(message.Data))

				if message.Data != nil {
					self.dataMessageCounter += 1
					self.dataMessageBytes += len(message.Data)

					if self.readStreamHasher != nil {
						self.readStreamHasher.Write(message.Data)
					}
				}

			case DataFinalize:
				expected := message.Data
				actual := self.readStreamHasher.Sum(nil)

				log.Debugf("[%s]   FINAL: Multipart receive complete (saw %d data packets, %d bytes): ",
					self.String(),
					self.dataMessageCounter,
					self.dataMessageBytes)

				log.Debugf("[%s]   FINAL:   expected: %x", self.String(), expected)
				log.Debugf("[%s]   FINAL:     actual: %x", self.String(), actual)

				// if bytes.Compare(expected, actual) == 0 {
				// 	log.Debugf("[%s]   FINAL: Transfer successful", self.String())
				// 	self.WriteMessage(self.connection, Acknowledgement, []byte{1})
				// } else {
				// 	log.Errorf("[%s]   FINAL: Transfer failed", self.String())
				// 	self.WriteMessage(self.connection, Acknowledgement, []byte{0})
				// }

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
				Peer:    self,
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
	// imposes rate limiting on reads (if configured)
	reader = self.GetReadRateLimiter(reader)

	if decryptedPayload, err := self.Encrypter.Decode(reader); err == nil {
		// decode the decrypted message payload
		if message, err := DecodeMessage(decryptedPayload); err == nil {
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
	// imposes rate limiting on writes (if configured)
	w = self.GetWriteRateLimiter(w)

	message := NewMessage(mType, p)

	if encodedMessage, err := message.Encode(); err == nil {
		if n, err := self.Encrypter.Encode(w, encodedMessage); err == nil {
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
			return 0, err
		}
	} else {
		return 0, err
	}
}

func (self *RemotePeer) SendMessage(mType MessageType, p []byte) (int, error) {
	return self.WriteMessage(self.connection, mType, p)
}

func (self *RemotePeer) Read(p []byte) (int, error) {
	message := <-self.messages

	switch message.Type {
	case DataBlock:
		return copy(p, message.Data), nil
	case DataFinalize:
		return 0, io.EOF
	default:
		return 0, fmt.Errorf("Reader interface only implemented for DataBlock messages")
	}
}

func (self *RemotePeer) Write(p []byte) (int, error) {
	if !self.isWritingDataStream {
		if _, err := self.WriteMessage(self.connection, DataStart, nil); err == nil {
			self.isWritingDataStream = true
			self.writeStreamHasher = sha256.New()
		} else {
			return 0, err
		}
	}

	n, err := self.WriteMessage(self.connection, DataBlock, p)

	// append data to running hash
	if self.writeStreamHasher != nil {
		io.Copy(self.writeStreamHasher, bytes.NewBuffer(p))
	}
	return n, err
}

func (self *RemotePeer) Close() error {
	log.Debugf("Got close")
	return nil
}

func (self *RemotePeer) Finalize() error {
	if self.isWritingDataStream {
		sum := []byte{}

		if self.writeStreamHasher != nil {
			sum = self.writeStreamHasher.Sum(nil)
		}

		self.isWritingDataStream = false
		self.writeStreamHasher = nil

		log.Debugf("Sending multipart close: %x", sum)
		_, err := self.WriteMessage(self.connection, DataFinalize, sum)

		if err != nil {
			log.Errorf("Failed to send multipart message termination")
		}

		// wait for acknowledgement that the message termination was received
		// select {
		// case ack := <-self.messages:
		// 	if ack.Type != Acknowledgement {
		// 		return fmt.Errorf("Invalid acknowledgement message type %d", ack.Type)
		// 	}

		// 	if ack.Data != nil && ack.Data[0] == 1 {
		// 		log.Debugf("Remote peer acknowledges successful transfer")
		// 	} else {
		// 		log.Warningf("Remote peer acknowledges unsuccessful transfer")
		// 	}
		// case <-time.After(RemotePeerDataFinalizeAckTimeout):
		// 	return fmt.Errorf("Timed out waiting for multipart acknowledgement, waited %s", RemotePeerDataFinalizeAckTimeout)
		// }

		return err
	} else {
		return nil
	}
}
