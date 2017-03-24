package byteflood

//go:generate esc -o static.go -pkg byteflood -prefix ui ui

import (
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/diecast"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/gorilla/websocket"
	"github.com/husobee/vestigo"
	"github.com/orcaman/concurrent-map"
	"github.com/urfave/negroni"
	"net/http"
	"strings"
	"time"
)

type API struct {
	Address       string `json:"address,omitempty"`
	UiDirectory   string `json:"ui_directory,omitempty"`
	application   *Application
	eventUpgrader websocket.Upgrader
	eventStreams  cmap.ConcurrentMap
	events        chan *Event
}

var DefaultApiAddress = `:11984`
var DefaultResultLimit = 25
var DefaultUiDirectory = `embedded`

func NewAPI(application *Application) *API {
	return &API{
		Address:      DefaultApiAddress,
		UiDirectory:  DefaultUiDirectory,
		application:  application,
		eventStreams: cmap.New(),
		events:       make(chan *Event),
	}
}

func (self *API) Initialize() error {
	endpointModelMap[`directories`] = db.ScannedDirectories
	endpointModelMap[`downloads`] = db.Downloads
	endpointModelMap[`peers`] = db.AuthorizedPeers
	endpointModelMap[`shares`] = db.Shares
	endpointModelMap[`subscriptions`] = db.Subscriptions

	self.eventUpgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	go self.startEventDispatcher()

	go func() {
		for _ = range time.Tick(5 * time.Second) {
			self.SendEvent(&Event{
				Type: HeartbeatEvent,
			})
		}
	}()

	return nil
}

func (self *API) Serve() error {
	uiDir := self.UiDirectory

	if self.UiDirectory == `embedded` {
		uiDir = `/`
	}

	server := negroni.New()
	router := vestigo.NewRouter()
	mux := http.NewServeMux()
	ui := diecast.NewServer(uiDir, `*.html`)

	if self.UiDirectory == `embedded` {
		ui.SetFileSystem(FS(false))
	}

	if err := ui.Initialize(); err != nil {
		return err
	}

	router.Get(`/api/status`, self.handleStatus)
	router.Get(`/api/configuration`, self.handleGetConfig)
	router.Get(`/api/events`, self.wsEventStream)

	// download queue endpoints
	router.Get(`/api/downloads`, self.handleGetQueue)
	router.Get(`/api/downloads/history`, self.handleGetQueuedDownloads)
	router.Post(`/api/downloads/:peer/:share/:file`, self.handleEnqueueFile)

	// metadata database endpoints
	router.Get(`/api/db`, self.handleGetDatabase)
	router.Get(`/api/db/:id`, self.handleGetDatabaseItem)
	router.Get(`/api/db/query/*`, self.handleQueryDatabase)
	router.Get(`/api/db/browse/`, self.handleBrowseDatabase)
	router.Get(`/api/db/browse/:parent`, self.handleBrowseDatabase)
	router.Get(`/api/db/list/*`, self.handleListValuesInDatabase)
	router.Post(`/api/db/actions/:action`, self.handleActionDatabase)

	// scanned directory management endpoints
	router.Get(`/api/directories`, self.handleGetScannedDirectories)
	router.Get(`/api/directories/:id`, self.handleGetScannedDirectory)
	router.Post(`/api/directories`, self.handleSaveModel)
	router.Put(`/api/directories`, self.handleSaveModel)
	router.Delete(`/api/directories/:id`, self.handleDeleteModel)
	router.Get(`/api/directories/new`, self.handleGetNewModelInstance)
	router.Get(`/api/files/:file`, self.handleDownloadFile)

	// actions
	router.Post(`/api/actions/:action`, self.handlePerformAction)

	// authorized peer management endpoints
	router.Get(`/api/peers`, self.handleGetPeers)
	router.Post(`/api/peers`, self.handleSaveModel)
	router.Put(`/api/peers`, self.handleSaveModel)
	router.Delete(`/api/peers/:id`, self.handleDeleteModel)
	router.Get(`/api/peers/new`, self.handleGetNewModelInstance)
	router.Get(`/api/peers/:id`, self.handleGetPeer)

	// active session management endpoints
	router.Get(`/api/sessions`, self.handleGetSessions)
	router.Get(`/api/sessions/:session`, self.handleGetSession)
	router.Get(`/api/sessions/:session/files/:file`, self.handleDownloadFile)

	for _, method := range []string{`GET`, `POST`, `PUT`, `DELETE`, `HEAD`} {
		router.Add(method, `/api/sessions/:session/proxy/*`, self.handleProxyToSession)
	}

	// share management endpoints
	router.Get(`/api/shares`, self.handleGetShares)
	router.Post(`/api/shares`, self.handleSaveModel)
	router.Put(`/api/shares`, self.handleSaveModel)
	router.Delete(`/api/shares/:id`, self.handleDeleteModel)
	router.Get(`/api/shares/new`, self.handleGetNewModelInstance)

	router.Get(`/api/shares/:id`, self.handleGetShare)
	router.Get(`/api/shares/:id/view/:file`, self.handleGetShareFile)
	router.Get(`/api/shares/:id/parents/:file`, self.handleGetShareFileIdsToRoot)
	router.Get(`/api/shares/:id/manifest`, self.handleShareManifest)
	router.Get(`/api/shares/:id/manifest/:file`, self.handleShareManifest)
	router.Get(`/api/shares/:id/query/*`, self.handleQueryShare)
	router.Get(`/api/shares/:id/browse/`, self.handleBrowseShare)
	router.Get(`/api/shares/:id/browse/:parent`, self.handleBrowseShare)

	// subscription management endpoints
	router.Get(`/api/subscriptions`, self.handleGetSubscriptions)
	router.Post(`/api/subscriptions`, self.handleSaveModel)
	router.Put(`/api/subscriptions`, self.handleSaveModel)
	router.Get(`/api/subscriptions/new`, self.handleGetNewModelInstance)
	router.Get(`/api/subscriptions/:id`, self.handleGetSubscription)
	router.Delete(`/api/subscriptions/:id`, self.handleDeleteModel)

	mux.Handle(`/api/`, router)
	mux.Handle(`/`, ui)

	server.UseHandler(mux)
	server.Use(NewRequestLogger())

	log.Debugf("Running API server at %s", self.Address)
	server.Run(self.Address)
	return nil
}

// This returns an http.Handler that will respond to HTTP requests from remote peers.
func (self *API) GetPeerRequestHandler() http.Handler {
	router := vestigo.NewRouter()

	router.Get(`/`, self.handleGetSessionStatus)
	router.Post(`/transfers/:transfer/:file`, self.handleRequestFileFromShare)
	router.Get(`/shares`, self.handleGetShares)
	router.Get(`/shares/:id`, self.handleGetShare)
	router.Get(`/shares/:id/view/:file`, self.handleGetShareFile)
	router.Get(`/shares/:id/parents/:file`, self.handleGetShareFileIdsToRoot)
	router.Get(`/shares/:id/manifest`, self.handleShareManifest)
	router.Get(`/shares/:id/manifest/:file`, self.handleShareManifest)
	router.Get(`/shares/:id/query/*`, self.handleQueryShare)
	router.Get(`/shares/:id/browse/`, self.handleBrowseShare)
	router.Get(`/shares/:id/browse/:parent`, self.handleBrowseShare)

	return router
}

func (self *API) SendEvent(event *Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	self.events <- event
}

func (self *API) SendMessage(message string) {
	self.SendEvent(&Event{
		Type:    MessageEvent,
		Payload: message,
	})
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

func (self *API) startEventDispatcher() {
	for event := range self.events {
		self.eventStreams.IterCb(func(key string, valueI interface{}) {
			if conn, ok := valueI.(*websocket.Conn); ok {
				conn.WriteJSON(event)
			}
		})
	}
}
