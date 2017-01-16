package byteflood

import (
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/peer"
	"github.com/husobee/vestigo"
	"net/http"
)

func (self *API) handleGetPeers(w http.ResponseWriter, req *http.Request) {
	var peers []peer.RemotePeer

	if err := db.AuthorizedPeers.All(&peers); err == nil {
		Respond(w, peers)
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (self *API) handleGetPeer(w http.ResponseWriter, req *http.Request) {
	peer := new(peer.AuthorizedPeer)

	if err := db.AuthorizedPeers.Get(vestigo.Param(req, `id`), peer); err == nil {
		Respond(w, peer)
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}
