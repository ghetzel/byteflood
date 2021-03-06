package byteflood

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/husobee/vestigo"
)

type PeerConnectRequest struct {
	Address string `json:"address"`
}

func (self *API) handleGetSessions(w http.ResponseWriter, req *http.Request) {
	rv := make([]map[string]interface{}, 0)

	for _, peer := range self.application.LocalPeer.GetSessions() {
		rv = append(rv, peer.ToMap())
	}

	Respond(w, &rv)
}

func (self *API) handleConnectSession(w http.ResponseWriter, req *http.Request) {
	payload := PeerConnectRequest{}

	if err := json.NewDecoder(req.Body).Decode(&payload); err == nil {
		go self.application.LocalPeer.ConnectToAndMonitor(payload.Address)
		http.Error(w, ``, http.StatusNoContent)
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (self *API) handleGetSession(w http.ResponseWriter, req *http.Request) {
	if remotePeer, ok := self.application.LocalPeer.GetSession(vestigo.Param(req, `session`)); ok {
		Respond(w, remotePeer.ToMap())
	} else {
		http.Error(w, "session not found", http.StatusNotFound)
	}
}

func (self *API) handleProxyToSession(w http.ResponseWriter, req *http.Request) {
	if remotePeer, ok := self.application.LocalPeer.GetSession(vestigo.Param(req, `session`)); ok {
		proxyPath := vestigo.Param(req, `_name`)
		proxyPath = strings.TrimPrefix(proxyPath, `/proxy`)
		proxyPath = `/` + strings.TrimPrefix(proxyPath, `/`)

		// log.Debugf("Proxying %s %s to peer %s (session: %s)", req.Method, proxyPath, remotePeer.Name, remotePeer.SessionID())

		if response, err := remotePeer.ServiceRequest(
			req.Method,
			proxyPath,
			req.Body,
			nil,
		); err == nil {
			// write response headers
			for key, values := range response.Header {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}

			// write response status code
			w.WriteHeader(response.StatusCode)

			// write response body
			io.Copy(w, response.Body)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	} else {
		http.Error(w, "session not found", http.StatusNotFound)
	}
}

func (self *API) handleGetSessionStatus(w http.ResponseWriter, req *http.Request) {
	if remotePeer, ok := self.application.LocalPeer.GetSession(req.Header.Get(`X-Byteflood-Session`)); ok {
		Respond(w, map[string]interface{}{
			`peer`: map[string]interface{}{
				`id`: self.application.LocalPeer.GetID(),
			},
			`requested_by`: map[string]interface{}{
				`id`: remotePeer.GetID(),
			},
		})
	} else {
		http.Error(w, `unknown session`, http.StatusForbidden)
	}
}
