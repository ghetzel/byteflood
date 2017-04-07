package byteflood

import (
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/husobee/vestigo"
	"github.com/sabhiram/go-gitignore"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
)

func (self *API) handleGetScannedDirectories(w http.ResponseWriter, req *http.Request) {
	if dirs, err := db.GetScannedDirectories(self.db); err == nil {
		Respond(w, dirs)
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (self *API) handleGetScannedDirectory(w http.ResponseWriter, req *http.Request) {
	dir := new(db.Directory)

	if err := self.db.ScannedDirectories.Get(vestigo.Param(req, `id`), dir); err == nil {
		Respond(w, dir)
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func (self *API) handleScannedDirectoryTestIgnoreList(w http.ResponseWriter, req *http.Request) {
	if ignorelist, err := ioutil.ReadAll(req.Body); err == nil {
		if ig, err := ignore.CompileIgnoreLines(strings.Split(string(ignorelist[:]), "\n")...); err == nil {
			if v := qs(req, `path`); v != `` {
				if testpath, err := url.QueryUnescape(v); err == nil {
					if ig.MatchesPath(testpath) {
						w.WriteHeader(http.StatusNoContent)
					} else {
						http.Error(w, fmt.Sprintf("Path %q does not match the given ignorelist", testpath), http.StatusBadRequest)
					}
				} else {
					http.Error(w, err.Error(), http.StatusBadRequest)
				}
			} else {
				w.WriteHeader(http.StatusNoContent)
			}
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}
