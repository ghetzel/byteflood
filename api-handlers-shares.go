package byteflood

import (
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/shares"
	"github.com/husobee/vestigo"
	"net/http"
	"strings"
)

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

func (self *API) handleGetShareFile(w http.ResponseWriter, req *http.Request) {
	share := shares.NewShare()

	if err := db.Shares.Get(vestigo.Param(req, `id`), share); err == nil {
		if file, err := share.Get(vestigo.Param(req, `file`)); err == nil {
			Respond(w, file)
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
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
