package client

import (
	"fmt"
	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/go-stockutil/typeutil"
	"strings"
)

func (self *Client) GetAuthorizedPeers() (output []*peer.AuthorizedPeer, err error) {
	err = self.Retrieve(`peers`, nil, &output)
	return
}

func (self *Client) GetAuthorizedPeer(peerID string) (output *peer.AuthorizedPeer, err error) {
	err = self.Retrieve(`peers`, peerID, &output)
	return
}

func (self *Client) GetSession(idOrName string) (output *peer.RemotePeer, err error) {
	err = self.Retrieve(`sessions`, idOrName, &output)
	return
}

func (self *Client) AuthorizePeer(id string, name string, group string, addresses []string) error {
	if typeutil.IsEmpty(id) {
		return fmt.Errorf("%q cannot be empty", `id`)
	}

	if typeutil.IsEmpty(name) {
		return fmt.Errorf("%q cannot be empty", `name`)
	}

	return self.Create(`peers`, peer.AuthorizedPeer{
		ID:        id,
		PeerName:  name,
		Group:     group,
		Addresses: strings.Join(addresses, `,`),
	})
}

func (self *Client) RevokePeer(id string) error {
	return self.Delete(`peers`, id)
}
