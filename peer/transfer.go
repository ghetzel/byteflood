package peer

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"github.com/satori/go.uuid"
	"hash"
)

type Transfer struct {
	ID               uuid.UUID
	Peer             *RemotePeer
	ExpectedSize     int
	BytesReceived    int
	ExpectedChecksum []byte
	ActualChecksum   []byte
	hasher           hash.Hash
}

func NewTransfer(peer *RemotePeer, size int) *Transfer {
	return &Transfer{
		ID:           uuid.NewV4(),
		Peer:         peer,
		ExpectedSize: size,
		hasher:       sha256.New(),
	}
}

func (self *Transfer) Write(p []byte) (int, error) {
	self.BytesReceived += len(p)

	if err := self.verifySize(); err != nil {
		return 0, err
	}

	return self.hasher.Write(p)
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
