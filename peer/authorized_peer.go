package peer

import (
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/util"
	"github.com/ghetzel/go-stockutil/sliceutil"
	"github.com/ghetzel/pivot/filter"
	"strings"
)

type AuthorizedPeer struct {
	ID        string `json:"id"`
	PeerName  string `json:"name"`
	Groups    string `json:"groups,omitempty"`
	Addresses string `json:"addresses,omitempty"`
}

func ExpandPeerGroup(groupOrName string) []string {
	names := make([]string, 0)
	var f filter.Filter

	if strings.HasPrefix(groupOrName, `@`) {
		if groupFilter, err := db.ParseFilter(map[string]interface{}{
			`group`: strings.TrimPrefix(groupOrName, `@`),
		}); err == nil {
			f = groupFilter
		} else {
			log.Warning(err)
			return nil
		}
	} else {
		if nameFilter, err := db.ParseFilter(map[string]interface{}{
			`name`: groupOrName,
		}); err == nil {
			f = nameFilter
		} else {
			log.Warning(err)
			return nil
		}
	}

	var peers []*AuthorizedPeer

	if err := db.AuthorizedPeers.Find(f, &peers); err == nil {
		for _, peer := range peers {
			names = append(names, peer.PeerName)
		}
	} else {
		log.Warning(err)
	}

	return names
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
