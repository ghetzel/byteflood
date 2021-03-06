package byteflood

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/go-stockutil/httputil"
	"github.com/ghetzel/metabase"
	"github.com/ghetzel/pivot/dal"
	"github.com/husobee/vestigo"
)

type DatabaseScanRequest struct {
	Labels   []string `json:"labels"`
	DeepScan bool     `json:"deep"`
}

func (self *API) handleGetDatabase(w http.ResponseWriter, req *http.Request) {
	Respond(w, db.Instance)
}

func (self *API) handleGetDatabaseItem(w http.ResponseWriter, req *http.Request) {
	var record dal.Record

	if err := db.Metadata.Get(vestigo.Param(req, `id`), &record); err == nil {
		Respond(w, record)
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func (self *API) handleQueryDatabase(w http.ResponseWriter, req *http.Request) {
	if limit, offset, sort, err := getSearchParams(req); err == nil {
		if f, err := metabase.ParseFilter(vestigo.Param(req, `_name`)); err == nil {
			f.Limit = limit
			f.Offset = offset
			f.Sort = sort

			if v := httputil.Q(req, `fields`); v != `` {
				f.Fields = strings.Split(v, `,`)
			}

			var recordset dal.RecordSet

			if err := db.Metadata.Find(f, &recordset); err == nil {
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
	if limit, offset, sort, err := getSearchParams(req); err == nil {
		query := ``

		if parent := vestigo.Param(req, `parent`); parent == `` {
			query = fmt.Sprintf("parent=%s", metabase.RootGroupName)
		} else {
			query = fmt.Sprintf("parent=%s", strings.TrimPrefix(parent, `/`))
		}

		if f, err := metabase.ParseFilter(query); err == nil {
			f.Limit = limit
			f.Offset = offset
			f.Sort = sort

			if v := httputil.Q(req, `fields`); v != `` {
				f.Fields = strings.Split(v, `,`)
			}

			var recordset dal.RecordSet

			if err := db.Metadata.Find(f, &recordset); err == nil {
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

	if v := httputil.Q(req, `q`); v == `` {
		if rs, err := db.Metadata.List(fields); err == nil {
			Respond(w, rs)
			return
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		if f, err := metabase.ParseFilter(v); err == nil {
			if rs, err := db.Metadata.ListWithFilter(fields, f); err == nil {
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
			DeepScan: httputil.QBool(req, `deep`),
		}

		if req.ContentLength > 0 {
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}

		go db.Instance.Scan(payload.DeepScan, payload.Labels...)

	case `cleanup`:
		go db.Instance.Cleanup(false)

	default:
		http.Error(w, ``, http.StatusNotFound)
		return
	}

	http.Error(w, ``, http.StatusNoContent)
}
