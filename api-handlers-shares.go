package byteflood

import (
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/shares"
	"github.com/husobee/vestigo"
	"io"
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
	share := shares.NewShare(self.db)

	if err := self.db.Shares.Get(vestigo.Param(req, `id`), share); err == nil {
		Respond(w, share)
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func (self *API) handleGetShareFile(w http.ResponseWriter, req *http.Request) {
	share := shares.NewShare(self.db)

	if err := self.db.Shares.Get(vestigo.Param(req, `id`), share); err == nil {
		if file, err := share.Get(vestigo.Param(req, `file`)); err == nil {
			Respond(w, file)
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func (self *API) handleShareManifest(w http.ResponseWriter, req *http.Request) {
	share := shares.NewShare(self.db)

	if err := self.db.Shares.Get(vestigo.Param(req, `id`), share); err == nil {
		fileId := vestigo.Param(req, `file`)
		var files []*db.File

		if fileId == `` {
			if f, err := share.Children(); err == nil {
				files = f
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			if file, err := share.Get(vestigo.Param(req, `file`)); err == nil {
				files = append(files, file)
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
		log.Debugf(strings.Join(db.DefaultManifestFields, "\t")+"%s\n", fieldset)

		for _, file := range files {
			if manifest, err := file.GetManifest(fields, filterString); err == nil {
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
	share := shares.NewShare(self.db)

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
	share := shares.NewShare(self.db)

	if err := self.db.Shares.Get(vestigo.Param(req, `id`), share); err == nil {
		if limit, offset, sort, err := self.getSearchParams(req); err == nil {
			if results, err := share.Find(vestigo.Param(req, `_name`), limit, offset, sort); err == nil {
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
	share := shares.NewShare(self.db)

	if err := self.db.Shares.Get(vestigo.Param(req, `id`), share); err == nil {
		if limit, offset, sort, err := self.getSearchParams(req); err == nil {
			query := ``

			if parent := vestigo.Param(req, `parent`); parent == `` {
				query = fmt.Sprintf("parent=%s", db.RootDirectoryName)
			} else {
				query = fmt.Sprintf("parent=%s", strings.TrimPrefix(parent, `/`))
			}

			if results, err := share.Find(query, limit, offset, sort); err == nil {
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
