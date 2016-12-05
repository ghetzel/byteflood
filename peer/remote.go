package peer

import (
	"github.com/hashicorp/yamux"
	"github.com/satori/go.uuid"
)

type RemotePeer struct {
	Peer
	PublicKey []byte
	session   *yamux.Session
	id        uuid.UUID
}

func (self *RemotePeer) String() string {
	return self.id.String()
}

func (self *RemotePeer) ID() []byte {
	return self.id.Bytes()
}
