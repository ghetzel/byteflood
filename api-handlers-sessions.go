package byteflood

import (
	"encoding/json"
	"github.com/husobee/vestigo"
	"github.com/satori/go.uuid"
	"io"
	"net/http"
)

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
		if response, err := remotePeer.ServiceRequest(
			req.Method,
			`/`+vestigo.Param(req, `_name`),
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
				`id`: self.application.LocalPeer.ID(),
			},
			`requested_by`: map[string]interface{}{
				`id`: remotePeer.ID,
			},
		})
	} else {
		http.Error(w, `unknown session`, http.StatusForbidden)
	}
}

func (self *API) handleRequestFileFromShare(w http.ResponseWriter, req *http.Request) {
	// get remote peer from proxied request
	if remotePeer, ok := self.application.LocalPeer.GetSession(req.Header.Get(`X-Byteflood-Session`)); ok {
		// get the absolute filesystem path to the file at :id
		if absPath, err := self.application.Database.GetFileAbsolutePath(vestigo.Param(req, `file`)); err == nil {
			// parse the given :transfer UUID
			if transferId, err := uuid.FromString(vestigo.Param(req, `transfer`)); err == nil {
				// kick off the transfer on our end
				// TODO: this should be entered into an upload queue
				// self.application.QueueUpload(remotePeer, transferId, absPath)
				go remotePeer.TransferFile(transferId, absPath)
				http.Error(w, ``, http.StatusNoContent)
			} else {
				http.Error(w, err.Error(), http.StatusBadRequest)
			}
		} else {
			http.Error(w, err.Error(), http.StatusNotFound)
		}
	} else {
		http.Error(w, `unknown session`, http.StatusForbidden)
	}
}