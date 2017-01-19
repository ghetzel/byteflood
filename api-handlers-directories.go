package byteflood

import (
	"github.com/ghetzel/byteflood/db"
	"github.com/husobee/vestigo"
	"net/http"
)

func (self *API) handleGetScannedDirectories(w http.ResponseWriter, req *http.Request) {
	var dirs []*db.Directory

	if err := db.ScannedDirectories.All(&dirs); err == nil {
		Respond(w, dirs)
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (self *API) handleGetScannedDirectory(w http.ResponseWriter, req *http.Request) {
	dir := new(db.Directory)

	if err := db.ScannedDirectories.Get(vestigo.Param(req, `id`), dir); err == nil {
		Respond(w, dir)
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}
