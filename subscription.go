package byteflood

import (
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/go-stockutil/sliceutil"
	"time"
)

type WantedItem struct {
	SessionID string
	ShareName string
	EntityID  string
}

type Subscription struct {
	ID              int       `json:"id"`
	ShareName       string    `json:"share_name"`
	SourceGroup     string    `json:"source_group"`
	TargetPath      string    `json:"target_path"`
	Filter          string    `json:"filter,omitempty"`
	BytesDownloaded uint64    `json:"bytes_downloaded"`
	Quota           uint64    `json:"quota,omitempty"`
	QuotaResetAt    time.Time `json:"quota_reset_at,omitempty"`
	QuotaInterval   uint64    `json:"quota_interval,omitempty"`
	db              *db.Database
}

func NewSubscription(id int, share string, source string, target string) *Subscription {
	return &Subscription{
		ID:          id,
		ShareName:   share,
		SourceGroup: source,
		TargetPath:  target,
	}
}

func (self *Subscription) GetWantedItems(application *Application) ([]*WantedItem, error) {
	if self.ShareName == `` {
		return nil, fmt.Errorf("A share name must be specified")
	}

	items := make([]*WantedItem, 0)
	requestedPaths := make([]string, 0)
	policy := db.ChecksumPolicy
	var peers []*peer.RemotePeer

	if self.SourceGroup == `` {
		peers = application.LocalPeer.GetPeers()
	} else {
		// get manifests from everyone in the source group that we're connected to
		if peersInGroup, err := application.LocalPeer.GetPeersInGroup(self.SourceGroup); err == nil {
			peers = peersInGroup
		} else {
			return nil, err
		}
	}

	for _, remotePeer := range peers {
		if manifest, err := remotePeer.GetManifest(self.ShareName, policy.Fields...); err == nil {
			manifest.BaseDirectory = self.TargetPath

			// get the list of files the remote peer has that we want
			if updates, err := manifest.GetUpdateManifest(policy); err == nil {
				for _, item := range updates.Items {
					if !sliceutil.ContainsString(requestedPaths, item.RelativePath) {
						log.Debugf("Want %s:%s from peer %v", self.ShareName, item.RelativePath, remotePeer)

						items = append(items, &WantedItem{
							SessionID: remotePeer.SessionID(),
							ShareName: self.ShareName,
							EntityID:  item.ID,
						})

						requestedPaths = append(requestedPaths, item.RelativePath)
					}
				}
			} else {
				return nil, err
			}
		} else {
			log.Errorf("Failed to retrieve manifest from %v: %v", remotePeer, err)
		}
	}

	return items, nil
}

func (self *Subscription) SetDatabase(conn *db.Database) {
	self.db = conn
}
