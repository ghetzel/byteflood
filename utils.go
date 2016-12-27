package byteflood

import (
	"encoding/json"
	"net/http"
)

const Description = `Encrypted file sharing and metadata management`
const Version = `0.1.0`

func Respond(w http.ResponseWriter, data interface{}) {
	w.Header().Set(`Content-Type`, `application/json`)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
