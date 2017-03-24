package byteflood

import (
	"github.com/husobee/vestigo"
	"net/http"
)

func (self *API) handleGetSubscriptions(w http.ResponseWriter, req *http.Request) {
	var subscriptions []*Subscription

	if err := self.db.Subscriptions.All(&subscriptions); err == nil {
		Respond(w, subscriptions)
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (self *API) handleGetSubscription(w http.ResponseWriter, req *http.Request) {
	subscription := new(Subscription)

	if err := self.db.Subscriptions.Get(vestigo.Param(req, `id`), subscription); err == nil {
		Respond(w, subscription)
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}
