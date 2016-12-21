package byteflood

import (
	"encoding/json"
	"github.com/ghetzel/byteflood/scanner"
	"github.com/ghetzel/diecast"
	"github.com/julienschmidt/httprouter"
	"github.com/urfave/negroni"
	"net/http"
)

type API struct {
	Address     string
	UiDirectory string
	app         *Application
	scanner     *scanner.Scanner
}

var DefaultApiAddress = `:10451`

func NewAPI(app *Application) *API {
	return &API{
		Address:     DefaultApiAddress,
		UiDirectory: `./ui`,
		app:         app,
	}
}

func (self *API) Serve() error {
	uiDir := self.UiDirectory

	if self.UiDirectory == `embedded` {
		uiDir = `/`
	}

	server := negroni.New()
	router := httprouter.New()
	ui := diecast.NewServer(uiDir)
	ui.RoutePrefix = `/ui`

	// if self.UiDirectory == `embedded` {
	// 	ui.SetFileSystem(assetFS())
	// }

	if err := ui.Initialize(); err != nil {
		return err
	}

	// routes not registered below will fallback to the UI server
	router.NotFound = ui

	router.GET(`/`, func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		http.Redirect(w, req, `/ui`, 301)
	})

	router.GET(`/peers`, func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		rv := make([]map[string]interface{}, 0)

		for _, peer := range self.app.LocalPeer().GetPeers() {
			rv = append(rv, peer.ToMap())
		}

		if err := json.NewEncoder(w).Encode(rv); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	router.GET(`/peers/:id`, func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		if remotePeer, ok := self.app.LocalPeer().GetPeer(params.ByName(`id`)); ok {
			if err := json.NewEncoder(w).Encode(remotePeer.ToMap()); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		} else {
			http.Error(w, "peer not found", http.StatusNotFound)
		}
	})

	server.UseHandler(router)

	log.Debugf("Running API server at %s", self.Address)
	server.Run(self.Address)
	return nil
}

// This returns an http.Handler that will respond to HTTP requests from remote peers.
func (self *API) peerRequestHandler() http.Handler {
	router := httprouter.New()

	router.GET(`/`, func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		w.Header().Set(`Content-Type`, `application/json`)
		json.NewEncoder(w).Encode(map[string]interface{}{
			`peer`: map[string]interface{}{
				`id`: self.app.LocalPeer().ID(),
			},
		})
	})

	return router
}
