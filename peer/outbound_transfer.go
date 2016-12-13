package peer

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"github.com/satori/go.uuid"
	"hash"
	"io"
)

type OutboundTransfer struct {
	ID               uuid.UUID
	ExpectedSize     int
	BytesSent        int
	ExpectedChecksum []byte
	ActualChecksum   []byte
	peer             *RemotePeer
	hasher           hash.Hash
	shouldStop       error
	hasCompleted     bool
}

func NewOutboundTransfer(peer *RemotePeer, size int) *OutboundTransfer {
	return &OutboundTransfer{
		ID:           uuid.NewV4(),
		ExpectedSize: size,
		peer:         peer,
		hasher:       sha256.New(),
	}
}

func (self *OutboundTransfer) Initialize() error {
	// encode and send the transfer start header
	if message, err := NewMessageEncoded(DataStart, self.ExpectedSize, BinaryLEUint64); err == nil {
		if _, err := self.peer.SendMessage(message); err == nil {
			// transfers are answered with a go/no-go reply
			message := self.peer.WaitNextMessage()

			switch message.Type {
			case DataProceed:
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
}

func (self *OutboundTransfer) Write(p []byte) (int, error) {
	// check if we should be terminating an in-progress transfer
	if self.shouldStop != nil {
		return 0, self.completeTransfer(self.shouldStop)
	}

	// make sure we're not trying to write more than we should
	if (self.BytesSent + len(p)) > self.ExpectedSize {
		err := fmt.Errorf(
			"write exceeds declared size (%d bytes)",
			self.ExpectedSize,
		)

		// send failure termination to peer and return error
		self.completeTransfer(err)
		return 0, err
	} else {
		// write to rolling checksum
		if _, err := io.Copy(self.hasher, bytes.NewBuffer(p)); err != nil {
			return 0, fmt.Errorf("checksum error: %v", err)
		}

		// increment bytes written counter
		self.BytesSent += len(p)
	}

	// send the data block message to the peer
	n, err := self.peer.SendMessage(NewMessage(DataBlock, p))

	if err == nil {
		// if the message was written without error and it wrote more data than was requested,
		// tell the caller that all of their data was written (but not more).  Callers are not
		// expecting io.Writer to have written more than it received.
		if n > len(p) {
			n = len(p)
		}
	} else {
		// writing the data block failed somehow, inform the peer and stop the transfer
		self.completeTransfer(err)
	}

	return n, err
}

func (self *OutboundTransfer) Terminate(err error) {
	self.shouldStop = err
}

func (self *OutboundTransfer) Close() error {
	if !self.hasCompleted {
		return self.completeTransfer()
	} else {
		return fmt.Errorf("Transfer has already been completed")
	}
}

// Send a checked transfer termination message.  If an error is specified, the message type
// will indicate failure and include the error message.  If no error is given, the message
// type will indicate success and the message data will be the data checksum being summed
// during the write.
//
func (self *OutboundTransfer) completeTransfer(errs ...error) error {
	self.hasCompleted = true

	// if no errors were specified when calling this function
	if len(errs) == 0 {
		sum := self.hasher.Sum(nil)

		// send the transfer termination (successful)
		log.Debugf("Completed checked transfer: %x", sum)
		_, err := self.peer.SendMessage(NewMessage(DataFinalize, sum))

		return err
	} else {
		// encode and send the failed transfer termination with error message
		if errMsg, err := NewMessageEncoded(DataFailed, errs[0].Error(), StringEncoding); err == nil {
			_, err := self.peer.SendMessage(errMsg)
			return err
		} else {
			return err
		}
	}
}

func (self *OutboundTransfer) verifySize() error {
	if self.ExpectedSize > 0 {
		if self.BytesSent > self.ExpectedSize {
			return fmt.Errorf("write exceeds expected size")
		}
	}

	return nil
}
