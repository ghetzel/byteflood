package peer

import (
	"fmt"
	"strings"

	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/util"
	"github.com/ghetzel/go-stockutil/sliceutil"
)

type AuthorizedPeer struct {
	ID        string `json:"id"`
	PeerName  string `json:"name"`
	Groups    string `json:"groups,omitempty"`
	Addresses string `json:"addresses,omitempty"`
}

func ExpandPeerGroup(groupOrName string) ([]string, error) {
	names := make([]string, 0)

	if strings.HasPrefix(groupOrName, `@`) {
		if err := db.AuthorizedPeers.Each(AuthorizedPeer{}, func(v interface{}, err error) {
			if err == nil {
				if peer, ok := v.(*AuthorizedPeer); ok {
					if peer.IsMemberOf(groupOrName) {
						names = append(names, peer.PeerName)
					}
				}
			}
		}); err != nil {
			return nil, err
		}
	} else {
		if nameFilter, err := db.ParseFilter(map[string]interface{}{
			`name`: groupOrName,
		}); err == nil {
			var peers []*AuthorizedPeer

			if err := db.AuthorizedPeers.Find(nameFilter, &peers); err == nil {
				for _, peer := range peers {
					names = append(names, peer.PeerName)
				}
			} else {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	return names, nil
}

func GetAuthorizedPeer(id string) (*AuthorizedPeer, error) {
	v := db.AuthorizedPeers.NewInstance()

	if err := db.AuthorizedPeers.Get(id, v); err == nil {
		if ap, ok := v.(*AuthorizedPeer); ok {
			return ap, nil
		} else {
			return nil, fmt.Errorf("invalid type")
		}
	} else {
		return nil, err
	}
}

func (self *AuthorizedPeer) GetAddresses() []string {
	return util.SplitMulti.Split(self.Addresses, -1)
}

func (self *AuthorizedPeer) IsMemberOf(groupOrName string) bool {
	if strings.HasPrefix(groupOrName, `@`) {
		if sliceutil.ContainsAnyString(
			util.SplitMulti.Split(self.Groups, -1),
			groupOrName,
		) {
			return true
		}
	} else {
		if self.PeerName == groupOrName {
			return true
		}
	}

	return false
}
