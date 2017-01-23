package byteflood

import (
	"github.com/husobee/vestigo"
	"net/http"
	// "os"
)

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
