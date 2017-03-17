package byteflood

import (
	"encoding/json"
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/byteflood/shares"
	"github.com/ghetzel/pivot/dal"
	"github.com/ghetzel/pivot/mapper"
	"github.com/husobee/vestigo"
	"net/http"
	"reflect"
	"strings"
)

// populated in API.Initialize()
var endpointModelMap = map[string]mapper.Mapper{}

var endpointInstanceMap = map[string]reflect.Type{
	`shares`:      reflect.TypeOf(shares.Share{}),
	`peers`:       reflect.TypeOf(peer.RemotePeer{}),
	`directories`: reflect.TypeOf(db.Directory{}),
	`downloads`:   reflect.TypeOf(QueuedDownload{}),
}

func (self *API) handleStatus(w http.ResponseWriter, req *http.Request) {
	Respond(w, map[string]interface{}{
		`version`: Version,
	})
}

func (self *API) handleGetConfig(w http.ResponseWriter, req *http.Request) {
	Respond(w, self.application)
}

func (self *API) handleGetNewModelInstance(w http.ResponseWriter, req *http.Request) {
	parts := strings.Split(req.URL.Path, `/`)

	if len(parts) >= 3 {
		modelName := parts[2]

		if typeOf, ok := endpointInstanceMap[modelName]; ok {
			Respond(w, reflect.New(typeOf).Interface())
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

		if err := model.Delete(vestigo.Param(req, `id`)); err == nil {
			http.Error(w, ``, http.StatusNoContent)
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	} else {
		http.Error(w, ``, http.StatusNotFound)
	}
}
