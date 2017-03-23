package shares

import (
	"time"
)

type WantedItem struct {
	SessionID string
	ShareName string
	EntityID  string
}

type Subscription struct {
	ID              string    `json:"id"`
	ShareName       string    `json:"share_name"`
	SourceGroup     string    `json:"source_group"`
	TargetPath      string    `json:"target_path"`
	Filter          string    `json:"filter,omitempty"`
	BytesDownloaded uint64    `json:"bytes_downloaded"`
	Quota           uint64    `json:"quota,omitempty"`
	QuotaResetAt    time.Time `json:"quota_reset_at,omitempty"`
	QuotaInterval   uint64    `json:"quota_interval,omitempty"`
}

func (self *Subscription) GetWantedItems() []*WantedItem {
	items := make([]*WantedItem, 0)

	// get manifests from everyone in the source group that we're connected to

	// for each manifest:
	//     set BaseDirectory to TargetPath
	//     for every file in manifest.GetUpdateManifest()
	// items = append(items, &WantedItem{
	//     SessionID: session_id_of_peer,
	//     ShareName: share_name_of_peer,
	//     EntityID:  item.ID,
	// })

	return items
}
