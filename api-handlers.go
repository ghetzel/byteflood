package byteflood

import (
	"encoding/json"
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/pivot/dal"
	"github.com/julienschmidt/httprouter"
	"github.com/satori/go.uuid"
	"io"
	"net/http"
	"os"
	"strings"
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

			if v := self.qs(req, `fields`); v != `` {
				f.Fields = strings.Split(v, `,`)
			}

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

func (self *API) handleBrowseDatabase(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	if limit, offset, sort, err := self.getSearchParams(req); err == nil {
		query := ``

		if parent := params.ByName(`parent`); parent == `/` {
			query = fmt.Sprintf("parent=%s", db.RootDirectoryName)
		} else {
			query = fmt.Sprintf("parent=%s", strings.TrimPrefix(parent, `/`))
		}

		if f, err := self.application.Database.ParseFilter(query); err == nil {
			f.Limit = limit
			f.Offset = offset
			f.Sort = sort

			if v := self.qs(req, `fields`); v != `` {
				f.Fields = strings.Split(v, `,`)
			}

			if recordset, err := self.application.Database.Query(
				db.MetadataCollectionName,
				f,
			); err == nil {
				Respond(w, recordset)
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (self *API) handleListValuesInDatabase(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	var recordset *dal.RecordSet

	fV := strings.TrimPrefix(params.ByName(`fields`), `/`)

	if fV == `` {
		http.Error(w, `Must specify at least one field name to list`, http.StatusBadRequest)
		return
	}

	fields := strings.Split(fV, `/`)

	if v := self.qs(req, `q`); v == `` {
		if rs, err := self.application.Database.ListMetadata(fields); err == nil {
			recordset = rs
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		if f, err := self.application.Database.ParseFilter(v); err == nil {
			if rs, err := self.application.Database.ListMetadata(fields, f); err == nil {
				recordset = rs
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	results := make(map[interface{}]interface{})

	for _, record := range recordset.Records {
		results[record.ID] = record.Get(`values`)
	}

	Respond(w, results)
}

func (self *API) handleActionDatabase(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	switch params.ByName(`action`) {
	case `scan`:
		payload := DatabaseScanRequest{
			Force: self.qsBool(req, `force`),
		}

		if req.ContentLength > 0 {
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}

		self.application.Database.ForceRescan = payload.Force

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
		Respond(w, remotePeer.ToMap())
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
	Respond(w, self.application.Shares)
}

func (self *API) handleGetShare(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	if share, ok := self.application.GetShareByName(params.ByName(`share`)); ok {
		Respond(w, share)
	} else {
		http.Error(w, `Not Found`, http.StatusNotFound)
	}
}

func (self *API) handleQueryShare(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	if share, ok := self.application.GetShareByName(params.ByName(`share`)); ok {
		if limit, offset, sort, err := self.getSearchParams(req); err == nil {
			if results, err := share.Find(params.ByName(`query`), limit, offset, sort); err == nil {
				Respond(w, results)
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
			query := ``

			if parent := params.ByName(`parent`); parent == `/` {
				query = fmt.Sprintf("parent=%s", db.RootDirectoryName)
			} else {
				query = fmt.Sprintf("parent=%s", strings.TrimPrefix(parent, `/`))
			}

			if results, err := share.Find(query, limit, offset, sort); err == nil {
				Respond(w, results)
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
