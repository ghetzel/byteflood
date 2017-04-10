package byteflood

import (
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/go-stockutil/sliceutil"
	"github.com/husobee/vestigo"
	"net/http"
)

func (self *API) handleGetPeers(w http.ResponseWriter, req *http.Request) {
	var peers []peer.AuthorizedPeer

	if err := self.db.AuthorizedPeers.All(&peers); err == nil {
		Respond(w, peers)
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (self *API) handleGetPeer(w http.ResponseWriter, req *http.Request) {
	peer := new(peer.AuthorizedPeer)

	if err := self.db.AuthorizedPeers.Get(vestigo.Param(req, `id`), peer); err == nil {
		Respond(w, peer)
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func (self *API) handlePeersList(w http.ResponseWriter, req *http.Request) {
	field := vestigo.Param(req, `fields`)
	var spec interface{}

	if prefix := qs(req, `prefix`); prefix != `` {
		spec = map[string]interface{}{
			field: fmt.Sprintf("prefix:%v", prefix),
		}
	} else {
		spec = `all`
	}

	if f, err := db.ParseFilter(spec); err == nil {
		if results, err := self.db.AuthorizedPeers.ListWithFilter(
			[]string{field},
			f,
		); err == nil {
			if values, ok := results[field]; ok {
				Respond(w, sliceutil.Stringify(values))
			} else {
				Respond(w, make([]string, 0))
			}
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}
