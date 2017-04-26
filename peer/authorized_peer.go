package peer

import (
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/util"
	"github.com/ghetzel/pivot/filter"
	"strings"
)

type AuthorizedPeer struct {
	ID        string `json:"id"`
	PeerName  string `json:"name"`
	Group     string `json:"group,omitempty"`
	Addresses string `json:"addresses,omitempty"`
	db        *db.Database
}

func ExpandPeerGroup(conn *db.Database, groupOrName string) []string {
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

	if err := conn.AuthorizedPeers.Find(f, &peers); err == nil {
		for _, peer := range peers {
			names = append(names, peer.PeerName)
		}
	} else {
		log.Warning(err)
	}

	return names
}

func GetAuthorizedPeer(conn *db.Database, id string) (*AuthorizedPeer, error) {
	v := conn.AuthorizedPeers.NewInstance()

	if err := conn.AuthorizedPeers.Get(id, v); err == nil {
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

func (self *AuthorizedPeer) SetDatabase(conn *db.Database) {
	self.db = conn
}

func (self *AuthorizedPeer) IsMemberOf(groupOrName string) bool {
	if strings.HasPrefix(groupOrName, `@`) {
		if strings.TrimPrefix(groupOrName, `@`) == self.Group {
			return true
		}
	} else {
		if self.PeerName == groupOrName {
			return true
		}
	}

	return false
}
