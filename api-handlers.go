package byteflood

import (
	"encoding/json"
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/shares"
	"github.com/ghetzel/pivot/dal"
	"github.com/ghetzel/pivot/mapper"
	"github.com/husobee/vestigo"
	"github.com/satori/go.uuid"
	"io"
	"net/http"
	"os"
	"strings"
)

func (self *API) handleStatus(w http.ResponseWriter, req *http.Request) {
	Respond(w, map[string]interface{}{
		`version`: Version,
	})
}

func (self *API) handleGetConfig(w http.ResponseWriter, req *http.Request) {
	Respond(w, self.application)
}

func (self *API) handleGetNewModelInstance(w http.ResponseWriter, req *http.Request) {
	parts := strings.Split(req.URL.Path, `/`)

	if len(parts) >= 3 {
		modelName := parts[2]

		switch modelName {
		case `shares`:
			Respond(w, shares.NewShare())
		default:
			http.Error(w, fmt.Sprintf("Unknown model '%s'", modelName), http.StatusNotFound)
		}
	} else {
		http.Error(w, `Not Found`, http.StatusNotFound)
	}
}

func (self *API) handleSaveModel(w http.ResponseWriter, req *http.Request) {
	parts := strings.Split(req.URL.Path, `/`)

	if len(parts) >= 3 {
		var recordset dal.RecordSet
		var model mapper.Mapper
		modelName := parts[2]

		switch modelName {
		case `shares`:
			model = db.Shares
		default:
			http.Error(w, fmt.Sprintf("Unknown model '%s'", modelName), http.StatusNotFound)
			return
		}

		if err := json.NewDecoder(req.Body).Decode(&recordset); err == nil {
			var err error

			for _, record := range recordset.Records {
				if req.Method == `POST` {
					err = model.Create(record)
				} else {
					err = model.Update(record)
				}

				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			}

			http.Error(w, ``, http.StatusNoContent)
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	} else {
		http.Error(w, ``, http.StatusNotFound)
	}
}

func (self *API) handleDeleteModel(w http.ResponseWriter, req *http.Request) {
	parts := strings.Split(req.URL.Path, `/`)

	if len(parts) >= 3 {
		var model mapper.Mapper

		modelName := parts[2]

		switch modelName {
		case `shares`:
			model = db.Shares
		default:
			http.Error(w, fmt.Sprintf("Unknown model '%s'", modelName), http.StatusNotFound)
			return
		}

		if err := model.Delete(vestigo.Param(req, `id`)); err == nil {
			http.Error(w, ``, http.StatusNoContent)
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	} else {
		http.Error(w, ``, http.StatusNotFound)
	}
}

func (self *API) handleGetQueue(w http.ResponseWriter, req *http.Request) {
	Respond(w, self.application.Queue)
}

func (self *API) handleEnqueueFile(w http.ResponseWriter, req *http.Request) {
	self.application.Queue.Add(
		vestigo.Param(req, `peer`),
		vestigo.Param(req, `file`),
	)

	http.Error(w, ``, http.StatusNoContent)
}

func (self *API) handleDownloadFile(w http.ResponseWriter, req *http.Request) {
	if download, err := self.application.Queue.Download(
		vestigo.Param(req, `peer`),
		vestigo.Param(req, `file`),
	); err == nil {
		Respond(w, download)
	} else if os.IsExist(err) {
		http.Error(w, err.Error(), http.StatusConflict)
	} else {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (self *API) handleGetDatabase(w http.ResponseWriter, req *http.Request) {
	Respond(w, self.application.Database)
}

func (self *API) handleGetDatabaseItem(w http.ResponseWriter, req *http.Request) {
	if record, err := self.application.Database.RetrieveRecord(vestigo.Param(req, `id`)); err == nil {
		Respond(w, record)
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func (self *API) handleQueryDatabase(w http.ResponseWriter, req *http.Request) {
	if limit, offset, sort, err := self.getSearchParams(req); err == nil {
		if f, err := db.ParseFilter(vestigo.Param(req, `_name`)); err == nil {
			f.Limit = limit
			f.Offset = offset
			f.Sort = sort

			if v := self.qs(req, `fields`); v != `` {
				f.Fields = strings.Split(v, `,`)
			}

			if recordset, err := self.application.Database.Query(
				db.MetadataSchema.Name,
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

func (self *API) handleBrowseDatabase(w http.ResponseWriter, req *http.Request) {
	if limit, offset, sort, err := self.getSearchParams(req); err == nil {
		query := ``

		if parent := vestigo.Param(req, `parent`); parent == `` {
			query = fmt.Sprintf("parent=%s", db.RootDirectoryName)
		} else {
			query = fmt.Sprintf("parent=%s", strings.TrimPrefix(parent, `/`))
		}

		if f, err := db.ParseFilter(query); err == nil {
			f.Limit = limit
			f.Offset = offset
			f.Sort = sort

			if v := self.qs(req, `fields`); v != `` {
				f.Fields = strings.Split(v, `,`)
			}

			if recordset, err := self.application.Database.Query(
				db.MetadataSchema.Name,
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

func (self *API) handleListValuesInDatabase(w http.ResponseWriter, req *http.Request) {
	fV := strings.TrimPrefix(vestigo.Param(req, `fields`), `/`)

	if fV == `` {
		http.Error(w, `Must specify at least one field name to list`, http.StatusBadRequest)
		return
	}

	fields := strings.Split(fV, `/`)

	if v := self.qs(req, `q`); v == `` {
		if rs, err := self.application.Database.ListMetadata(fields); err == nil {
			Respond(w, rs)
			return
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		if f, err := db.ParseFilter(v); err == nil {
			if rs, err := self.application.Database.ListMetadata(fields, f); err == nil {
				Respond(w, rs)
				return
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
}

func (self *API) handleActionDatabase(w http.ResponseWriter, req *http.Request) {
	switch vestigo.Param(req, `action`) {
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

func (self *API) handleGetPeers(w http.ResponseWriter, req *http.Request) {
	rv := make([]map[string]interface{}, 0)

	for _, peer := range self.application.LocalPeer.GetPeers() {
		rv = append(rv, peer.ToMap())
	}

	Respond(w, &rv)
}

func (self *API) handleConnectPeer(w http.ResponseWriter, req *http.Request) {
	payload := PeerConnectRequest{}

	if err := json.NewDecoder(req.Body).Decode(&payload); err == nil {
		go self.application.LocalPeer.ConnectToAndMonitor(payload.Address)
		http.Error(w, ``, http.StatusNoContent)
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (self *API) handleGetPeer(w http.ResponseWriter, req *http.Request) {
	if remotePeer, ok := self.application.LocalPeer.GetPeer(vestigo.Param(req, `peer`)); ok {
		Respond(w, remotePeer.ToMap())
	} else {
		http.Error(w, "peer not found", http.StatusNotFound)
	}
}

func (self *API) handleProxyToPeer(w http.ResponseWriter, req *http.Request) {
	if remotePeer, ok := self.application.LocalPeer.GetPeer(vestigo.Param(req, `peer`)); ok {
		if response, err := remotePeer.ServiceRequest(
			req.Method,
			vestigo.Param(req, `path`),
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

func (self *API) handleGetShares(w http.ResponseWriter, req *http.Request) {
	var shares []shares.Share

	if err := db.Shares.All(&shares); err == nil {
		Respond(w, shares)
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (self *API) handleGetShare(w http.ResponseWriter, req *http.Request) {
	share := shares.NewShare()

	if err := db.Shares.Get(vestigo.Param(req, `id`), share); err == nil {
		Respond(w, share)
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func (self *API) handleQueryShare(w http.ResponseWriter, req *http.Request) {
	share := shares.NewShare()

	if err := db.Shares.Get(vestigo.Param(req, `id`), share); err == nil {
		if limit, offset, sort, err := self.getSearchParams(req); err == nil {
			if results, err := share.Find(vestigo.Param(req, `_name`), limit, offset, sort); err == nil {
				Respond(w, results)
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func (self *API) handleBrowseShare(w http.ResponseWriter, req *http.Request) {
	share := shares.NewShare()

	if err := db.Shares.Get(vestigo.Param(req, `id`), share); err == nil {
		if limit, offset, sort, err := self.getSearchParams(req); err == nil {
			query := ``

			if parent := vestigo.Param(req, `parent`); parent == `` {
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
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func (self *API) handleGetPeerStatus(w http.ResponseWriter, req *http.Request) {
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

func (self *API) handleRequestFileFromShare(w http.ResponseWriter, req *http.Request) {
	// get remote peer from proxied request
	if remotePeer, ok := self.application.LocalPeer.GetPeer(req.Header.Get(`X-Byteflood-Session`)); ok {
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
		http.Error(w, `unknown peer`, http.StatusForbidden)
	}
}
