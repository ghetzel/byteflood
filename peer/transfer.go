package peer

import (
    "github.com/satori/go.uuid"
    "crypto/sha256"
)

type Transfer struct {
    ID   uuid.UUID
    Peer *RemotePeer
    ExpectedSize int
    ExpectedChecksum []byte
    ActualChecksum []byte
    hasher hash.Hash
}

func NewTransfer(peer *RemotePeer, size int) *Transfer {
    return &Transfer{
        ID: uuid.NewV4(),
        Peer: peer,
        ExpectedSize: size,
        hasher: sha256.New(),
    }
}

func (self *Transfer) Write(p []byte) (int, error) {
    return self.hasher.Write(p)
}
