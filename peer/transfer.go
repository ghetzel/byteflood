package peer

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"github.com/satori/go.uuid"
	"hash"
	"io"
	"time"
)

var TransferWriteTimeout = 10 * time.Second

// A Transfer represents the transfer of data from a remote peer to the local peer instance. Transfers can
// have an expected size, or be unbounded.  For bounded transfers, checks are performed to ensure that the
// transfer will terminate if the data written exceeds the expected amount, as well as calculating a
// checksum of the data received and verifying it against an expected checksum.
//
type Transfer struct {
	ID               uuid.UUID   `json:"id"`
	Peer             *RemotePeer `json:"peer"`
	ExpectedSize     uint64      `json:"expected_size"`
	BytesReceived    uint64      `json:"bytes_received"`
	ExpectedChecksum []byte      `json:"expected_checksum"`
	ActualChecksum   []byte      `json:"checksum"`
	Error            string      `json:"error"`
	hasher           hash.Hash
	destination      io.Writer
	finished         bool
	completed        chan error
	lastDataReceived time.Time
	dataTimer        *time.Timer
}

func NewTransfer(peer *RemotePeer, size uint64) *Transfer {
	return &Transfer{
		ID:           uuid.NewV4(),
		Peer:         peer,
		ExpectedSize: size,
		hasher:       sha256.New(),
		completed:    make(chan error),
	}
}

// Set the destination for all data received that belongs to this transfer.
func (self *Transfer) SetWriter(w io.Writer) {
	self.destination = w
}

// Block until the transfer is completed (successfully or otherwise).
func (self *Transfer) Wait() error {
	err := <-self.completed
	return err
}

// Returns whether the transfer has completed.
func (self *Transfer) IsFinished() bool {
	return self.finished
}

// Mark the transfer as being completed.
func (self *Transfer) Complete(err error) {
	select {
	case self.completed <- err:
	default:
	}

	if err != nil {
		self.Error = err.Error()
	}

	self.finished = true
}

// Implements the io.Writer interface for transfers.  Data will be added to the
// rolling checksum, and if given, written to the destination io.Writer.
func (self *Transfer) Write(p []byte) (int, error) {
	defer self.resetDataTimer()

	if self.finished {
		return 0, fmt.Errorf("attempted write on 	completed transfer")
	}

	self.BytesReceived += uint64(len(p))

	if err := self.verifySize(); err != nil {
		return 0, err
	}

	// write the block to the rolling checksum
	hashN, err := self.hasher.Write(p)

	// if a destination writer is set, write the data there
	if self.destination != nil {
		return self.destination.Write(p)
	}

	// otherwise, return the result of writing to the checksum
	return hashN, err
}

// Verify that the checksum of data recevied so far matches the given checksum.
func (self *Transfer) Verify(checksum []byte) error {
	self.ExpectedChecksum = checksum

	if err := self.verifySize(); err != nil {
		return err
	}

	if err := self.verifyChecksum(); err != nil {
		return err
	}

	return nil
}

func (self *Transfer) verifySize() error {
	if self.ExpectedSize > 0 {
		if self.BytesReceived > self.ExpectedSize {
			return fmt.Errorf("write exceeds expected size")
		}
	}

	return nil
}

func (self *Transfer) verifyChecksum() error {
	if self.ExpectedChecksum != nil {
		self.ActualChecksum = self.hasher.Sum(nil)

		if bytes.Compare(self.ExpectedChecksum, self.ActualChecksum) != 0 {
			return fmt.Errorf("checksum mismatch")
		}
	}

	return nil
}

func (self *Transfer) resetDataTimer() {
	if self.dataTimer != nil {
		if !self.dataTimer.Stop() {
			<-self.dataTimer.C
		}
	}

	self.dataTimer = time.AfterFunc(TransferWriteTimeout, func() {
		self.Complete(fmt.Errorf("Timed out waiting for next write in transfer %v", self.ID))
	})
}
