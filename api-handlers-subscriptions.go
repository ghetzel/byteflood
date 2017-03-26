package byteflood

import (
	"fmt"
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
	var subscription Subscription

	if err := self.db.Subscriptions.Get(vestigo.Param(req, `id`), &subscription); err == nil {
		Respond(w, subscription)
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

func (self *API) handleActionSubscription(w http.ResponseWriter, req *http.Request) {
	var subscription Subscription

	if err := self.db.Subscriptions.Get(vestigo.Param(req, `id`), &subscription); err == nil {
		action := vestigo.Param(req, `action`)

		switch action {
		case `sync`:
			if err := subscription.Sync(self.application); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		default:
			http.Error(w, fmt.Sprintf("Unknown action '%s'", action), http.StatusBadRequest)
			return
		}

		http.Error(w, ``, http.StatusNoContent)
	} else {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}
