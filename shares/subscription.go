package shares

type Subscription struct {
	PeerName string `json:"peer_name"`
	PeerID   string `json:"peer_id,omitempty"`
}
