package peer

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/encryption"
	"github.com/ghetzel/byteflood/util"
	"github.com/jbenet/go-base58"
	"github.com/orcaman/concurrent-map"
	"github.com/satori/go.uuid"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

var BadMessageThreshold = 10
var ServiceResponseTimeout = 10 * time.Second
var DefaultHeartbeatInterval = 10 * time.Second
var DefaultHeartbeatAckTimeout = 10 * time.Second

type MessageHandler func(*Message)

// A RemotePeer represents the remote end of a peer-to-peer connection (as opposed to a
// LocalPeer). One RemotePeer instance is created for each connection to another peer,
// regardless of which side initiated the connection.
//
// Each RemotePeer operates over an underlying TCP connection, with all traffic encrypted
// using the encryption.Encrypter and encryption.Decrypter interfaces.
//
type RemotePeer struct {
	Peer                  `json:"-"`
	ID                    string                        `json:"id"`
	Name                  string                        `json:"name"`
	Encryption            encryption.EncrypterDecrypter `json:"-"`
	Addresses             string                        `json:"addresses,omitempty"`
	HeartbeatInterval     time.Duration
	HeartbeatAckTimeout   time.Duration
	messageFn             MessageHandler
	inboundTransfers      cmap.ConcurrentMap
	connection            *net.TCPConn
	publicKey             []byte
	readLimiter           *util.RateLimitingReadWriter
	writeLimiter          *util.RateLimitingReadWriter
	heartbeatCount        int
	lastSeenAt            time.Time
	badMessageCount       int
	originalRequest       *PeeringRequest
	messagesAwaitingReply map[uuid.UUID]chan *Message
	messageReplyLock      sync.Mutex
}

func NewRemotePeerFromRequest(request *PeeringRequest, connection *net.TCPConn) (*RemotePeer, error) {
	if err := request.Validate(); err == nil {
		return &RemotePeer{
			ID:                    base58.Encode(request.PublicKey[:]),
			HeartbeatInterval:     DefaultHeartbeatInterval,
			HeartbeatAckTimeout:   DefaultHeartbeatAckTimeout,
			publicKey:             request.PublicKey,
			connection:            connection,
			originalRequest:       request,
			inboundTransfers:      cmap.New(),
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

func (self *RemotePeer) SessionID() string {
	return self.originalRequest.String()
}

func (self *RemotePeer) String() string {
	name := self.Name

	if name == `` {
		name = `UNKNOWN`
	}

	return fmt.Sprintf("%s/%s",
		name,
		self.connection.RemoteAddr().String(),
	)
}

func (self *RemotePeer) ToMap() map[string]interface{} {
	return map[string]interface{}{
		`id`:           self.ID,
		`name`:         self.Name,
		`session_id`:   self.SessionID(),
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
	if request, err := http.NewRequest(method, fmt.Sprintf("byteflood://%s%s", self.ID, path), body); err == nil {
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
	self.inboundTransfers.IterCb(func(id string, v interface{}) {
		if transfer, ok := v.(*Transfer); ok {
			log.Infof("Stopping transfer %v", id)
			transfer.Complete(fmt.Errorf("terminated due to disconnect"))
		}

		self.inboundTransfers.Remove(id)
	})

	return self.connection.Close()
}

// Retrieves the file manifest of the named share
//
func (self *RemotePeer) GetManifest(share string, fields ...string) (*db.Manifest, error) {
	if response, err := self.ServiceRequest(
		`GET`,
		fmt.Sprintf("/shares/%v/manifest", share),
		nil,
		map[string]string{
			`X-Byteflood-Manifest-Fields`: strings.Join(fields, `,`),
		},
	); err == nil {
		manifest := db.NewManifest(``)

		if err := manifest.LoadTSV(response.Body); err == nil {
			return manifest, nil
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}

// Receive the next message from this peer.
//
func (self *RemotePeer) ReceiveMessage() (*Message, error) {
	// imposes rate limiting on reads (if configured)
	reader := self.getReadRateLimiter(self.connection)

	// ensures that the decrypter is using the reader we've specified
	self.Encryption.SetSource(reader)

	if cleartext, err := self.Encryption.ReadPacket(); err == nil {
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

// Sends a message to the remote peer.
//
func (self *RemotePeer) SendMessage(message *Message) (int, error) {
	// imposes rate limiting on writes (if configured)
	writer := self.getWriteRateLimiter(self.connection)

	// get an exclusive lock on writing to the encrypter
	// release it once we've encrypted the data
	self.Encryption.Lock()
	defer self.Encryption.Unlock()

	// ensures that the encrypter is using the writer we've specified
	self.Encryption.SetTarget(writer)

	// encode the message for transport
	if encodedMessage, err := message.Encode(); err == nil {
		encodedMessageR := bytes.NewReader(encodedMessage)

		// write encoded message (cleartext) to encrypter, which will in turn write
		// the ciphertext and protocol data to the writer specified above
		if n, err := io.Copy(self.Encryption, encodedMessageR); err == nil {
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

// Sends the supplied message then waits up to the given timeout for a reply.  A reply is any message
// received whose GroupID field matches the GroupID of the message that was sent.  If the given
// message does not have a GroupID, one will be randomly generated before the message is sent.
//
// It is the remote peer's responsibility to send a message with a matching GroupID before timeout
// elapses.
//
func (self *RemotePeer) SendMessageChecked(message *Message, timeout time.Duration) (*Message, error) {
	if uuid.Equal(message.GroupID, uuid.Nil) {
		message.GroupID = uuid.NewV4()
	}

	replyChan := make(chan *Message)
	self.messageReplyLock.Lock()
	self.messagesAwaitingReply[message.GroupID] = replyChan
	self.messageReplyLock.Unlock()

	defer self.RemoveMessageReply(message.GroupID)

	if _, err := self.SendMessage(message); err == nil {
		select {
		case reply := <-replyChan:
			// log.Debugf("[%s] Received reply to message %v(%v): %v", self.String(), message.Type, message.GroupID, reply.Type)
			return reply, nil
		case <-time.After(timeout):
			return nil, fmt.Errorf("Timed out waiting for reply to message %v", message.GroupID)
		}
	} else {
		return nil, err
	}
}

func (self *RemotePeer) RemoveMessageReply(id uuid.UUID) {
	self.messageReplyLock.Lock()
	delete(self.messagesAwaitingReply, id)
	self.messageReplyLock.Unlock()
}

// Sets up a new inbound transfer.  Only data messages belonging to a known inbound transfer will be
// accepted.
func (self *RemotePeer) CreateInboundTransfer(size uint64) *Transfer {
	transfer := NewTransfer(self, size)
	self.inboundTransfers.Set(transfer.ID.String(), transfer)

	return transfer
}

// Retrieves an in-progress inbound transfer in a thread safe manner.
func (self *RemotePeer) GetInboundTransfer(id uuid.UUID) (*Transfer, bool) {
	if v, ok := self.inboundTransfers.Get(id.String()); ok {
		if transfer, ok := v.(*Transfer); ok {
			return transfer, true
		}
	}

	return nil, false
}

// Removes an inbound transfer by ID in a thread safe manner.
func (self *RemotePeer) RemoveInboundTransfer(id uuid.UUID) {
	self.inboundTransfers.Remove(id.String())
}

// Starts an outbound transfer. If a non-zero size is specified, the receiving peer will
// verify that the received data is exactly that length.  If size is zero, this verification will not
// be performed. It is recommended that if the size is known beforehand that it should be sent.
//
func (self *RemotePeer) CreateOutboundTransfer(id uuid.UUID, size uint64) (*OutboundTransfer, error) {
	transfer := NewOutboundTransfer(self, id, size)

	if err := transfer.Initialize(); err == nil {
		return transfer, nil
	} else {
		return nil, err
	}
}

// Transfers the contents of the given io.Reader to the peer in a streaming fashion.
//
func (self *RemotePeer) TransferStream(id uuid.UUID, stream io.Reader) error {
	if transfer, err := self.CreateOutboundTransfer(id, 0); err == nil {
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
func (self *RemotePeer) TransferData(id uuid.UUID, data []byte) error {
	if transfer, err := self.CreateOutboundTransfer(id, uint64(len(data))); err == nil {
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
func (self *RemotePeer) TransferFile(id uuid.UUID, path string) error {
	log.Debugf("[%v] Transfer requested for file at %s", id, path)

	if file, err := os.Open(path); err == nil {
		if stat, err := file.Stat(); err == nil {
			if transfer, err := self.CreateOutboundTransfer(id, uint64(stat.Size())); err == nil {
				log.Infof("[%v] Sending file %s (%d bytes)", self, path, stat.Size())

				if _, err := io.Copy(transfer, file); err == nil {
					return transfer.Close()
				} else {
					log.Debugf("[%v] Transfer failed: %v", id, err)
					return err
				}
			} else {
				log.Debugf("[%v] Transfer failed: %v", id, err)
				return err
			}
		} else {
			log.Debugf("[%v] Transfer failed: %v", id, err)
			return err
		}
	} else {
		log.Debugf("[%v] Transfer failed: %v", id, err)
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
	log.Noticef("Connected to %s (Session ID: %s)", self.String(), self.SessionID())

	// any condition that causes this function to return should also cause a disconnect
	defer func() {
		self.Disconnect()
		localPeer.RemovePeer(self.SessionID())
	}()

	for {
		if message, err := self.receiveMessagesIterate(localPeer); err == nil {
			if message != nil && self.messageFn != nil {
				self.messageFn(message)
			}
		} else {
			log.Errorf("protocol error: %v", err)
			return err
		}
	}

	err := fmt.Errorf("protocol error: unknown")
	log.Error(err)
	return err
}

func (self *RemotePeer) receiveMessagesIterate(localPeer *LocalPeer) (*Message, error) {
	if message, err := self.ReceiveMessage(); err == nil {
		self.badMessageCount = 0

		var replyErr error

		// log.Debugf("[%s] RECV: message-type=%s, payload=%d bytes", self.String(), message.Type.String(), len(message.Data))

		switch message.Type {
		case DataStart:
			// only accept data for known transfers
			if transfer, ok := self.GetInboundTransfer(message.GroupID); ok {
				v, ok := message.Value().(uint64)

				if !ok {
					v = 0
				}

				// only perform size check if we're expecting a specific amount of data
				if transfer.ExpectedSize == 0 || v == transfer.ExpectedSize {
					// TODO: local can reject by sending DataTerminate w/ an optional string reason
					//       based on a to-be-determined file receive policy

					// give peer the go-ahead to start writing data
					_, replyErr = self.SendMessage(NewGroupedMessage(message.GroupID, DataProceed, nil))
				} else {
					// terminate our transfer with the error
					transfer.Complete(fmt.Errorf("invalid transfer size (%d != %d)", v, transfer.ExpectedSize))

					if m, err := NewMessageEncoded(DataTerminate, "invalid transfer size", StringEncoding); err == nil {
						m.GroupID = message.GroupID
						_, replyErr = self.SendMessage(m)
					} else {
						replyErr = err
					}
				}
			} else {
				if m, err := NewMessageEncoded(DataTerminate, "unsolicited data", StringEncoding); err == nil {
					m.GroupID = message.GroupID
					_, replyErr = self.SendMessage(m)
				} else {
					replyErr = err
				}
			}

		case DataProceed:
			// this is explicitly waited for by RemotePeer, nothing to do here

		case DataTerminate:
			// TODO: handle when remote side terminates an inbound transfer

		case DataBlock:
			if transfer, ok := self.GetInboundTransfer(message.GroupID); ok {
				_, replyErr = transfer.Write(message.Data)
			} else {
				log.Warningf("unsolicited data block (%d bytes)", len(message.Data))
			}

			if replyErr != nil {
				if m, err := NewMessageEncoded(DataTerminate, replyErr.Error(), StringEncoding); err == nil {
					_, replyErr = self.SendMessage(m)
				} else {
					replyErr = err
				}
			}

		case DataFinalize:
			if transfer, ok := self.GetInboundTransfer(message.GroupID); ok {
				if err := transfer.Verify(message.Data); err == nil {
					// if the transfer was successful, inform the peer with an Acknowledgment
					_, replyErr = self.SendMessage(NewGroupedMessage(message.GroupID, Acknowledgment, nil))
					transfer.Complete(nil)

				} else {
					log.Errorf("[%s] Transfer failed verification: %v", self.String(), err)

					// if the transfer failed verification, reply with a failure message
					if m, err := NewMessageEncoded(DataFailed, err.Error(), StringEncoding); err == nil {
						m.GroupID = message.GroupID
						_, replyErr = self.SendMessage(m)
					} else {
						replyErr = err
					}

					transfer.Complete(err)
				}
			} else {
				log.Warningf("unsolicited data finalize message")
			}

		case ServiceRequest:
			responseBuffer := bytes.NewBuffer(nil)

			if err := localPeer.PeerServer().HandleRequest(self, responseBuffer, message.Data); err == nil {
				_, replyErr = self.SendMessage(NewGroupedMessage(
					message.GroupID,
					ServiceResponse,
					responseBuffer.Bytes(),
				))
			} else {
				replyErr = err
			}

		case Heartbeat:
			// change the type of the message and reply with it
			message.Type = HeartbeatAck
			_, replyErr = self.SendMessage(message)
		}

		// update this to track last contact time
		self.lastSeenAt = time.Now()

		if replyErr != nil {
			log.Errorf("[%s] Send reply failed: %v", self.String(), replyErr)
		}

		// if this message ID is being waited on, dispatch it
		self.messageReplyLock.Lock()
		replyChan, ok := self.messagesAwaitingReply[message.GroupID]
		self.messageReplyLock.Unlock()

		if ok {
			select {
			case replyChan <- message:
			default:
				log.Warningf("[%s] Message contained a reply nobody was waiting for", self.String())

				// it is normally the caller's responsibility to cleanup the message, but since
				// nobody was waiting, we'll do it.
				self.RemoveMessageReply(message.GroupID)
			}
		}

		return message, nil

	} else if err == io.EOF {
		log.Infof("Remote peer disconnected")

		return nil, io.EOF
	} else {
		log.Errorf("[%v] message error (%d/%d): %v", self, self.badMessageCount, BadMessageThreshold, err)
		self.badMessageCount += 1

		if self.badMessageCount >= BadMessageThreshold {
			return nil, err
		}

		return nil, nil
	}
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
