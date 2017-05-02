package client

import (
	"fmt"
	"strings"
	"time"

	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/go-stockutil/typeutil"
)

type Peer struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	SessionID  string    `json:"session_id"`
	Address    string    `json:"address"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

func (self *Client) GetSessions() (output *[]Peer, err error) {
	err = self.Retrieve(`sessions`, nil, &output)
	return
}

func (self *Client) GetSession(idOrName string) (output *Peer, err error) {
	err = self.Retrieve(`sessions`, idOrName, &output)
	return
}

func (self *Client) GetAuthorizedPeers() (output []*peer.AuthorizedPeer, err error) {
	err = self.Retrieve(`peers`, nil, &output)
	return
}

func (self *Client) GetAuthorizedPeer(peerID string) (output *peer.AuthorizedPeer, err error) {
	err = self.Retrieve(`peers`, peerID, &output)
	return
}

func (self *Client) AuthorizePeer(id string, name string, groups []string, addresses []string) error {
	if typeutil.IsEmpty(id) {
		return fmt.Errorf("%q cannot be empty", `id`)
	}

	if typeutil.IsEmpty(name) {
		return fmt.Errorf("%q cannot be empty", `name`)
	}

	return self.Create(`peers`, peer.AuthorizedPeer{
		ID:        id,
		PeerName:  name,
		Groups:    strings.Join(groups, ` `),
		Addresses: strings.Join(addresses, ` `),
	})
}

func (self *Client) RevokePeer(id string) error {
	return self.Delete(`peers`, id)
}
