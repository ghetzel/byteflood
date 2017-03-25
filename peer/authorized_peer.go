package peer

import (
	"github.com/ghetzel/byteflood/db"
	"regexp"
)

var rxAddrSplit = regexp.MustCompile(`[\s,;]+`)

type AuthorizedPeer struct {
	ID        string `json:"id"`
	PeerName  string `json:"name"`
	Group     string `json:"group,omitempty"`
	Addresses string `json:"addresses,omitempty"`
	db        *db.Database
}

func (self *AuthorizedPeer) GetAddresses() []string {
	return rxAddrSplit.Split(self.Addresses, -1)
}

func (self *AuthorizedPeer) SetDatabase(conn *db.Database) {
	self.db = conn
}
