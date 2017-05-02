package byteflood

import (
	"encoding/json"
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/byteflood/shares"
	"github.com/ghetzel/go-stockutil/httputil"
	"github.com/ghetzel/go-stockutil/typeutil"
	"github.com/ghetzel/pivot/dal"
	"github.com/husobee/vestigo"
	"github.com/microcosm-cc/bluemonday"
	"github.com/russross/blackfriday"
	"github.com/satori/go.uuid"
	"io"
	"math/rand"
	"net/http"
	"path"
	"strings"
)

type EntryParent struct {
	ID     string `json:"id"`
	Parent string `json:"parent"`
	Name   string `json:"name"`
	Last   bool   `json:"last,omitempty"`
}

func writeTsvFileLine(w io.Writer, share *shares.Share, item db.ManifestItem) {
	values := make([]string, len(item.Values))
	var fieldset string

	for i, value := range item.Values {
		if value != nil {
			values[i] = fmt.Sprintf("%v", value)
		}
	}

	if len(values) > 0 {
		fieldset = "\t" + strings.Join(values, "\t")
	}

	fmt.Fprintf(w, "%s\t%s\t%s%v\n", item.ID, item.RelativePath, item.Type, fieldset)
}

func (self *API) handleSaveShare(w http.ResponseWriter, req *http.Request) {
	var recordset dal.RecordSet

	if err := json.NewDecoder(req.Body).Decode(&recordset); err == nil {
		for _, record := range recordset.Records {
			var err error

			// if a scanned directory path was given, and if it doesn't already exist, create it
			if v := record.Get(`scanned_directory_path`, nil); !typeutil.IsEmpty(v) {
				scanPath := fmt.Sprintf("%v", v)

				if existing := db.Instance.GetDirectoriesByFile(scanPath); len(existing) == 0 {
					// create the directory
					autolabel := fmt.Sprintf("%v_%x", path.Base(scanPath), rand.Int31())

					if err := db.ScannedDirectories.Create(&db.Directory{
						ID:   autolabel,
						Path: scanPath,
					}); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}

					record.Set(`filter`, fmt.Sprintf("label=%v", autolabel))
				} else {
					record.Set(`filter`, fmt.Sprintf("label=%v", existing[0].ID))
				}

			}

			if req.Method == `POST` {
				err = db.Shares.CreateOrUpdate(record.ID, record)
			} else {
				err = db.Shares.Update(record)
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
}

func (self *API) handleGetShares(w http.ResponseWriter, req *http.Request, client peer.Peer) {
	if s, err := shares.GetShares(client, httputil.QBool(req, `stats`)); err == nil {
		Respond(w, s)
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (self *API) handleGetShare(w http.ResponseWriter, req *http.Request, client peer.Peer) {
	if s, err := shares.GetShares(client, httputil.QBool(req, `stats`), vestigo.Param(req, `id`)); err == nil {
		Respond(w, s[0])
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func (self *API) handleGetShareStats(w http.ResponseWriter, req *http.Request, client peer.Peer) {
	if s, err := shares.GetShares(client, false, vestigo.Param(req, `id`)); err == nil {
		share := s[0]

		if stats, err := share.GetStats(); err == nil {
			Respond(w, stats)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func (self *API) handleGetShareEntry(w http.ResponseWriter, req *http.Request, client peer.Peer) {
	if s, err := shares.GetShares(client, false, vestigo.Param(req, `id`)); err == nil {
		share := s[0]

		if entry, err := share.Get(vestigo.Param(req, `entry`)); err == nil {
			Respond(w, entry)
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func (self *API) handleShareManifest(w http.ResponseWriter, req *http.Request, client peer.Peer) {
	if s, err := shares.GetShares(client, false, vestigo.Param(req, `id`)); err == nil {
		share := s[0]

		entryId := vestigo.Param(req, `entry`)
		var entries []*db.Entry

		if entryId == `` {
			if f, err := share.Children(); err == nil {
				entries = f
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			if entry, err := share.Get(vestigo.Param(req, `entry`)); err == nil {
				entries = append(entries, entry)
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		var fieldset string

		if qs := req.URL.Query().Get(`fields`); qs != `` {
			fieldset = qs
		} else if hdr := req.Header.Get(`X-Byteflood-Manifest-Fields`); hdr != `` {
			fieldset = hdr
		}

		fields := strings.Split(fieldset, `,`)
		filterString := req.URL.Query().Get(`q`)

		w.Header().Set(`Content-Type`, `text/plain; charset=utf-8`)

		if len(fieldset) > 0 {
			fieldset = "\t" + strings.Join(fields, "\t")
		}

		fmt.Fprintf(w, strings.Join(db.DefaultManifestFields, "\t")+"%s\n", fieldset)

		for _, entry := range entries {
			if manifest, err := entry.GetManifest(fields, filterString); err == nil {
				for _, item := range manifest.Items {
					writeTsvFileLine(w, &share, item)
				}
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func (self *API) handleGetShareFileIdsToRoot(w http.ResponseWriter, req *http.Request, client peer.Peer) {
	if s, err := shares.GetShares(client, false, vestigo.Param(req, `id`)); err == nil {
		share := s[0]

		parents := make([]EntryParent, 0)
		current := vestigo.Param(req, `file`)
		last := true

		for {
			if current == `` || current == `root` {
				break
			}

			if file, err := share.Get(current); err == nil {
				parents = append([]EntryParent{
					{
						ID:     current,
						Parent: file.Parent,
						Name:   fmt.Sprintf("%v", file.Get(`file.name`, path.Base(file.RelativePath))),
						Last:   last,
					},
				}, parents...)

				current = file.Parent
				last = false
			} else {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}

		Respond(w, parents)
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func (self *API) handleQueryAllShares(w http.ResponseWriter, req *http.Request, client peer.Peer) {
	if s, err := shares.GetShares(client, httputil.QBool(req, `stats`)); err == nil {
		resultsets := make(map[string]interface{})

		for _, share := range s {
			if results, err := self.getResultsetFromShare(&share, req); err == nil {
				resultsets[share.ID] = results
			} else {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}

		Respond(w, resultsets)
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (self *API) handleQueryShare(w http.ResponseWriter, req *http.Request, client peer.Peer) {
	if s, err := shares.GetShares(client, false, vestigo.Param(req, `id`)); err == nil {
		if results, err := self.getResultsetFromShare(&s[0], req); err == nil {
			Respond(w, results)
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func (self *API) handleBrowseShare(w http.ResponseWriter, req *http.Request, client peer.Peer) {
	if s, err := shares.GetShares(client, false, vestigo.Param(req, `id`)); err == nil {
		share := s[0]

		if limit, offset, sort, err := getSearchParams(req); err == nil {
			var fields []string
			query := ``

			if parent := vestigo.Param(req, `parent`); parent == `` {
				query = fmt.Sprintf("parent=%s", db.RootDirectoryName)
			} else {
				query = fmt.Sprintf("parent=%s", strings.TrimPrefix(parent, `/`))
			}

			if v := req.URL.Query().Get(`fields`); v != `` {
				fields = strings.Split(v, `,`)
			}

			if results, err := share.Find(query, limit, offset, sort, fields); err == nil {
				Respond(w, results)
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func (self *API) handleShareLandingPage(w http.ResponseWriter, req *http.Request, client peer.Peer) {
	if s, err := shares.GetShares(client, false, vestigo.Param(req, `id`)); err == nil {
		share := s[0]

		if share.LongDescription != `` {
			output := blackfriday.MarkdownCommon([]byte(share.LongDescription[:]))
			output = bluemonday.UGCPolicy().SanitizeBytes(output)

			w.Header().Set(`Content-Type`, `text/html; charset=utf-8`)
			w.Write(output)
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
	} else {
		http.Error(w, ``, http.StatusNotFound)
	}
}

func (self *API) handleRequestEntryFromShare(w http.ResponseWriter, req *http.Request, client peer.Peer) {
	// perform the share authorization check on remote peers
	if !client.IsLocal() {
		if _, err := shares.GetShares(client, false, vestigo.Param(req, `share`)); err != nil {
			http.Error(w, ``, http.StatusNotFound)
			return
		}
	}

	// get remote peer from proxied request
	if remotePeer, ok := self.application.LocalPeer.GetSession(client.SessionID()); ok {
		var entry db.Entry

		if err := db.Metadata.Get(vestigo.Param(req, `entry`), &entry); err == nil {
			// get the absolute filesystem path to the entry at :id
			if absPath, err := entry.GetAbsolutePath(); err == nil {
				if strings.HasPrefix(path.Base(absPath), QueueTempFileFormat) {
					http.Error(w, `refusing to transfer temporary file`, http.StatusForbidden)
					return
				}

				// parse the given :transfer UUID
				if transferId, err := uuid.FromString(vestigo.Param(req, `transfer`)); err == nil {
					// start the transfer and return
					if err := remotePeer.TransferFile(transferId, absPath); err == nil {
						http.Error(w, ``, http.StatusNoContent)
					} else {
						http.Error(w, err.Error(), http.StatusInternalServerError)
					}
				} else {
					http.Error(w, err.Error(), http.StatusBadRequest)
				}
			} else {
				http.Error(w, err.Error(), http.StatusNotFound)
			}
		} else {
			http.Error(w, err.Error(), http.StatusNotFound)
		}
	} else {
		http.Error(w, `unknown session`, http.StatusForbidden)
	}
}

func (self *API) getResultsetFromShare(share *shares.Share, req *http.Request) (*dal.RecordSet, error) {
	if limit, offset, sort, err := getSearchParams(req); err == nil {
		var fields []string

		if v := req.URL.Query().Get(`fields`); v != `` {
			fields = strings.Split(v, `,`)
		}

		if results, err := share.Find(vestigo.Param(req, `_name`), limit, offset, sort, fields); err == nil {
			return results, nil
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}
