package byteflood

import (
	"encoding/json"
	"fmt"
	"github.com/ghetzel/diecast"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/julienschmidt/httprouter"
	"github.com/urfave/negroni"
	"io"
	"net/http"
)

type API struct {
	Address     string `json:"address,omitempty"`
	UiDirectory string `json:"ui_directory,omitempty"`
	application *Application
}

type PeerConnectRequest struct {
	Address string `json:"address"`
}

type DatabaseScanRequest struct {
	Labels []string `json:"labels"`
}

var DefaultApiAddress = `:11984`
var DefaultResultLimit = 25

func NewAPI() *API {
	return &API{
		Address:     DefaultApiAddress,
		UiDirectory: `./ui`, // TODO: this will be "embedded" after development settles
	}
}

func (self *API) Initialize() error {
	if self.application == nil {
		return fmt.Errorf("Cannot use API without an associated application instance")
	}

	if self.Address == `` {
		self.Address = DefaultApiAddress
	}

	if self.UiDirectory == `` {
		self.UiDirectory = `embedded`
	}

	return nil
}

func (self *API) Serve() error {
	uiDir := self.UiDirectory

	if self.UiDirectory == `embedded` {
		uiDir = `/`
	}

	server := negroni.New()
	router := httprouter.New()
	ui := diecast.NewServer(uiDir, `*.html`)

	// if self.UiDirectory == `embedded` {
	// 	ui.SetFileSystem(assetFS())
	// }

	if err := ui.Initialize(); err != nil {
		return err
	}

	// routes not registered below will fallback to the UI server
	router.NotFound = ui

	router.GET(`/api/status`, func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			`version`: Version,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	router.GET(`/api/configuration`, func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		if err := json.NewEncoder(w).Encode(self.application); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	router.GET(`/api/db`, func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		if err := json.NewEncoder(w).Encode(self.application.Database); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	router.POST(`/api/db/actions/scan`, func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		payload := DatabaseScanRequest{}

		if req.ContentLength > 0 {
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}

		go self.application.Database.Scan(payload.Labels...)
		http.Error(w, ``, http.StatusNoContent)
	})

	router.GET(`/api/peers`, func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		rv := make([]map[string]interface{}, 0)

		for _, peer := range self.application.LocalPeer.GetPeers() {
			rv = append(rv, peer.ToMap())
		}

		if err := json.NewEncoder(w).Encode(rv); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	router.POST(`/api/peers/actions/connect`, func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		payload := PeerConnectRequest{}

		if err := json.NewDecoder(req.Body).Decode(&payload); err == nil {
			go self.application.LocalPeer.ConnectToAndMonitor(payload.Address)
			http.Error(w, ``, http.StatusNoContent)
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	})

	router.GET(`/api/peers/:id`, func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		if remotePeer, ok := self.application.LocalPeer.GetPeer(params.ByName(`id`)); ok {
			if err := json.NewEncoder(w).Encode(remotePeer.ToMap()); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		} else {
			http.Error(w, "peer not found", http.StatusNotFound)
		}
	})

	for _, method := range []string{`GET`, `POST`, `PUT`, `DELETE`, `HEAD`} {
		router.Handle(method, `/api/proxy/:session/*path`, func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
			if remotePeer, ok := self.application.LocalPeer.GetPeer(params.ByName(`session`)); ok {
				if response, err := remotePeer.ServiceRequest(
					req.Method,
					params.ByName(`path`),
					req.Body,
					nil,
				); err == nil {
					// write response headers
					for key, values := range response.Header {
						for _, value := range values {
							w.Header().Add(key, value)
						}
					}

					// write response status code
					w.WriteHeader(response.StatusCode)

					// write response body
					io.Copy(w, response.Body)
				} else {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
			} else {
				http.Error(w, "peer not found", http.StatusNotFound)
			}
		})
	}

	router.GET(`/api/shares`, func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		if err := json.NewEncoder(w).Encode(self.application.Shares); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	router.GET(`/api/shares/:name`, func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		if share, ok := self.application.GetShareByName(params.ByName(`name`)); ok {
			if err := json.NewEncoder(w).Encode(share); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		} else {
			http.Error(w, `Not Found`, http.StatusNotFound)
		}
	})

	router.GET(`/api/shares/:name/query/*query`, func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		if share, ok := self.application.GetShareByName(params.ByName(`name`)); ok {
			limit := 0
			offset := 0

			if i, err := self.qsInt(req, `limit`); err == nil {
				if i > 0 {
					limit = int(i)
				} else {
					limit = DefaultResultLimit
				}
			} else {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			if i, err := self.qsInt(req, `offset`); err == nil {
				offset = int(i)
			} else {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			if results, err := share.Find(params.ByName(`query`), limit, offset); err == nil {
				if err := json.NewEncoder(w).Encode(results); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		} else {
			http.Error(w, `Not Found`, http.StatusNotFound)
		}
	})

	server.UseHandler(router)

	log.Debugf("Running API server at %s", self.Address)
	server.Run(self.Address)
	return nil
}

// This returns an http.Handler that will respond to HTTP requests from remote peers.
func (self *API) GetPeerRequestHandler() http.Handler {
	router := httprouter.New()

	router.GET(`/`, func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		w.Header().Set(`Content-Type`, `application/json`)
		json.NewEncoder(w).Encode(map[string]interface{}{
			`peer`: map[string]interface{}{
				`id`: self.application.LocalPeer.ID(),
			},
		})
	})

	return router
}

func (self *API) qsInt(req *http.Request, key string) (int64, error) {
	if v := req.URL.Query().Get(key); v != `` {
		if i, err := stringutil.ConvertToInteger(v); err == nil {
			return i, nil
		} else {
			return 0, fmt.Errorf("%s: %v", key, err)
		}
	}

	return 0, nil
}
