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
	readLimiter                    *util.RateLimitingReadWriter
	writeLimiter                   *util.RateLimitingReadWriter
}

func NewRemotePeerFromRequest(request *PeeringRequest, connection *net.TCPConn) (*RemotePeer, error) {
	if err := request.Validate(); err == nil {
		if id, err := uuid.FromBytes(request.ID); err == nil {
			return &RemotePeer{
				publicKey:  request.PublicKey,
				id:         id,
				connection: connection,
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
			return message, nil
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
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

// Starts a checked transfer.  All subsequent writes (until FinishChecked is called) will be hashed
// and sent as one logical transaction.  If a non-zero size is specified, the receiving peer will
// verify that the received data is exactly that length.  If size is zero, this verification will not
// be performed. It is recommended that if the size is known beforehand that it should be sent.
//
func (self *RemotePeer) BeginChecked(size int) error {
	// only we we aren't already performing a transfer...
	if !self.outboundTransferActive {
		// encode and send the transfer start header
		if message, err := NewMessageEncoded(DataStart, size, BinaryLEUint64); err == nil {
			if _, err := self.SendMessage(message); err == nil {
				// transfers are answered with a go/no-go reply
				if message, err := self.ReceiveMessage(); err == nil {
					switch message.Type {
					case DataProceed:
						self.outboundTransferActive = true
						self.outboundTransferTotalBytes = size
						self.outboundTransferBytesWritten = 0
						self.outboundTransferChecksum = sha256.New()
						return nil

					case DataTerminate:
						return fmt.Errorf("Remote peer refused transfer: %v", message.Value())

					default:
						return fmt.Errorf("Remote peer sent an invalid reply: %s", message.Type.String())

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
	} else {
		return fmt.Errorf("A checked transfer is already in progress")
	}
}

func (self *RemotePeer) Write(p []byte) (int, error) {
	// append data to running hash if we're in the middle of a checked transfer
	if self.outboundTransferActive {
		// check if we should be terminating an in-progress transfer
		if self.shouldTerminateCheckedTransfer {
			err := fmt.Errorf("Received transfer termination from remote peer")
			self.shouldTerminateCheckedTransfer = false
			return 0, self.FinishChecked(err)
		}

		// make sure we're not trying to write more than we should
		if (self.outboundTransferBytesWritten + len(p)) > self.outboundTransferTotalBytes {
			err := fmt.Errorf(
				"write exceeds declared size (%d bytes)",
				self.outboundTransferTotalBytes,
			)

			// send failure termination to peer and return error
			self.FinishChecked(err)
			return 0, err
		} else {
			// write to rolling checksum
			if _, err := io.Copy(self.outboundTransferChecksum, bytes.NewBuffer(p)); err != nil {
				return 0, fmt.Errorf("checksum error: %v", err)
			}

			// increment bytes written counter
			self.outboundTransferBytesWritten += len(p)
		}
	}

	// send the data block message to the peer
	n, err := self.SendMessage(NewMessage(DataBlock, p))

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
		self.outboundTransferActive = false
		self.outboundTransferChecksum = nil
	}()

	// if we're actually in a transfer...
	if self.outboundTransferActive {
		// if this call is error-free
		if len(errs) == 0 {
			sum := []byte{}

			// sum the running hash
			if self.outboundTransferChecksum != nil {
				sum = self.outboundTransferChecksum.Sum(nil)
			}

			// send the transfer termination (successful)
			log.Debugf("Completed checked transfer: %x", sum)
			_, err := self.SendMessage(NewMessage(DataFinalize, sum))

			return err
		} else {
			// encode and send the failed transfer termination with error message
			if errMsg, err := NewMessageEncoded(DataFailed, errs[0].Error(), StringEncoding); err == nil {
				_, err := self.SendMessage(errMsg)
				return err
			} else {
				return err
			}
		}
	} else {
		return nil
	}
}

func (self *RemotePeer) WriteChecked(send []byte) error {
	// start the checked transfer
	if err := self.BeginChecked(len(send)); err != nil {
		return err
	}

	// send the data
	if _, err := io.Copy(self, bytes.NewBuffer(send)); err != nil {
		return err
	}

	// complete the checked transfer
	if err := self.FinishChecked(); err != nil {
		return err
	}

	return nil
}

func (self *RemotePeer) ReadChecked() ([]byte, error) {
	var responseHeader *Message
	var expectedSize int
	response := bytes.NewBuffer(nil)

messageLoop:
	for {
		if message, err := self.ReceiveMessage(); err == nil {
			switch message.Type {
			case DataStart:
				if responseHeader == nil {
					if v, ok := message.Value().(uint64); ok {
						expectedSize = int(v)
						responseHeader = message
					} else {
						return nil, fmt.Errorf("Malformed checked transfer header")
					}
				} else {
					return nil, fmt.Errorf("Received duplicate checked transfer header")
				}

			case DataBlock:
				if message.Data != nil {
					response.Write(message.Data)
				}

			case DataFinalize:
				if expectedSize == 0 || message.actualSize == expectedSize {
					if bytes.Compare(message.Data, message.actualChecksum) != 0 {
						return nil, fmt.Errorf("checksum mismatch")
					} else {
						break messageLoop
					}
				} else {
					return nil, fmt.Errorf("size mismatch; expected: %d, got: %d", expectedSize, message.actualSize)
				}

			case DataFailed:
				msg, _ := message.Value().(string)
				return nil, fmt.Errorf(msg)

			default:
				return nil, fmt.Errorf("Invalid response")
			}
		} else {
			return nil, err
		}
	}

	return response.Bytes(), nil
}

// Performs a transaction with another peer by performing a checked transfer with them, then
// reading a response and returning it.
func (self *RemotePeer) Transact(send []byte) ([]byte, error) {
	if err := self.WriteChecked(send); err != nil {
		return nil, err
	}

	return self.ReadChecked()
}
