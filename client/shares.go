package client

import (
	"fmt"
	"github.com/ghetzel/byteflood/shares"
	"github.com/ghetzel/pivot/dal"
)

func (self *Client) GetShares(peerOrSession string) (output []*shares.Share, err error) {
	prefix := `shares`

	if peerOrSession == `` {
		prefix = fmt.Sprintf("sessions/%v/proxy/shares", peerOrSession)
	}

	err = self.Retrieve(prefix, nil, &output)
	return
}

func (self *Client) GetShare(shareID string, peerOrSession string) (output *shares.Share, err error) {
	prefix := `shares`

	if peerOrSession == `` {
		prefix = fmt.Sprintf("sessions/%v/proxy/shares", peerOrSession)
	}

	err = self.Retrieve(prefix, shareID, &output)
	return
}

func (self *Client) BrowseShare(shareID string, parent string, peerOrSession string) (output *dal.RecordSet, err error) {
	prefix := `shares`

	if peerOrSession == `` {
		prefix = fmt.Sprintf("sessions/%v/proxy/shares", peerOrSession)
	}

	err = self.Retrieve(
		prefix,
		fmt.Sprintf("%v/browse/%s", shareID, parent),
		&output,
	)
	return
}
