package byteflood

import (
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/shares"
	"github.com/ghetzel/go-stockutil/stringutil"
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

func writeTsvFileLine(w io.Writer, share *shares.Share, file *db.File, fields []string) {
	var fType string
	var fieldset string
	values := make([]string, len(fields))

	if file.IsDirectory {
		fType = `directory`
	} else {
		fType = `file`
	}

	for i, fieldName := range fields {
		if v := file.Get(fieldName); v != nil {
			if vS, err := stringutil.ToString(v); err == nil {
				values[i] = vS
			}
		}
	}

	if len(fields) > 0 {
		fieldset = "\t" + strings.Join(values, "\t")
	}

	fmt.Fprintf(w, "%d\t%s\t%s\t%s%v\n", share.ID, file.ID, fType, file.RelativePath, fieldset)
}

func (self *API) handleGetShares(w http.ResponseWriter, req *http.Request) {
	var shares []shares.Share

	if err := db.Shares.All(&shares); err == nil {
		Respond(w, shares)
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (self *API) handleGetShare(w http.ResponseWriter, req *http.Request) {
	share := shares.NewShare()

	if err := db.Shares.Get(vestigo.Param(req, `id`), share); err == nil {
		Respond(w, share)
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func (self *API) handleGetShareFile(w http.ResponseWriter, req *http.Request) {
	share := shares.NewShare()

	if err := db.Shares.Get(vestigo.Param(req, `id`), share); err == nil {
		if file, err := share.Get(vestigo.Param(req, `file`)); err == nil {
			Respond(w, file)
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func (self *API) handleShareSynclist(w http.ResponseWriter, req *http.Request) {
	share := shares.NewShare()

	if err := db.Shares.Get(vestigo.Param(req, `id`), share); err == nil {
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

		fieldset := req.URL.Query().Get(`fields`)
		fields := strings.Split(fieldset, `,`)
		filterString := req.URL.Query().Get(`q`)

		w.Header().Set(`Content-Type`, `text/plain`)

		if len(fieldset) > 0 {
			fieldset = "\t" + strings.Join(fields, "\t")
		}

		fmt.Fprintf(w, "share_id\tid\ttype\tpath%s\n", fieldset)

		for _, file := range files {
			if err := file.Walk(func(path string, file *db.File, err error) error {
				if err == nil {
					writeTsvFileLine(w, share, file, fields)
					return nil
				} else {
					return err
				}
			}, filterString); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func (self *API) handleGetShareFileIdsToRoot(w http.ResponseWriter, req *http.Request) {
	share := shares.NewShare()

	if err := db.Shares.Get(vestigo.Param(req, `id`), share); err == nil {
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
	share := shares.NewShare()

	if err := db.Shares.Get(vestigo.Param(req, `id`), share); err == nil {
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
	share := shares.NewShare()

	if err := db.Shares.Get(vestigo.Param(req, `id`), share); err == nil {
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
