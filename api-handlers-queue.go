package byteflood

import (
	"fmt"
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

	if err := self.db.Downloads.All(&downloads); err == nil {
		Respond(w, downloads)
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (self *API) handleEnqueueEntry(w http.ResponseWriter, req *http.Request) {
	if err := self.application.Queue.Add(
		vestigo.Param(req, `peer`),
		vestigo.Param(req, `share`),
		vestigo.Param(req, `entry`),
		`/dev/null`,
	); err == nil {
		http.Error(w, ``, http.StatusNoContent)
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (self *API) handleActionQueue(w http.ResponseWriter, req *http.Request) {
	action := vestigo.Param(req, `action`)

	switch action {
	case `clear`:
		if err := self.application.Queue.Clear(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	default:
		http.Error(w, fmt.Sprintf("Unknown action '%s'", action), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (self *API) handleDownloadFile(w http.ResponseWriter, req *http.Request) {
	session := vestigo.Param(req, `session`)
	entryId := vestigo.Param(req, `file`)

	if mimeType := req.URL.Query().Get(`mimetype`); mimeType != `` {
		w.Header().Set(`Content-Type`, mimeType)
	}

	if session != `` {
		if _, err := self.application.Queue.Download(
			w,
			session,
			entryId,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	} else {
		var entry db.Entry

		if err := self.db.Metadata.Get(entryId, &entry); err == nil {
			if absPath, err := entry.GetAbsolutePath(); err == nil {
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
