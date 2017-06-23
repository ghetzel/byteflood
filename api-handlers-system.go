package byteflood

import (
	"net/http"

	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/metabase"
	"github.com/husobee/vestigo"
)

func (self *API) handleGetSystemProperties(w http.ResponseWriter, req *http.Request) {
	var properties []metabase.Property

	if err := db.System.All(&properties); err == nil {
		Respond(w, properties)
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (self *API) handleGetSystemProperty(w http.ResponseWriter, req *http.Request) {
	property := new(metabase.Property)

	if err := db.System.Get(vestigo.Param(req, `id`), property); err == nil {
		Respond(w, property)
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}
