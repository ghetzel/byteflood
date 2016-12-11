package peer

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"github.com/ghetzel/byteflood/encryption"
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
	Encrypter           *encryption.Encrypter
	Decrypter           *encryption.Decrypter
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
				//  log.Debugf("[%s]   FINAL: Transfer successful", self.String())
				//  self.WriteMessage(self.connection, Acknowledgement, []byte{1})
				// } else {
				//  log.Errorf("[%s]   FINAL: Transfer failed", self.String())
				//  self.WriteMessage(self.connection, Acknowledgement, []byte{0})
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

	// ensures that the decrypter is using the reader we've specified
	self.Decrypter.SetSource(reader)

	if cleartext, err := self.Decrypter.ReadPacket(); err == nil {
		// decode the decrypted message payload
		if message, err := DecodeMessage(cleartext); err == nil {
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

	// ensures that the encrypter is using the writer we've specified
	self.Encrypter.SetTarget(w)

	message := NewMessage(mType, p)

	// encode the message for transport
	if encodedMessage, err := message.Encode(); err == nil {
		encodedMessageR := bytes.NewReader(encodedMessage)

		// write encoded message (cleartext) to encrypter, which will in turn write
		// the ciphertext and protocol data to the writer specified above
		if n, err := io.Copy(self.Encrypter, encodedMessageR); err == nil {
			log.Debugf("[%s] Encrypted %d bytes (%d encoded, %d data)",
				mType.String(),
				n,
				len(encodedMessage),
				len(message.Data))
			return int(n), err
		} else {
			log.Debugf("[%s] write error: %v", mType.String(), err)
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

// Starts a checked transfer.  All subsequent writes (until FinishChecked is called) will be hashed
// and sent as one logical transaction.
func (self *RemotePeer) BeginChecked() error {
	if !self.isWritingDataStream {
		if _, err := self.WriteMessage(self.connection, DataStart, nil); err == nil {
			self.isWritingDataStream = true
			self.writeStreamHasher = sha256.New()
			return nil
		} else {
			return err
		}
	} else {
		return fmt.Errorf("A checked transfer is already in progress")
	}
}

func (self *RemotePeer) Write(p []byte) (int, error) {
	n, err := self.WriteMessage(self.connection, DataBlock, p)

	// if the message was written without error and it wrote more data than was requested,
	// tell the caller that all of their data was written (but not more).  Callers are not
	// expecting io.Writer to have written more than it received.
	if err == nil && n > len(p) {
		n = len(p)
	}

	// append data to running hash if we're in the middle of a checked transfer
	if self.writeStreamHasher != nil {
		io.Copy(self.writeStreamHasher, bytes.NewBuffer(p))
	}

	return n, err
}

func (self *RemotePeer) Close() error {
	log.Debugf("Got close")
	return nil
}

func (self *RemotePeer) FinishChecked() error {
	if self.isWritingDataStream {
		sum := []byte{}

		if self.writeStreamHasher != nil {
			sum = self.writeStreamHasher.Sum(nil)
		}

		self.isWritingDataStream = false
		self.writeStreamHasher = nil

		log.Debugf("Completed checked transfer: %x", sum)
		_, err := self.WriteMessage(self.connection, DataFinalize, sum)

		return err
	} else {
		return nil
	}
}
