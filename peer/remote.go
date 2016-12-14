package peer

import (
	"bytes"
	"fmt"
	"github.com/ghetzel/byteflood/encryption"
	"github.com/ghetzel/byteflood/util"
	"github.com/multiformats/go-multistream"
	"github.com/satori/go.uuid"
	"io"
	"net"
	"os"
	"time"
)

var DataFinalizeAckTimeout = 2000 * time.Millisecond
var MessageBufferSize = 512
var BadMessageThreshold = 10
var ServiceRequestBufferSize = 1048576
var ServiceResponseBufferSize = 1048576
var ServiceMessageDispatchTimeout = 500 * time.Millisecond
var ServiceResponseTimeout = 5 * time.Second

type RemotePeer struct {
	io.ReadWriteCloser
	Peer
	Encrypter               *encryption.Encrypter
	Decrypter               *encryption.Decrypter
	activeTransfer          *Transfer
	connection              *net.TCPConn
	id                      uuid.UUID
	isWaitingForNextMessage bool
	nextMessageType         int
	messages                chan *Message
	publicKey               []byte
	readLimiter             *util.RateLimitingReadWriter
	writeLimiter            *util.RateLimitingReadWriter
	serviceBuffer           io.ReadWriteCloser
}

func NewRemotePeerFromRequest(request *PeeringRequest, connection *net.TCPConn) (*RemotePeer, error) {
	if err := request.Validate(); err == nil {
		if id, err := uuid.FromBytes(request.ID); err == nil {
			return &RemotePeer{
				publicKey:     request.PublicKey,
				id:            id,
				connection:    connection,
				messages:      make(chan *Message),
				serviceBuffer: NewClosableBuffer(nil),
			}, nil
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}

func (self *RemotePeer) GetPublicKey() []byte {
	return self.publicKey
}

func (self *RemotePeer) ID() string {
	return self.id.String()
}

func (self *RemotePeer) String() string {
	return self.ID()
}

func (self *RemotePeer) UUID() uuid.UUID {
	return self.id
}

// Specify a limit (in bytes per second) to impose on data downloaded from this peer.  Up to burstSize
// bytes can be read in a single call (which can exceed the bytesPerSecond rate for that call).
//
func (self *RemotePeer) SetReadRateLimit(bytesPerSecond int, burstSize int) {
	self.readLimiter = util.NewRateLimitingReadWriter(float64(bytesPerSecond), burstSize)
}

// Specify a limit (in bytes per second) to impose on data uploaded to this peer.  Up to burstSize
// bytes can be written in a single call (which can exceed the bytesPerSecond rate for that call).
//
func (self *RemotePeer) SetWriteRateLimit(bytesPerSecond int, burstSize int) {
	self.writeLimiter = util.NewRateLimitingReadWriter(float64(bytesPerSecond), burstSize)
}

func (self *RemotePeer) getReadRateLimiter(r io.Reader) io.Reader {
	if self.readLimiter != nil {
		self.readLimiter.SetReader(r)
		return self.readLimiter
	} else {
		return r
	}
}

func (self *RemotePeer) getWriteRateLimiter(w io.Writer) io.Writer {
	if self.writeLimiter != nil {
		self.writeLimiter.SetWriter(w)
		return self.writeLimiter
	} else {
		return w
	}
}

// Writes that occur directly on RemotePeer are sent along as Service messages
//
func (self *RemotePeer) Write(p []byte) (int, error) {
	if _, err := self.SendMessage(NewMessage(Service, p)); err == nil {
		return len(p), nil
	} else {
		return 0, err
	}
}

// Disconnect from this peer and close the connection.
//
func (self *RemotePeer) Disconnect() error {
	log.Infof("Disconnecting from %s", self.String())
	return self.connection.Close()
}

// Receive the next message from this peer.
//
func (self *RemotePeer) ReceiveMessage() (*Message, error) {
	// imposes rate limiting on reads (if configured)
	reader := self.getReadRateLimiter(self.connection)

	// ensures that the decrypter is using the reader we've specified
	self.Decrypter.SetSource(reader)

	if cleartext, err := self.Decrypter.ReadPacket(); err == nil {
		// decode the decrypted message payload
		if message, err := DecodeMessage(cleartext); err == nil {
			if self.isWaitingForNextMessage {
				if self.nextMessageType < 0 || message.Type == MessageType(self.nextMessageType) {
					self.messages <- message
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

// Blocks until the next message is received and returns that message.  If timeout > 0, an
// error will be returned if no message is received within that duration.
//
func (self *RemotePeer) WaitNextMessage(timeout time.Duration) (*Message, error) {
	self.isWaitingForNextMessage = true

	defer func() {
		self.isWaitingForNextMessage = false
		self.nextMessageType = -1
	}()

	if timeout != 0 {
		select {
		case message := <-self.messages:
			return message, nil
		case <-time.After(timeout):
			return nil, fmt.Errorf("Timed out waiting for next message")
		}
	} else {
		return <-self.messages, nil
	}
}

func (self *RemotePeer) WaitNextMessageByType(timeout time.Duration, messageType MessageType) (*Message, error) {
	self.nextMessageType = int(messageType)
	return self.WaitNextMessage(timeout)
}

// Sends a message packet to this peer.
//
func (self *RemotePeer) SendMessage(message *Message) (int, error) {
	// imposes rate limiting on writes (if configured)
	writer := self.getWriteRateLimiter(self.connection)

	// ensures that the encrypter is using the writer we've specified
	self.Encrypter.SetTarget(writer)

	// encode the message for transport
	if encodedMessage, err := message.Encode(); err == nil {
		encodedMessageR := bytes.NewReader(encodedMessage)

		// write encoded message (cleartext) to encrypter, which will in turn write
		// the ciphertext and protocol data to the writer specified above
		if n, err := io.Copy(self.Encrypter, encodedMessageR); err == nil {
			log.Debugf("[%s] WRITE: [%s] Encrypted %d bytes (%d encoded, %d data)",
				self.String(),
				message.Type.String(),
				n,
				len(encodedMessage),
				len(message.Data))
			return int(n), err
		} else {
			log.Debugf("[%s] WRITE: [%s] error: %v", message.Type.String(), err)
			return 0, err
		}
	} else {
		return 0, err
	}
}

// Starts an outbound transfer. If a non-zero size is specified, the receiving peer will
// verify that the received data is exactly that length.  If size is zero, this verification will not
// be performed. It is recommended that if the size is known beforehand that it should be sent.
//
func (self *RemotePeer) CreateTransfer(size int) (*OutboundTransfer, error) {
	transfer := NewOutboundTransfer(self, size)

	if err := transfer.Initialize(); err == nil {
		return transfer, nil
	} else {
		return nil, err
	}
}

// Transfers the contents of the given io.Reader to the peer in a streaming fashion.
//
func (self *RemotePeer) TransferStream(stream io.Reader) error {
	if transfer, err := self.CreateTransfer(0); err == nil {
		if _, err := io.Copy(transfer, stream); err == nil {
			return transfer.Close()
		} else {
			return err
		}
	} else {
		return err
	}
}

// Transfers the given byte slice to the peer.
//
func (self *RemotePeer) TransferData(data []byte) error {
	if transfer, err := self.CreateTransfer(len(data)); err == nil {
		// send the data
		if _, err := io.Copy(transfer, bytes.NewBuffer(data)); err == nil {
			return transfer.Close()
		} else {
			return err
		}
	} else {
		return err
	}
}

// Transfers the given filename to the peer.
//
func (self *RemotePeer) TransferFile(path string) error {
	if file, err := os.Open(path); err == nil {
		if stat, err := file.Stat(); err == nil {
			if transfer, err := self.CreateTransfer(int(stat.Size())); err == nil {
				if _, err := io.Copy(transfer, file); err == nil {
					return transfer.Close()
				} else {
					return err
				}
			} else {
				return err
			}
		} else {
			return err
		}
	} else {
		return err
	}
}

// Receives messages from the given peer on an ongoing basis.  Returns an error
// representing a disconnect or other connection error.  This function never returns
// nil and is intended to be long-running on a per-peer basis.
//
func (self *RemotePeer) ReceiveMessages(localPeer *LocalPeer) error {
	log.Debugf("Receiving messages from %s", self.ID())
	var errorCount int

	// any condition that causes this function to return should also cause a disconnect
	defer self.Disconnect()

	go self.startServiceMultiplexer()

	for {
		if message, err := self.ReceiveMessage(); err == nil {
			errorCount = 0

			var replyErr error

			log.Debugf("[%s] Received Message Type: %s", self.ID(), message.Type.String())

			switch message.Type {
			case DataStart:
				v, ok := message.Value().(uint64)

				if !ok {
					v = 0
				}

				// create a new transfer
				self.activeTransfer = NewTransfer(self, int(v))

				// TODO: local can reject by sending DataTerminate w/ an optional string reason
				//       based on a to-be-determined file receive policy

				// give peer the go-ahead to start writing data
				_, err = self.SendMessage(NewMessage(DataProceed, nil))

			case DataProceed:
				// this is explicitly waited for by RemotePeer, nothing to do here

			case DataBlock:
				if self.activeTransfer != nil {
					_, replyErr = self.activeTransfer.Write(message.Data)
				}

				if replyErr != nil {
					if m, err := NewMessageEncoded(DataTerminate, err.Error(), StringEncoding); err == nil {
						_, replyErr = self.SendMessage(m)
					} else {
						replyErr = err
					}
				}

			case DataFinalize:
				if self.activeTransfer != nil {
					if err := self.activeTransfer.Verify(message.Data); err == nil {
						// if the transfer was successful, inform the peer with an Acknowledgment
						_, replyErr = self.SendMessage(NewMessage(Acknowledgment, nil))
					} else {
						log.Errorf("[%s] Transfer failed verification: %v", self.String(), err)

						// if the transfer failed verification, reply with a failure message
						if m, err := NewMessageEncoded(DataFailed, err.Error(), StringEncoding); err == nil {
							_, replyErr = self.SendMessage(m)
						} else {
							replyErr = err
						}
					}

					self.activeTransfer = nil
				}

			case Service:
				_, replyErr = self.serviceBuffer.Write(message.Data)
			}

			if replyErr != nil {
				log.Errorf("[%s] Send reply failed: %v", self.ID(), replyErr)
			}
		} else if err == io.EOF {
			log.Infof("Remote peer disconnected")
			return io.EOF
		} else {
			log.Errorf("error receiving message: %v", err)
			errorCount += 1

			if errorCount < BadMessageThreshold {
				continue
			} else {
				return err
			}
		}
	}

	return fmt.Errorf("Protocol Error")
}

func (self *RemotePeer) startServiceMultiplexer() {
	mux := multistream.NewMultistreamMuxer()

	mux.AddHandler(`/api`, func(proto string, rwc io.ReadWriteCloser) error {
		log.Debugf("%s: Got api request", proto)
		return rwc.Close()
	})

	mux.Handle(self.serviceBuffer)
}
