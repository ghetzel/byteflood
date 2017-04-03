package byteflood

import (
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/shares"
	"github.com/husobee/vestigo"
	"github.com/microcosm-cc/bluemonday"
	"github.com/russross/blackfriday"
	"io"
	"net/http"
	"path"
	"strings"
	"time"
)

type shareStatsCacheItem struct {
	Stats     *shares.Stats
	Timestamp time.Time
}

var shareStatsCache = make(map[string]shareStatsCacheItem)
var shareStatsCacheTimeout = time.Duration(30) * time.Minute

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
	log.Debugf("%s\t%s\t%s%v\n", item.ID, item.RelativePath, item.Type, fieldset)
}

func (self *API) handleGetShares(w http.ResponseWriter, req *http.Request) {
	var shares []shares.Share

	if err := self.db.Shares.All(&shares); err == nil {
		Respond(w, shares)
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (self *API) handleGetShare(w http.ResponseWriter, req *http.Request) {
	share := db.SharesSchema.NewInstance().(*shares.Share)

	if err := self.db.Shares.Get(vestigo.Param(req, `id`), share); err == nil {
		Respond(w, share)
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func (self *API) handleGetShareStats(w http.ResponseWriter, req *http.Request) {
	share := db.SharesSchema.NewInstance().(*shares.Share)

	if err := self.db.Shares.Get(vestigo.Param(req, `id`), share); err == nil {
		var stats *shares.Stats

		if cacheItem, ok := shareStatsCache[share.ID]; ok {
			if time.Since(cacheItem.Timestamp) <= shareStatsCacheTimeout {
				stats = cacheItem.Stats
			} else {
				delete(shareStatsCache, share.ID)
			}
		}

		if stats == nil {
			if s, err := share.GetStats(); err == nil {
				stats = s

				shareStatsCache[share.ID] = shareStatsCacheItem{
					Stats:     s,
					Timestamp: time.Now(),
				}
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		Respond(w, stats)
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func (self *API) handleGetShareEntry(w http.ResponseWriter, req *http.Request) {
	share := db.SharesSchema.NewInstance().(*shares.Share)

	if err := self.db.Shares.Get(vestigo.Param(req, `id`), share); err == nil {
		if entry, err := share.Get(vestigo.Param(req, `entry`)); err == nil {
			Respond(w, entry)
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func (self *API) handleShareManifest(w http.ResponseWriter, req *http.Request) {
	share := db.SharesSchema.NewInstance().(*shares.Share)

	if err := self.db.Shares.Get(vestigo.Param(req, `id`), share); err == nil {
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
					writeTsvFileLine(w, share, item)
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

func (self *API) handleGetShareFileIdsToRoot(w http.ResponseWriter, req *http.Request) {
	share := db.SharesSchema.NewInstance().(*shares.Share)

	if err := self.db.Shares.Get(vestigo.Param(req, `id`), share); err == nil {
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

func (self *API) handleQueryShare(w http.ResponseWriter, req *http.Request) {
	share := db.SharesSchema.NewInstance().(*shares.Share)

	if err := self.db.Shares.Get(vestigo.Param(req, `id`), share); err == nil {
		if limit, offset, sort, err := self.getSearchParams(req); err == nil {
			var fields []string

			if v := req.URL.Query().Get(`fields`); v != `` {
				fields = strings.Split(v, `,`)
			}

			if results, err := share.Find(vestigo.Param(req, `_name`), limit, offset, sort, fields); err == nil {
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

func (self *API) handleBrowseShare(w http.ResponseWriter, req *http.Request) {
	share := db.SharesSchema.NewInstance().(*shares.Share)

	if err := self.db.Shares.Get(vestigo.Param(req, `id`), share); err == nil {
		if limit, offset, sort, err := self.getSearchParams(req); err == nil {
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

func (self *API) handleShareLandingPage(w http.ResponseWriter, req *http.Request) {
	share := db.SharesSchema.NewInstance().(*shares.Share)

	if err := self.db.Shares.Get(vestigo.Param(req, `id`), share); err == nil {
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
