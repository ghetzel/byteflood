package byteflood

//go:generate esc -o static.go -pkg byteflood -prefix ui ui

import (
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/diecast"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/gorilla/websocket"
	// "github.com/gregjones/httpcache"
	"github.com/husobee/vestigo"
	"github.com/orcaman/concurrent-map"
	"github.com/urfave/negroni"
	"net/http"
	"path"
	"strings"
	"time"
)

type API struct {
	Address       string `json:"address,omitempty"`
	UiDirectory   string `json:"ui_directory,omitempty"`
	application   *Application
	db            *db.Database
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
		db:           application.Database,
		eventStreams: cmap.New(),
		events:       make(chan *Event),
	}
}

func (self *API) Initialize() error {
	endpointModelMap[`directories`] = self.db.ScannedDirectories
	endpointModelMap[`downloads`] = self.db.Downloads
	endpointModelMap[`peers`] = self.db.AuthorizedPeers
	endpointModelMap[`shares`] = self.db.Shares
	endpointModelMap[`subscriptions`] = self.db.Subscriptions
	endpointModelMap[`properties`] = self.db.System

	self.eventUpgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	go self.startEventDispatcher()

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

	// handle serving UI from an embedded FileSystem
	if self.UiDirectory == `embedded` {
		ui.SetFileSystem(FS(false))
	}

	// provide a cache-aware http.Client to diecast
	// diecast.BindingClient = &http.Client{
	// 	Transport: httpcache.NewMemoryCacheTransport(),
	// }

	if err := ui.Initialize(); err != nil {
		return err
	}

	router.Get(`/api/status`, self.handleStatus)
	router.Get(`/api/configuration`, self.handleGetConfig)
	router.Get(`/api/events`, self.wsEventStream)
	router.Get(`/api/browse/*`, self.handleBrowseLocalDirectories)

	router.Get(`/api/properties`, self.handleGetSystemProperties)
	router.Get(`/api/properties/:id`, self.handleGetSystemProperty)
	router.Post(`/api/properties`, self.handleSaveModel)
	router.Put(`/api/properties`, self.handleSaveModel)
	router.Delete(`/api/properties/:id`, self.handleDeleteModel)
	router.Get(`/api/properties/new`, self.handleGetNewModelInstance)

	// download queue endpoints
	router.Get(`/api/downloads`, self.handleGetQueue)
	router.Get(`/api/downloads/history`, self.handleGetQueuedDownloads)
	router.Post(`/api/downloads/actions/:action`, self.handleActionQueue)
	router.Post(`/api/downloads/:peer/:share/:entry`, self.handleEnqueueEntry)

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
	router.Post(`/api/directories/ignorelist`, self.handleScannedDirectoryTestIgnoreList)
	router.Post(`/api/directories`, self.handleSaveModel)
	router.Put(`/api/directories`, self.handleSaveModel)
	router.Delete(`/api/directories/:id`, self.handleDeleteModel)
	router.Get(`/api/directories/new`, self.handleGetNewModelInstance)
	router.Add(http.MethodHead, `/api/files/:file`, self.handleDownloadFile)
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
	router.Add(http.MethodHead, `/api/sessions/:session/:share/files/:file`, self.handleDownloadFile)
	router.Get(`/api/sessions/:session/:share/files/:file`, self.handleDownloadFile)

	for _, method := range []string{`GET`, `POST`, `PUT`, `DELETE`, `HEAD`} {
		router.Add(method, `/api/sessions/:session/proxy/*`, self.handleProxyToSession)
	}

	// share management endpoints
	router.Get(`/api/shares`, self.wrapHandlerWithLocalPeer(self.handleGetShares))
	router.Post(`/api/shares`, self.handleSaveModel)
	router.Put(`/api/shares`, self.handleSaveModel)
	router.Delete(`/api/shares/:id`, self.handleDeleteModel)
	router.Get(`/api/shares/new`, self.handleGetNewModelInstance)

	router.Get(`/api/shares/:id`, self.wrapHandlerWithLocalPeer(self.handleGetShare))
	router.Get(`/api/shares/:id/landing`, self.wrapHandlerWithLocalPeer(self.handleShareLandingPage))
	router.Get(`/api/shares/:id/stats`, self.wrapHandlerWithLocalPeer(self.handleGetShareStats))
	router.Get(`/api/shares/:id/view/:entry`, self.wrapHandlerWithLocalPeer(self.handleGetShareEntry))
	router.Get(`/api/shares/:id/parents/:file`, self.wrapHandlerWithLocalPeer(self.handleGetShareFileIdsToRoot))
	router.Get(`/api/shares/:id/manifest`, self.wrapHandlerWithLocalPeer(self.handleShareManifest))
	router.Get(`/api/shares/:id/manifest/:file`, self.wrapHandlerWithLocalPeer(self.handleShareManifest))
	router.Get(`/api/shares/:id/query/*`, self.wrapHandlerWithLocalPeer(self.handleQueryShare))
	router.Get(`/api/shares/:id/browse/`, self.wrapHandlerWithLocalPeer(self.handleBrowseShare))
	router.Get(`/api/shares/:id/browse/:parent`, self.wrapHandlerWithLocalPeer(self.handleBrowseShare))

	// subscription management endpoints
	router.Get(`/api/subscriptions`, self.handleGetSubscriptions)
	router.Post(`/api/subscriptions`, self.handleSaveModel)
	router.Put(`/api/subscriptions`, self.handleSaveModel)
	router.Get(`/api/subscriptions/new`, self.handleGetNewModelInstance)
	router.Get(`/api/subscriptions/:id`, self.handleGetSubscription)
	router.Delete(`/api/subscriptions/:id`, self.handleDeleteModel)
	router.Post(`/api/subscriptions/:id/actions/:action`, self.handleActionSubscription)

	mux.Handle(`/api/`, router)
	mux.Handle(`/`, ui)

	server.UseHandler(mux)
	reqlog := NewRequestLogger()
	reqlog.Methods = []string{`-get`}
	server.Use(reqlog)

	log.Debugf("Running API server at %s", self.Address)
	server.Run(self.Address)
	return nil
}

// This returns an http.Handler that will respond to HTTP requests from remote peers.
func (self *API) GetPeerRequestHandler() http.Handler {
	router := vestigo.NewRouter()

	router.Get(`/`, self.handleGetSessionStatus)

	// TODO: this needs to go through share to enforce authz
	router.Post(`/transfers/:transfer/:share/:entry`, self.wrapHandlerWithRemotePeer(self.handleRequestEntryFromShare))
	router.Get(`/shares`, self.wrapHandlerWithRemotePeer(self.handleGetShares))
	router.Get(`/shares/:id`, self.wrapHandlerWithRemotePeer(self.handleGetShare))
	router.Get(`/shares/:id/landing`, self.wrapHandlerWithRemotePeer(self.handleShareLandingPage))
	router.Get(`/shares/:id/stats`, self.wrapHandlerWithRemotePeer(self.handleGetShareStats))
	router.Get(`/shares/:id/view/:entry`, self.wrapHandlerWithRemotePeer(self.handleGetShareEntry))
	router.Get(`/shares/:id/parents/:file`, self.wrapHandlerWithRemotePeer(self.handleGetShareFileIdsToRoot))
	router.Get(`/shares/:id/manifest`, self.wrapHandlerWithRemotePeer(self.handleShareManifest))
	router.Get(`/shares/:id/manifest/:file`, self.wrapHandlerWithRemotePeer(self.handleShareManifest))
	router.Get(`/shares/:id/query/*`, self.wrapHandlerWithRemotePeer(self.handleQueryShare))
	router.Get(`/shares/:id/browse/`, self.wrapHandlerWithRemotePeer(self.handleBrowseShare))
	router.Get(`/shares/:id/browse/:parent`, self.wrapHandlerWithRemotePeer(self.handleBrowseShare))

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

func getSearchParams(req *http.Request) (int, int, []string, error) {
	limit := 0
	offset := 0
	var sort []string

	if i, err := qsInt(req, `limit`); err == nil {
		if i > 0 {
			limit = int(i)
		} else {
			limit = DefaultResultLimit
		}
	} else {
		return 0, 0, nil, err
	}

	if i, err := qsInt(req, `offset`); err == nil {
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

// Takes a Request, ResponseWriter, and db.Entry and writes out the appropriate HTTP response headers
// for the entry.  These include Content-Length and -Disposition, modification/cache control headers,
// and modification timestamps.  The function will return whether the actual content should be written to
// the client (true) or not.  If the function returns false, the calling handler should return immediately.
//
func writeHttpHeadersForEntry(w http.ResponseWriter, req *http.Request, entry *db.Entry) bool {
	ifNoneMatch := strings.Trim(
		strings.TrimPrefix(req.Header.Get(`If-None-Match`), `W/`),
		`"`,
	)

	if entry.Size > 0 {
		w.Header().Set(`Content-Length`, fmt.Sprintf("%d", entry.Size))
	}

	if v := w.Header().Get(`Content-Disposition`); v == `` {
		w.Header().Set(`Content-Disposition`, fmt.Sprintf(
			"attachment; filename=%q",
			path.Base(entry.RelativePath),
		))
	}

	// if the content-type hasn't already been set, use the mimetype associated with the given entry
	if v := w.Header().Get(`Content-Type`); v == `` {
		w.Header().Set(`Content-Type`, fmt.Sprintf("%v", entry.Get(`file.mime.type`, `application/octet-stream`)))
	}

	if entry.Checksum != `` {
		w.Header().Set(`ETag`, fmt.Sprintf("%q", entry.Checksum))

		// short circuit if the If-None-Match header was specified and the checksum
		// the client gave us matches this one
		if entry.Checksum == ifNoneMatch {
			w.WriteHeader(http.StatusNotModified)
			return false
		}
	}

	return true
}

func writeCacheHeaders(w http.ResponseWriter, maxAge int) {
	if maxAge < 0 {
		w.Header().Del(`Cache-Control`)
	} else {
		w.Header().Set(`Cache-Control`, fmt.Sprintf("max-age=%d", maxAge))
	}
}

func qsInt(req *http.Request, key string) (int64, error) {
	if v := req.URL.Query().Get(key); v != `` {
		if i, err := stringutil.ConvertToInteger(v); err == nil {
			return i, nil
		} else {
			return 0, fmt.Errorf("%s: %v", key, err)
		}
	}

	return 0, nil
}

func qsBool(req *http.Request, key string) bool {
	if v := req.URL.Query().Get(key); v == `true` {
		return true
	}

	return false
}

func qs(req *http.Request, key string) string {
	return req.URL.Query().Get(key)
}
