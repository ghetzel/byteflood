package peer

import (
	"encoding/json"
	"fmt"
	"github.com/julienschmidt/httprouter"
	"github.com/urfave/negroni"
	"net"
	"net/http"
)

type ManagementServer struct {
	addr      string
	localPeer *LocalPeer
	server    *negroni.Negroni
}

func NewManagementServer(localPeer *LocalPeer, addr string) *ManagementServer {
	if a, p, err := net.SplitHostPort(addr); err == nil {
		if a == `` {
			addr = fmt.Sprintf("127.0.0.1:%s", p)
		}
	}

	return &ManagementServer{
		localPeer: localPeer,
		addr:      addr,
		server:    negroni.New(),
	}
}

func (self *ManagementServer) ListenAndServe() error {
	router := httprouter.New()

	router.GET(`/peers`, func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		rv := make([]map[string]interface{}, 0)

		for _, peer := range self.localPeer.GetPeers() {
			rv = append(rv, peer.ToMap())
		}

		if err := json.NewEncoder(w).Encode(rv); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	router.GET(`/peers/:id`, func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		if remotePeer, ok := self.localPeer.GetPeer(params.ByName(`id`)); ok {
			if err := json.NewEncoder(w).Encode(remotePeer.ToMap()); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		} else {
			http.Error(w, "peer not found", http.StatusNotFound)
		}
	})

	self.server.UseHandler(router)

	log.Debugf("Running management server at %s", self.addr)
	self.server.Run(self.addr)

	return nil
}
