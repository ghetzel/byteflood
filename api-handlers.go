package byteflood

import (
	"encoding/json"
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/julienschmidt/httprouter"
	"github.com/satori/go.uuid"
	"io"
	"net/http"
	"os"
)

func (self *API) handleStatus(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	Respond(w, map[string]interface{}{
		`version`: Version,
	})
}

func (self *API) handleGetConfig(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	Respond(w, self.application)
}

func (self *API) handleGetQueue(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	Respond(w, self.application.Queue)
}

func (self *API) handleEnqueueFile(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	self.application.Queue.Add(params.ByName(`peer`), params.ByName(`file`))
	http.Error(w, ``, http.StatusNoContent)
}

func (self *API) handleDownloadFile(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	if download, err := self.application.Queue.Download(params.ByName(`peer`), params.ByName(`file`)); err == nil {
		Respond(w, download)
	} else if os.IsExist(err) {
		http.Error(w, err.Error(), http.StatusConflict)
	} else {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (self *API) handleGetDatabase(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	Respond(w, self.application.Database)
}

func (self *API) handleGetDatabaseItem(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	if record, err := self.application.Database.RetrieveRecord(params.ByName(`id`)); err == nil {
		Respond(w, record)
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func (self *API) handleQueryDatabase(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	if limit, offset, sort, err := self.getSearchParams(req); err == nil {
		if f, err := self.application.Database.ParseFilter(params.ByName(`query`)); err == nil {
			f.Limit = limit
			f.Offset = offset
			f.Sort = sort

			if recordset, err := self.application.Database.Query(
				db.MetadataCollectionName,
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
	if remotePeer, ok := self.application.LocalPeer.GetPeer(params.ByName(`peer`)); ok {
		if err := json.NewEncoder(w).Encode(remotePeer.ToMap()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	} else {
		http.Error(w, "peer not found", http.StatusNotFound)
	}
}

func (self *API) handleProxyToPeer(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	if remotePeer, ok := self.application.LocalPeer.GetPeer(params.ByName(`peer`)); ok {
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
	if share, ok := self.application.GetShareByName(params.ByName(`share`)); ok {
		if err := json.NewEncoder(w).Encode(share); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	} else {
		http.Error(w, `Not Found`, http.StatusNotFound)
	}
}

func (self *API) handleQueryShare(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	if share, ok := self.application.GetShareByName(params.ByName(`share`)); ok {
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
	if share, ok := self.application.GetShareByName(params.ByName(`share`)); ok {
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
	if remotePeer, ok := self.application.LocalPeer.GetPeer(req.Header.Get(`X-Byteflood-Session`)); ok {
		Respond(w, map[string]interface{}{
			`peer`: map[string]interface{}{
				`id`: self.application.LocalPeer.ID(),
			},
			`requested_by`: map[string]interface{}{
				`id`: remotePeer.ID(),
			},
		})
	} else {
		http.Error(w, `unknown peer`, http.StatusForbidden)
	}
}

func (self *API) handleRequestFileFromShare(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	// get remote peer from proxied request
	if remotePeer, ok := self.application.LocalPeer.GetPeer(req.Header.Get(`X-Byteflood-Session`)); ok {
		// get the absolute filesystem path to the file at :id
		if absPath, err := self.application.Database.GetFileAbsolutePath(params.ByName(`file`)); err == nil {
			// parse the given :transfer UUID
			if transferId, err := uuid.FromString(params.ByName(`transfer`)); err == nil {
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
		http.Error(w, `unknown peer`, http.StatusForbidden)
	}
}
