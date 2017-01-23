package byteflood

import (
	"github.com/ghetzel/byteflood/db"
	"github.com/husobee/vestigo"
	"net/http"
	// "os"
)

func (self *API) handleGetQueue(w http.ResponseWriter, req *http.Request) {
	var downloads []QueuedDownload

	if err := db.Downloads.All(&downloads); err == nil {
		Respond(w, downloads)
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (self *API) handleEnqueueFile(w http.ResponseWriter, req *http.Request) {
	if err := self.application.Queue.Add(
		vestigo.Param(req, `peer`),
		vestigo.Param(req, `file`),
	); err == nil {
		http.Error(w, ``, http.StatusNoContent)
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

// func (self *API) handleDownloadFile(w http.ResponseWriter, req *http.Request) {
// 	if download, err := self.application.Queue.Download(
// 		vestigo.Param(req, `peer`),
// 		vestigo.Param(req, `file`),
// 	); err == nil {
// 		Respond(w, download)
// 	} else if os.IsExist(err) {
// 		http.Error(w, err.Error(), http.StatusConflict)
// 	} else {
// 		http.Error(w, err.Error(), http.StatusInternalServerError)
// 	}
// }
