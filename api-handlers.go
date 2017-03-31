package byteflood

import (
	"encoding/json"
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/go-stockutil/pathutil"
	"github.com/ghetzel/pivot/dal"
	"github.com/ghetzel/pivot/mapper"
	"github.com/husobee/vestigo"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
)

// populated in API.Initialize()
var endpointModelMap = map[string]mapper.Mapper{}

var endpointInstanceMap = map[string]*dal.Collection{
	`directories`:   db.ScannedDirectoriesSchema,
	`downloads`:     db.DownloadsSchema,
	`peers`:         db.AuthorizedPeersSchema,
	`shares`:        db.SharesSchema,
	`subscriptions`: db.SubscriptionsSchema,
	`system`:        db.SystemSchema,
}

type ActionPeerConnect struct {
	ID      string `json:"id,omitempty"`
	Address string `json:"address,omitempty"`
}

type ActionPeerDisconnect struct {
	SessionID string `json:"session_id"`
}

func (self *API) handleStatus(w http.ResponseWriter, req *http.Request) {
	Respond(w, map[string]interface{}{
		`version`:       Version,
		`local_peer_id`: self.application.LocalPeer.ID(),
	})
}

func (self *API) handleGetConfig(w http.ResponseWriter, req *http.Request) {
	Respond(w, self.application)
}

func (self *API) handleGetNewModelInstance(w http.ResponseWriter, req *http.Request) {
	parts := strings.Split(req.URL.Path, `/`)

	if len(parts) >= 3 {
		modelName := parts[2]

		if schema, ok := endpointInstanceMap[modelName]; ok {
			Respond(w, schema.NewInstance())
		} else {
			http.Error(w, fmt.Sprintf("Unknown model '%s'", modelName), http.StatusNotFound)
		}
	} else {
		http.Error(w, `Not Found`, http.StatusNotFound)
	}
}

func (self *API) handleSaveModel(w http.ResponseWriter, req *http.Request) {
	parts := strings.Split(req.URL.Path, `/`)

	if len(parts) >= 3 {
		var recordset dal.RecordSet
		var model mapper.Mapper
		modelName := parts[2]

		if m, ok := endpointModelMap[modelName]; ok && m != nil {
			model = m
		} else {
			http.Error(w, fmt.Sprintf("Unknown model '%s'", modelName), http.StatusNotFound)
			return
		}

		if err := json.NewDecoder(req.Body).Decode(&recordset); err == nil {
			var err error

			for _, record := range recordset.Records {
				if req.Method == `POST` {
					err = model.CreateOrUpdate(record.ID, record)
				} else {
					err = model.Update(record)
				}

				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			}

			http.Error(w, ``, http.StatusNoContent)
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	} else {
		http.Error(w, ``, http.StatusNotFound)
	}
}

func (self *API) handleDeleteModel(w http.ResponseWriter, req *http.Request) {
	parts := strings.Split(req.URL.Path, `/`)

	if len(parts) >= 3 {
		var model mapper.Mapper

		modelName := parts[2]

		if m, ok := endpointModelMap[modelName]; ok && m != nil {
			model = m
		} else {
			http.Error(w, fmt.Sprintf("Unknown model '%s'", modelName), http.StatusNotFound)
			return
		}

		if model.Exists(vestigo.Param(req, `id`)) {
			if err := model.Delete(vestigo.Param(req, `id`)); err == nil {
				http.Error(w, ``, http.StatusNoContent)
			} else {
				http.Error(w, err.Error(), http.StatusBadRequest)
			}
		} else {
			http.Error(w, ``, http.StatusNotFound)
		}
	} else {
		http.Error(w, ``, http.StatusNotFound)
	}
}

func (self *API) handlePerformAction(w http.ResponseWriter, req *http.Request) {
	action := vestigo.Param(req, `action`)

	switch action {
	case `connect`:
		var payload ActionPeerConnect

		if err := json.NewDecoder(req.Body).Decode(&payload); err == nil {
			if payload.Address != `` {
				if _, err := self.application.LocalPeer.ConnectTo(payload.Address); err == nil {
					w.WriteHeader(http.StatusNoContent)
				} else {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}

			} else if payload.ID != `` {
				var peer peer.AuthorizedPeer

				if err := self.db.AuthorizedPeers.Get(payload.ID, &peer); err == nil {
					if addrs := peer.GetAddresses(); len(addrs) > 0 {
						// TODO: this sucks, does not handle multiple addresses
						if _, err := self.application.LocalPeer.ConnectTo(addrs[0]); err == nil {
							w.WriteHeader(http.StatusNoContent)
						} else {
							http.Error(w, err.Error(), http.StatusInternalServerError)
						}
					} else {
						http.Error(w, `No addresses associated with peer`, http.StatusBadRequest)
					}
				} else {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
			} else {
				http.Error(w, `Must specify either "id" or "address" field.`, http.StatusBadRequest)
			}
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}

	case `disconnect`:
		var payload ActionPeerDisconnect

		if err := json.NewDecoder(req.Body).Decode(&payload); err == nil {
			if payload.SessionID != `` {
				if err := self.application.LocalPeer.RemovePeer(payload.SessionID); err == nil {
					w.WriteHeader(http.StatusNoContent)
				} else {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
			} else {
				http.Error(w, `Must specify "session_id" field.`, http.StatusBadRequest)
			}
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}

	default:
		http.Error(w, ``, http.StatusNotFound)
	}
}

// Upgrades an image stream connection request and attaches the connection to the requesting client
//
func (self *API) wsEventStream(w http.ResponseWriter, req *http.Request) {
	if conn, err := self.eventUpgrader.Upgrade(w, req, nil); err == nil {
		id := fmt.Sprintf("%v", conn.RemoteAddr())

		conn.SetCloseHandler(func(code int, text string) error {
			self.eventStreams.Remove(id)
			return nil
		})

		self.eventStreams.Set(id, conn)
	} else {
		log.Errorf("Error setting up WebSocket: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (self *API) handleBrowseLocalDirectories(w http.ResponseWriter, req *http.Request) {
	query := vestigo.Param(req, `_name`)
	query = strings.TrimPrefix(query, `/`)
	query = strings.TrimPrefix(query, `browse`)

	if !strings.HasPrefix(query, `~`) {
		query = `/` + strings.TrimPrefix(query, `/`)
	} else if query == `~` {
		query = query + `/`
	}

	paths := make([]string, 0)

	if v, err := pathutil.ExpandUser(query); err == nil {
		givenRoot, _ := path.Split(query)
		browseRoot, dirFilter := path.Split(v)

		if root, err := os.Open(browseRoot); err == nil {
			if rootStat, err := root.Stat(); err == nil {
				if rootStat.IsDir() {
					if entries, err := root.Readdir(-1); err == nil {
						for _, entry := range entries {
							name := entry.Name()

							if entry.IsDir() {
								if !strings.HasPrefix(name, `.`) {
									if strings.HasPrefix(name, dirFilter) {
										paths = append(paths, path.Join(givenRoot, entry.Name()))
									}
								}
							}
						}

						sort.Strings(paths)
						Respond(w, paths)
					} else {
						http.Error(w, err.Error(), http.StatusInternalServerError)
					}
				} else {
					Respond(w, paths)
				}
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		} else if os.IsNotExist(err) {
			Respond(w, paths)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}
