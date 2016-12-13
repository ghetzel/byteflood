package peer

import (
	"bytes"
	"fmt"
	"github.com/ghetzel/byteflood/encryption"
	"github.com/ghetzel/byteflood/util"
	"github.com/satori/go.uuid"
	"hash"
	"io"
	"net"
	"os"
	"time"
)

var RemotePeerDataFinalizeAckTimeout = 2000 * time.Millisecond
var RemotePeerMessageBufferSize int = 512

type RemotePeer struct {
	io.ReadWriteCloser
	Peer
	Encrypter                      *encryption.Encrypter
	Decrypter                      *encryption.Decrypter
	publicKey                      []byte
	id                             uuid.UUID
	connection                     *net.TCPConn
	outboundTransferActive         bool
	shouldTerminateCheckedTransfer bool
	outboundTransferTotalBytes     int
	outboundTransferBytesWritten   int
	outboundTransferChecksum       hash.Hash
	activeTransfer                 *Transfer
	isWaitingForNextMessage        bool
	readLimiter                    *util.RateLimitingReadWriter
	writeLimiter                   *util.RateLimitingReadWriter
	messages                       chan *Message
}

func NewRemotePeerFromRequest(request *PeeringRequest, connection *net.TCPConn) (*RemotePeer, error) {
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

func (self *RemotePeer) Disconnect() error {
	log.Infof("Disconnecting from %s", self.String())
	return self.connection.Close()
}

func (self *RemotePeer) ReceiveMessage() (*Message, error) {
	// imposes rate limiting on reads (if configured)
	reader := self.GetReadRateLimiter(self.connection)

	// ensures that the decrypter is using the reader we've specified
	self.Decrypter.SetSource(reader)

	if cleartext, err := self.Decrypter.ReadPacket(); err == nil {
		// decode the decrypted message payload
		if message, err := DecodeMessage(cleartext); err == nil {
			if self.isWaitingForNextMessage {
				self.isWaitingForNextMessage = false
				select {
				case self.messages <- message:
				default:
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

func (self *RemotePeer) WaitNextMessage() *Message {
	self.isWaitingForNextMessage = true
	return <-self.messages
}

func (self *RemotePeer) SendMessage(message *Message) (int, error) {
	// imposes rate limiting on writes (if configured)
	writer := self.GetWriteRateLimiter(self.connection)

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
//
func (self *RemotePeer) CreateTransfer(size int) (*OutboundTransfer, error) {
	transfer := NewOutboundTransfer(self, size)

	if err := transfer.Initialize(); err == nil {
		return transfer, nil
	} else {
		return nil, err
	}
}

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

func (self *RemotePeer) TransferData(send []byte) error {
	if transfer, err := self.CreateTransfer(len(send)); err == nil {
		// send the data
		if _, err := io.Copy(transfer, bytes.NewBuffer(send)); err == nil {
			return transfer.Close()
		} else {
			return err
		}
	} else {
		return err
	}
}

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

	for {
		if message, err := self.ReceiveMessage(); err == nil {
			var replyErr error
			log.Debugf("[%s] Message Type: %s", self.ID(), message.Type.String())

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
						// if the transfer was successful, inform the peer with an Acknowledgement
						_, replyErr = self.SendMessage(NewMessage(Acknowledgement, nil))
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
			}

			if replyErr != nil {
				log.Errorf("[%s] Send reply failed: %v", replyErr)
			}
		} else if err == io.EOF {
			log.Infof("Remote peer disconnected")
			return io.EOF
		}
	}

	return fmt.Errorf("Protocol Error")
}
