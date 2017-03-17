package byteflood

import (
	"github.com/ghetzel/byteflood/db"
	"github.com/husobee/vestigo"
	"io"
	"net/http"
	"os"
)

func (self *API) handleGetQueue(w http.ResponseWriter, req *http.Request) {
	Respond(w, self.application.Queue)
}

func (self *API) handleGetQueuedDownloads(w http.ResponseWriter, req *http.Request) {
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
		vestigo.Param(req, `share`),
		vestigo.Param(req, `file`),
	); err == nil {
		http.Error(w, ``, http.StatusNoContent)
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (self *API) handleDownloadFile(w http.ResponseWriter, req *http.Request) {
	session := vestigo.Param(req, `session`)
	fileId := vestigo.Param(req, `file`)

	if mimeType := req.URL.Query().Get(`mimetype`); mimeType != `` {
		w.Header().Set(`Content-Type`, mimeType)
	}

	if session != `` {
		if _, err := self.application.Queue.Download(
			w,
			session,
			fileId,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	} else {
		var file db.File

		if err := db.Metadata.Get(fileId, &file); err == nil {
			if absPath, err := file.GetAbsolutePath(); err == nil {
				if osFile, err := os.Open(absPath); err == nil {
					if n, err := io.Copy(w, osFile); err == nil {
						log.Debugf("Transferred %d bytes", n)
					} else {
						log.Error(err)
						w.WriteHeader(http.StatusInternalServerError)
					}
				} else {
					log.Error(err)
					w.WriteHeader(http.StatusInternalServerError)
				}
			} else {
				log.Error(err)
				w.WriteHeader(http.StatusInternalServerError)
			}
		} else {
			log.Error(err)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}
