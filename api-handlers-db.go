package byteflood

import (
	"encoding/json"
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/husobee/vestigo"
	"net/http"
	"strings"
)

type DatabaseScanRequest struct {
	Labels []string `json:"labels"`
	Force  bool     `json:"force"`
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
	fV := vestigo.Param(req, `_name`)

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
