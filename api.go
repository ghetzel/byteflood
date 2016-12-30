package byteflood

import (
	"fmt"
	"github.com/ghetzel/diecast"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/julienschmidt/httprouter"
	"github.com/urfave/negroni"
	"html/template"
	"net/http"
	"strings"
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
	Force  bool     `json:"force"`
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

	ui.AdditionalFunctions = template.FuncMap{
		`Autobyte`: stringutil.ToByteString,
		`Percent`: func(value interface{}, args ...interface{}) (string, error) {
			if v, err := stringutil.ConvertToFloat(value); err == nil {
				outOf := 100.0
				format := "%.f"

				if len(args) > 0 {
					if o, err := stringutil.ConvertToFloat(args[0]); err == nil {
						outOf = o
					} else {
						return ``, err
					}
				}

				if len(args) > 1 {
					format = fmt.Sprintf("%v", args[1])
				}

				percent := float64((float64(v) / float64(outOf)) * 100.0)

				return fmt.Sprintf(format, percent), nil
			} else {
				return ``, err
			}
		},
	}

	// if self.UiDirectory == `embedded` {
	// 	ui.SetFileSystem(assetFS())
	// }

	if err := ui.Initialize(); err != nil {
		return err
	}

	// routes not registered below will fallback to the UI server
	router.NotFound = ui

	router.GET(`/api/status`, self.handleStatus)
	router.GET(`/api/configuration`, self.handleGetConfig)
	router.GET(`/api/queue`, self.handleGetQueue)
	router.POST(`/api/queue/:peer/:file`, self.handleEnqueueFile)
	router.GET(`/api/db`, self.handleGetDatabase)
	router.GET(`/api/db/view/:id`, self.handleGetDatabaseItem)
	router.GET(`/api/db/query/*query`, self.handleQueryDatabase)
	router.GET(`/api/db/browse/*parent`, self.handleBrowseDatabase)
	router.GET(`/api/db/list/*fields`, self.handleListValuesInDatabase)
	router.POST(`/api/db/actions/:action`, self.handleActionDatabase)
	router.GET(`/api/peers`, self.handleGetPeers)
	router.POST(`/api/peers`, self.handleConnectPeer)
	router.GET(`/api/peers/:peer`, self.handleGetPeer)
	router.GET(`/api/peers/:peer/files/:file`, self.handleDownloadFile)

	for _, method := range []string{`GET`, `POST`, `PUT`, `DELETE`, `HEAD`} {
		router.Handle(method, `/api/peers/:peer/proxy/*path`, self.handleProxyToPeer)
	}

	router.GET(`/api/shares`, self.handleGetShares)
	router.GET(`/api/shares/:share`, self.handleGetShare)
	router.GET(`/api/shares/:share/query/*query`, self.handleQueryShare)
	router.GET(`/api/shares/:share/browse/*parent`, self.handleBrowseShare)

	server.UseHandler(router)

	log.Debugf("Running API server at %s", self.Address)
	server.Run(self.Address)
	return nil
}

// This returns an http.Handler that will respond to HTTP requests from remote peers.
func (self *API) GetPeerRequestHandler() http.Handler {
	router := httprouter.New()

	router.GET(`/`, self.handleGetPeerStatus)
	router.GET(`/db/view/:id`, self.handleGetDatabaseItem)
	router.GET(`/db/query/*query`, self.handleQueryDatabase)
	router.GET(`/db/browse/*parent`, self.handleBrowseDatabase)
	router.GET(`/db/list/*fields`, self.handleListValuesInDatabase)
	router.POST(`/transfers/:transfer/:file`, self.handleRequestFileFromShare)
	router.GET(`/shares`, self.handleGetShares)
	router.GET(`/shares/:share`, self.handleGetShare)
	router.GET(`/shares/:share/query/*query`, self.handleQueryShare)
	router.GET(`/shares/:share/browse/*parent`, self.handleBrowseShare)

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

func (self *API) qsBool(req *http.Request, key string) bool {
	if v := req.URL.Query().Get(key); v == `true` {
		return true
	}

	return false
}

func (self *API) qs(req *http.Request, key string) string {
	return req.URL.Query().Get(key)
}

func (self *API) getSearchParams(req *http.Request) (int, int, []string, error) {
	limit := 0
	offset := 0
	var sort []string

	if i, err := self.qsInt(req, `limit`); err == nil {
		if i > 0 {
			limit = int(i)
		} else {
			limit = DefaultResultLimit
		}
	} else {
		return 0, 0, nil, err
	}

	if i, err := self.qsInt(req, `offset`); err == nil {
		offset = int(i)
	} else {
		return 0, 0, nil, err
	}

	if v := req.URL.Query().Get(`sort`); v != `` {
		sort = strings.Split(v, `,`)
	}

	return limit, offset, sort, nil
}
