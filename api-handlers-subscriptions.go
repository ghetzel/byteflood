package byteflood

import (
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/shares"
	"github.com/husobee/vestigo"
	"net/http"
)

func (self *API) handleGetSubscriptions(w http.ResponseWriter, req *http.Request) {
	var subscriptions []*shares.Subscription

	if err := db.Subscriptions.All(&subscriptions); err == nil {
		Respond(w, subscriptions)
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (self *API) handleGetSubscription(w http.ResponseWriter, req *http.Request) {
	subscription := new(shares.Subscription)

	if err := db.Subscriptions.Get(vestigo.Param(req, `id`), subscription); err == nil {
		Respond(w, subscription)
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}
