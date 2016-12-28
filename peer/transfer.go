package peer

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"github.com/satori/go.uuid"
	"hash"
	"io"
)

type Transfer struct {
	ID               uuid.UUID   `json:"id"`
	Peer             *RemotePeer `json:"peer"`
	ExpectedSize     uint64      `json:"expected_size"`
	BytesReceived    uint64      `json:"bytes_received"`
	ExpectedChecksum []byte      `json:"expected_checksum"`
	ActualChecksum   []byte      `json:"checksum"`
	hasher           hash.Hash
	destination      io.Writer
	finished         bool
	completed        chan error
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

func (self *Transfer) SetWriter(w io.Writer) {
	self.destination = w
}

func (self *Transfer) Wait() error {
	err := <-self.completed
	self.finished = true
	return err
}

func (self *Transfer) IsFinished() bool {
	return self.finished
}

func (self *Transfer) Write(p []byte) (int, error) {
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
	self.ActualChecksum = self.hasher.Sum(nil)

	if bytes.Compare(self.ExpectedChecksum, self.ActualChecksum) != 0 {
		return fmt.Errorf("checksum mismatch")
	}

	return nil
}
