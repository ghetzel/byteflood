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
	MessageSize             int
	Encrypter               *encryption.Encrypter
	Decrypter               *encryption.Decrypter
	Messages                chan *Message
	publicKey               []byte
	id                      uuid.UUID
	connection              *net.TCPConn
	messages                chan *Message
	isWritingDataStream     bool
	dataStreamSizeBytes     int
	dataStreamReceivedBytes int
	readStreamHasher        hash.Hash
	writeStreamHasher       hash.Hash
	dataMessageCounter      int
	dataMessageBytes        int
	readLimiter             *util.RateLimitingReadWriter
	writeLimiter            *util.RateLimitingReadWriter
}

type PeerMessage struct {
	FromPeer *RemotePeer
	Message  *Message
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

				if v, ok := message.Value().(uint64); ok {
					self.dataStreamSizeBytes = int(v)
				} else {
					log.Errorf("Invalid checked transfer header: malformed size")
					continue
				}

			case DataBlock:
				log.Debugf("[%s]    DATA: %d bytes", self.String(), len(message.Data))

				if message.Data != nil {
					self.dataMessageCounter += 1
					self.dataMessageBytes += len(message.Data)

					// if this data block overruns what we're expecting, replace this message with a DataFailed
					// message.
					//
					// TODO: Tell the remote peer stop stop sending us data
					//
					if self.dataStreamSizeBytes > 0 && self.dataMessageBytes > self.dataStreamSizeBytes {
						msg := fmt.Sprintf("Checked transfer too long: writes have exceeded %d bytes", self.dataStreamSizeBytes)
						log.Errorf(msg)

						message, _ = NewMessageEncoded(
							DataFailed,
							msg,
							StringEncoding,
						)
					} else {
						if self.readStreamHasher != nil {
							self.readStreamHasher.Write(message.Data)
						}
					}

				}

			case DataFinalize:
				expected := message.Data
				actual := self.readStreamHasher.Sum(nil)

				log.Debugf("[%s]   FINAL: Checked transfer complete (saw %d data packets, %d bytes): ",
					self.String(),
					self.dataMessageCounter,
					self.dataMessageBytes)

				log.Debugf("[%s]   FINAL:   expected: %x", self.String(), expected)
				log.Debugf("[%s]   FINAL:     actual: %x", self.String(), actual)

				if self.dataMessageBytes == self.dataStreamSizeBytes {
					if bytes.Compare(expected, actual) == 0 {
						log.Debugf("[%s]   FINAL: Transfer successful (received %d bytes)", self.String(), self.dataStreamSizeBytes)
						// self.WriteMessage(self.connection, NewMessage(Acknowledgement, []byte{1}))
					} else {
						log.Errorf("[%s]   FINAL: checksum mismatch", self.String())

						// replace this message with a DataFailed message
						message, _ = NewMessageEncoded(
							DataFailed,
							fmt.Sprintf("checksum mismatch"),
							StringEncoding,
						)
						// self.WriteMessage(self.connection, NewMessage(Acknowledgement, []byte{0}))
					}
				} else {
					log.Errorf("[%s]   FINAL: size mismatch", self.String())

					// replace this message with a DataFailed message
					message, _ = NewMessageEncoded(
						DataFailed,
						fmt.Sprintf("size mismatch"),
						StringEncoding,
					)
				}

				self.dataStreamSizeBytes = 0
				self.dataMessageBytes = 0
				self.dataMessageCounter = 0
				self.readStreamHasher = nil

			case DataFailed:
				log.Debugf("[%s] DATFAIL: Transfer failed: %v", self.String(), message.Value())

				self.dataStreamSizeBytes = 0
				self.dataMessageBytes = 0
				self.dataMessageCounter = 0
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
				FromPeer: self,
				Message:  message,
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

func (self *RemotePeer) WriteMessage(w io.Writer, message *Message) (int, error) {
	// imposes rate limiting on writes (if configured)
	w = self.GetWriteRateLimiter(w)

	// ensures that the encrypter is using the writer we've specified
	self.Encrypter.SetTarget(w)

	// encode the message for transport
	if encodedMessage, err := message.Encode(); err == nil {
		encodedMessageR := bytes.NewReader(encodedMessage)

		// write encoded message (cleartext) to encrypter, which will in turn write
		// the ciphertext and protocol data to the writer specified above
		if n, err := io.Copy(self.Encrypter, encodedMessageR); err == nil {
			log.Debugf("[%s] Encrypted %d bytes (%d encoded, %d data)",
				message.Type.String(),
				n,
				len(encodedMessage),
				len(message.Data))
			return int(n), err
		} else {
			log.Debugf("[%s] write error: %v", message.Type.String(), err)
			return 0, err
		}
	} else {
		return 0, err
	}
}

func (self *RemotePeer) SendMessage(message *Message) (int, error) {
	return self.WriteMessage(self.connection, message)
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
func (self *RemotePeer) BeginChecked(size int) error {
	if !self.isWritingDataStream {
		if message, err := NewMessageEncoded(DataStart, size, BinaryLEUint64); err == nil {
			if _, err := self.WriteMessage(self.connection, message); err == nil {
				self.isWritingDataStream = true
				self.dataStreamSizeBytes = size
				self.dataStreamReceivedBytes = 0
				self.writeStreamHasher = sha256.New()
				return nil
			} else {
				return err
			}
		} else {
			return err
		}
	} else {
		return fmt.Errorf("A checked transfer is already in progress")
	}
}

func (self *RemotePeer) Write(p []byte) (int, error) {
	// append data to running hash if we're in the middle of a checked transfer
	if self.isWritingDataStream {
		// make sure we're not trying to write more than we should
		if (self.dataStreamReceivedBytes + len(p)) > self.dataStreamSizeBytes {
			err := fmt.Errorf(
				"write exceeds declared size (%d bytes)",
				self.dataStreamSizeBytes,
			)

			// send failure termination to peer and return error
			self.FinishChecked(err)
			return 0, err
		} else {
			// write to rolling checksum
			if _, err := io.Copy(self.writeStreamHasher, bytes.NewBuffer(p)); err != nil {
				return 0, fmt.Errorf("checksum error: %v", err)
			}

			// increment bytes written counter
			self.dataStreamReceivedBytes += len(p)
		}
	}

	// actually send the data block message to the peer
	n, err := self.WriteMessage(self.connection, NewMessage(DataBlock, p))

	if err == nil {
		// if the message was written without error and it wrote more data than was requested,
		// tell the caller that all of their data was written (but not more).  Callers are not
		// expecting io.Writer to have written more than it received.
		if n > len(p) {
			n = len(p)
		}
	} else {
		// writing the data block failed somehow, inform the peer and stop the transfer
		self.FinishChecked(err)
	}

	return n, err
}

func (self *RemotePeer) Close() error {
	log.Debugf("Got close")
	return nil
}

// Send a checked transfer termination message.  If an error is specified, the message type
// will indicate failure and include the error message.  If no error is given, the message
// type will indicate success and the message data will be the data checksum being summed
// during the write.
//
func (self *RemotePeer) FinishChecked(errs ...error) error {
	defer func() {
		self.isWritingDataStream = false
		self.writeStreamHasher = nil
	}()

	if self.isWritingDataStream {
		if len(errs) == 0 {
			sum := []byte{}

			if self.writeStreamHasher != nil {
				sum = self.writeStreamHasher.Sum(nil)
			}

			log.Debugf("Completed checked transfer: %x", sum)
			_, err := self.WriteMessage(self.connection, NewMessage(DataFinalize, sum))

			return err
		} else {
			if errMsg, err := NewMessageEncoded(DataFailed, errs[0].Error(), StringEncoding); err == nil {
				_, err := self.WriteMessage(
					self.connection,
					errMsg,
				)

				return err
			} else {
				return err
			}
		}
	} else {
		return nil
	}
}
