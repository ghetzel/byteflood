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

	if err := db.Downloads.All(&downloads); err == nil {
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
	share := vestigo.Param(req, `share`)
	entryId := vestigo.Param(req, `file`)
	var outputWriter io.Writer

	// postprocessors intercept the data that would ordinarily be the response payload
	// and perform some operation on them. the processed data is then written to the given
	// io.Writer when the Flush() function is called.
	//
	if postprocessor := req.URL.Query().Get(`pp`); postprocessor != `` {
		if pp, ok := GetPostprocessorByName(postprocessor); ok {
			pp.SetWriter(w)

			defer func(rw http.ResponseWriter) {
				// remove Content-Length because we won't know what the final length is before
				// the postprocessor starts writing the output, at which point it's too late
				// to send a header.
				rw.Header().Del(`Content-Length`)

				// perform postprocessing and write data to the ResponseWriter
				pp.Flush()
			}(w)

			outputWriter = pp
		}
	} else {
		outputWriter = w
	}

	if mimeType := req.URL.Query().Get(`mimetype`); mimeType != `` {
		w.Header().Set(`Content-Type`, mimeType)
	}

	if session != `` {
		if _, err := self.application.Queue.Download(
			outputWriter,
			session,
			share,
			entryId,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	} else {
		var entry db.Entry

		if err := db.Metadata.Get(entryId, &entry); err == nil {
			if absPath, err := entry.GetAbsolutePath(); err == nil {
				if osFile, err := os.Open(absPath); err == nil {
					if !writeHttpHeadersForEntry(w, req, &entry) {
						return
					}

					switch req.Method {
					case `GET`:
						log.Debugf("User-Agent: %v", req.Header.Get(`User-Agent`))

						// actually write the data to the response
						if _, err := io.Copy(outputWriter, osFile); err != nil {
							log.Error(err)
							w.WriteHeader(http.StatusInternalServerError)
						}

					default:
						w.WriteHeader(http.StatusNoContent)
					}
				} else {
					log.Error(err)
					w.WriteHeader(http.StatusNotFound)
				}
			} else {
				log.Error(err)
				w.WriteHeader(http.StatusNotFound)
			}
		} else {
			log.Error(err)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}
