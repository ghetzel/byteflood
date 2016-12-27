package peer

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/ghetzel/byteflood/encryption"
	"github.com/ghetzel/byteflood/util"
	"github.com/jbenet/go-base58"
	"github.com/satori/go.uuid"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

var BadMessageThreshold = 10
var ServiceResponseTimeout = 5 * time.Second
var DefaultHeartbeatInterval = 1 * time.Second
var DefaultHeartbeatAckTimeout = 3 * time.Second

type MessageHandler func(*Message)

type RemotePeer struct {
	Peer                  `json:"-"`
	Name                  string                `json:"name"`
	Encrypter             *encryption.Encrypter `json:"-"`
	Decrypter             *encryption.Decrypter `json:"-"`
	HeartbeatInterval     time.Duration
	HeartbeatAckTimeout   time.Duration
	messageFn             MessageHandler
	inboundTransfer       *Transfer
	connection            *net.TCPConn
	publicKey             []byte
	readLimiter           *util.RateLimitingReadWriter
	writeLimiter          *util.RateLimitingReadWriter
	heartbeatCount        int
	lastSeenAt            time.Time
	badMessageCount       int
	originalRequest       *PeeringRequest
	messagesAwaitingReply map[uuid.UUID]chan *Message
	messageReplyLock      sync.RWMutex
}

func NewRemotePeerFromRequest(request *PeeringRequest, connection *net.TCPConn) (*RemotePeer, error) {
	if err := request.Validate(); err == nil {
		return &RemotePeer{
			HeartbeatInterval:     DefaultHeartbeatInterval,
			HeartbeatAckTimeout:   DefaultHeartbeatAckTimeout,
			publicKey:             request.PublicKey,
			connection:            connection,
			originalRequest:       request,
			messagesAwaitingReply: make(map[uuid.UUID]chan *Message),
		}, nil
	} else {
		return nil, err
	}
}

func (self *RemotePeer) SetMessageHandler(handler MessageHandler) {
	self.messageFn = handler
}

func (self *RemotePeer) GetPublicKey() []byte {
	return self.publicKey
}

func (self *RemotePeer) ID() string {
	return base58.Encode(self.publicKey[:])
}

func (self *RemotePeer) SessionID() string {
	addr := self.connection.RemoteAddr().String()
	return base58.Encode([]byte(addr[:]))
}

func (self *RemotePeer) String() string {
	return fmt.Sprintf("%s/%s",
		self.Name,
		self.connection.RemoteAddr().String(),
	)
}

func (self *RemotePeer) ToMap() map[string]interface{} {
	return map[string]interface{}{
		`id`:           self.ID(),
		`session_id`:   self.SessionID(),
		`name`:         self.Name,
		`public_key`:   fmt.Sprintf("%x", self.GetPublicKey()),
		`address`:      self.connection.RemoteAddr().String(),
		`last_seen_at`: self.lastSeenAt,
	}
}

func (self *RemotePeer) Write(p []byte) (int, error) {
	_, err := self.SendMessage(NewMessage(ServiceRequest, p))
	return len(p), err
}

func (self *RemotePeer) ServiceRequest(method string, path string, body io.Reader, headers map[string]string) (*http.Response, error) {
	if request, err := http.NewRequest(method, fmt.Sprintf("byteflood://%s%s", self.ID(), path), body); err == nil {
		if headers != nil {
			for k, v := range headers {
				request.Header.Set(k, v)
			}
		}

		buffer := bytes.NewBuffer(nil)

		if err := request.Write(buffer); err == nil {
			if message, err := self.SendMessageChecked(NewMessage(ServiceRequest, buffer.Bytes()), ServiceResponseTimeout); err == nil {
				responseBuffer := bytes.NewBuffer(message.Data)

				if response, err := http.ReadResponse(bufio.NewReader(responseBuffer), request); err == nil {
					return response, nil
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

// Run a continuous periodic heartbeat to ensure remote peer connection is available
func (self *RemotePeer) Heartbeat() error {
	if outMessage, err := NewMessageEncoded(Heartbeat, self.heartbeatCount, BinaryLEUint64); err == nil {
		if _, err := self.SendMessageChecked(outMessage, self.HeartbeatAckTimeout); err == nil {
			self.heartbeatCount += 1
			return nil
		} else {
			return err
		}
	} else {
		return err
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
			return message, nil
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
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
			// log.Debugf("[%s] SEND: [%s] Encrypted %d bytes (%d encoded, %d data)",
			// 	self.String(),
			// 	message.Type.String(),
			// 	n,
			// 	len(encodedMessage),
			// 	len(message.Data))
			return int(n), err
		} else {
			// log.Debugf("[%s] SEND: [%s] error: %v", self.String(), message.Type.String(), err)
			return 0, err
		}
	} else {
		return 0, err
	}
}

func (self *RemotePeer) SendMessageChecked(message *Message, timeout time.Duration) (*Message, error) {
	replyChan := make(chan *Message)
	self.messageReplyLock.Lock()
	message.ID = uuid.NewV4()
	self.messagesAwaitingReply[message.ID] = replyChan
	self.messageReplyLock.Unlock()

	defer self.ClearMessageReply(message)

	if _, err := self.SendMessage(message); err == nil {
		select {
		case reply := <-replyChan:
			// log.Debugf("[%s] Recevied reply to message %v(%v): %v", self.String(), message.Type, message.ID, reply.Type)
			return reply, nil
		case <-time.After(timeout):
			return nil, fmt.Errorf("Timed out waiting for reply to message %v", message.ID)
		}
	} else {
		return nil, err
	}
}

func (self *RemotePeer) ClearMessageReply(message *Message) {
	self.messageReplyLock.RLock()
	_, ok := self.messagesAwaitingReply[message.ID]
	self.messageReplyLock.RUnlock()

	if ok {
		self.messageReplyLock.Lock()
		delete(self.messagesAwaitingReply, message.ID)
		self.messageReplyLock.Unlock()
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

// Things that are running on and for the benefit of LocalPeer
//
// These functions are started from LocalPeer when a RemotePeer connects to it.
//
// ================================================================================================

// Receives messages from the given peer on an ongoing basis.  Returns an error
// representing a disconnect or other connection error.  This function never returns
// nil and is intended to be long-running on a per-peer basis.
//
func (self *RemotePeer) ReceiveMessages(localPeer *LocalPeer) error {
	log.Debugf("Receiving messages from %s (%s)", self.SessionID(), self.String())

	// any condition that causes this function to return should also cause a disconnect
	defer self.Disconnect()
	defer localPeer.RemovePeer(self.SessionID())

	for {
		if message, err := self.ReceiveMessagesIterate(localPeer); err == nil {
			if message != nil && self.messageFn != nil {
				self.messageFn(message)
			}
		} else {
			return err
		}
	}

	return fmt.Errorf("Protocol Error")
}

func (self *RemotePeer) ReceiveMessagesIterate(localPeer *LocalPeer) (*Message, error) {
	if message, err := self.ReceiveMessage(); err == nil {
		self.badMessageCount = 0

		var replyErr error

		// log.Debugf("[%s] RECV: message-type=%s, payload=%d bytes", self.String(), message.Type.String(), len(message.Data))

		switch message.Type {
		case DataStart:
			if self.inboundTransfer == nil {
				v, ok := message.Value().(uint64)

				if !ok {
					v = 0
				}

				// create a new inbound transfer
				self.inboundTransfer = NewTransfer(self, int(v))

				// TODO: local can reject by sending DataTerminate w/ an optional string reason
				//       based on a to-be-determined file receive policy

				// give peer the go-ahead to start writing data
				_, err = self.SendMessage(NewMessageReply(message.ID, DataProceed, nil))
			} else {
				if m, err := NewMessageEncoded(DataTerminate, "A transfer is already in progress with this peer", StringEncoding); err == nil {
					m.ID = message.ID
					_, replyErr = self.SendMessage(m)
				} else {
					replyErr = err
				}
			}

		case DataProceed:
			// this is explicitly waited for by RemotePeer, nothing to do here

		case DataBlock:
			if self.inboundTransfer != nil {
				_, replyErr = self.inboundTransfer.Write(message.Data)
			} else {
				log.Warningf("Received %d byte block outside of an active transfer", len(message.Data))
			}

			if replyErr != nil {
				if m, err := NewMessageEncoded(DataTerminate, err.Error(), StringEncoding); err == nil {
					_, replyErr = self.SendMessage(m)
				} else {
					replyErr = err
				}
			}

		case DataFinalize:
			if self.inboundTransfer != nil {
				if err := self.inboundTransfer.Verify(message.Data); err == nil {
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

				self.inboundTransfer = nil
			} else {
				log.Warningf("Received DataFinalize outside of an active transfer")
			}

		case ServiceRequest:
			responseBuffer := bytes.NewBuffer(nil)

			if err := localPeer.PeerServer().HandleRequest(self, responseBuffer, message.Data); err == nil {
				_, replyErr = self.SendMessage(NewMessageReply(
					message.ID,
					ServiceResponse,
					responseBuffer.Bytes(),
				))
			} else {
				replyErr = err
			}

		case Heartbeat:
			message.Type = HeartbeatAck
			_, replyErr = self.SendMessage(message)
		}

		// update this to track last contact time
		self.lastSeenAt = time.Now()

		if replyErr != nil {
			log.Errorf("[%s] Send reply failed: %v", self.String(), replyErr)
		}

		// if this message ID is being waited on, dispatch it
		self.messageReplyLock.RLock()
		replyChan, ok := self.messagesAwaitingReply[message.ID]
		self.messageReplyLock.RUnlock()

		if ok {
			select {
			case replyChan <- message:
			default:
				log.Warningf("[%s] Message contained a reply nobody was waiting for", self.String())

				// it is normally the caller's responsibility to cleanup the message, but since
				// nobody was waiting, we'll do it.
				self.ClearMessageReply(message)
			}
		}

		return message, nil

	} else if err == io.EOF {
		log.Infof("Remote peer disconnected")

		return nil, io.EOF
	} else {
		log.Errorf("error receiving message: %v", err)
		self.badMessageCount += 1

		if self.badMessageCount >= BadMessageThreshold {
			return nil, err
		}

		return nil, nil
	}
}
