package byteflood

import (
	"encoding/json"
	"fmt"
	"github.com/julienschmidt/httprouter"
	"io"
	"net/http"
)

func (self *API) handleStatus(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	Respond(w, map[string]interface{}{
		`version`: Version,
	})
}

func (self *API) handleGetConfig(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	Respond(w, self.application)
}

func (self *API) handleGetDatabase(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	Respond(w, self.application.Database)
}

func (self *API) handleQueryDatabase(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	if limit, offset, sort, err := self.getSearchParams(req); err == nil {
		if f, err := self.application.Database.ParseFilter(params.ByName(`query`)); err == nil {
			f.Limit = limit
			f.Offset = offset
			f.Sort = sort

			if recordset, err := self.application.Database.Query(
				self.application.Database.MetadataCollectionName,
				f,
			); err == nil {
				Respond(w, recordset)
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (self *API) handleActionDatabase(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	switch params.ByName(`action`) {
	case `scan`:
		payload := DatabaseScanRequest{}

		if req.ContentLength > 0 {
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}

		go self.application.Database.Scan(payload.Labels...)
		http.Error(w, ``, http.StatusNoContent)

	default:
		http.Error(w, ``, http.StatusNotFound)
	}
}

func (self *API) handleGetPeers(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	rv := make([]map[string]interface{}, 0)

	for _, peer := range self.application.LocalPeer.GetPeers() {
		rv = append(rv, peer.ToMap())
	}

	Respond(w, &rv)
}

func (self *API) handleConnectPeer(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	payload := PeerConnectRequest{}

	if err := json.NewDecoder(req.Body).Decode(&payload); err == nil {
		go self.application.LocalPeer.ConnectToAndMonitor(payload.Address)
		http.Error(w, ``, http.StatusNoContent)
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (self *API) handleGetPeer(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	if remotePeer, ok := self.application.LocalPeer.GetPeer(params.ByName(`id`)); ok {
		if err := json.NewEncoder(w).Encode(remotePeer.ToMap()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	} else {
		http.Error(w, "peer not found", http.StatusNotFound)
	}
}

func (self *API) handleProxyToPeer(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	if remotePeer, ok := self.application.LocalPeer.GetPeer(params.ByName(`session`)); ok {
		if response, err := remotePeer.ServiceRequest(
			req.Method,
			params.ByName(`path`),
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
		http.Error(w, "peer not found", http.StatusNotFound)
	}
}

func (self *API) handleGetShares(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	if err := json.NewEncoder(w).Encode(self.application.Shares); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (self *API) handleGetShare(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	if share, ok := self.application.GetShareByName(params.ByName(`name`)); ok {
		if err := json.NewEncoder(w).Encode(share); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	} else {
		http.Error(w, `Not Found`, http.StatusNotFound)
	}
}

func (self *API) handleQueryShare(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	if share, ok := self.application.GetShareByName(params.ByName(`name`)); ok {
		if limit, offset, sort, err := self.getSearchParams(req); err == nil {
			if results, err := share.Find(params.ByName(`query`), limit, offset, sort); err == nil {
				if err := json.NewEncoder(w).Encode(results); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	} else {
		http.Error(w, `Not Found`, http.StatusNotFound)
	}
}

func (self *API) handleBrowseShare(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	if share, ok := self.application.GetShareByName(params.ByName(`name`)); ok {
		if limit, offset, sort, err := self.getSearchParams(req); err == nil {
			query := fmt.Sprintf("parent=%s", params.ByName(`path`))

			if results, err := share.Find(query, limit, offset, sort); err == nil {
				if err := json.NewEncoder(w).Encode(results); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	} else {
		http.Error(w, `Not Found`, http.StatusNotFound)
	}
}

func (self *API) handleGetPeerStatus(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	Respond(w, map[string]interface{}{
		`peer`: map[string]interface{}{
			`id`: self.application.LocalPeer.ID(),
		},
	})
}
