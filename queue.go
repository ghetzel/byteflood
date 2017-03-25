package byteflood

import (
	"encoding/json"
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/ghetzel/pivot/dal"
	"io"
	"time"
)

var EmptyPollInterval = 3 * time.Second

type DownloadQueue struct {
	CurrentDownload *QueuedDownload `json:"current_download"`
	app             *Application
}

func NewDownloadQueue(app *Application) *DownloadQueue {
	return &DownloadQueue{
		app: app,
	}
}

// Appends a file to the download queue.
//
func (self *DownloadQueue) Add(sessionID string, shareID string, entryID string) error {
	// get peer
	if remotePeer, ok := self.app.LocalPeer.GetSession(sessionID); ok {
		// get file record from peer
		if response, err := remotePeer.ServiceRequest(`GET`, fmt.Sprintf("/shares/%s/view/%s", shareID, entryID), nil, nil); err == nil {
			entry := db.NewEntry(self.app.Database, ``, ``, ``)

			// parse and load record
			if err := json.NewDecoder(response.Body).Decode(entry); err == nil {
				if entry.IsDirectory {
					return self.enqueueDirectory(remotePeer, shareID, entryID)
				} else {
					return self.enqueueFile(remotePeer, shareID, entry)
				}
			} else {
				return err
			}
		} else {
			return err
		}
	} else {
		return fmt.Errorf("session %s not found", sessionID)
	}
}

func (self *DownloadQueue) enqueueDirectory(remotePeer *peer.RemotePeer, shareID string, directoryID string) error {
	// get file record from peer
	if response, err := remotePeer.ServiceRequest(`GET`, fmt.Sprintf("/shares/%s/browse/%s", shareID, directoryID), nil, nil); err == nil {
		recordset := dal.NewRecordSet()

		// parse and load record
		if err := json.NewDecoder(response.Body).Decode(recordset); err == nil {
			for _, record := range recordset.Records {
				if err := self.Add(remotePeer.SessionID(), shareID, fmt.Sprintf("%v", record.ID)); err != nil {
					log.Errorf("Failed to enqueue %s: %v", directoryID, err)
				}
			}

			return nil
		} else {
			return err
		}
	} else {
		return err
	}
}

func (self *DownloadQueue) enqueueFile(remotePeer *peer.RemotePeer, shareID string, entry *db.Entry) error {
	now := time.Now()

	var size uint64

	if entry.RelativePath == `` {
		return fmt.Errorf("File record does not contain a 'name' field")
	}

	if v := entry.Get(`file.size`); v != nil {
		if vv, err := stringutil.ConvertToInteger(v); err == nil {
			size = uint64(vv)
		} else {
			return err
		}
	} else {
		return fmt.Errorf("File record does not contain a 'file.size' field")
	}

	if download, ok := db.DownloadsSchema.NewInstance().(*QueuedDownload); ok {
		download.Status = `idle`
		download.SessionID = remotePeer.SessionID()
		download.PeerName = remotePeer.Name
		download.ShareID = shareID
		download.FileID = entry.ID
		download.Priority = now.UnixNano()
		download.Name = entry.RelativePath
		download.DestinationPath = fmt.Sprintf("/tmp/%s", remotePeer.ID)
		download.Size = size
		download.AddedAt = now

		if err := self.app.Database.Downloads.Create(download); err == nil {
			return err
		}
	} else {
		return fmt.Errorf("Failed to create new download instance")
	}

	return nil
}

// Downloads the given file ID from a named peer or session ID.  This function will block waiting
// for the download to finish.  The QueuedDownload that is returned is an io.Reader referencing the
// downloaded data.
//
func (self *DownloadQueue) Download(w io.Writer, sessionID string, fileID string) (*QueuedDownload, error) {
	if download, ok := db.DownloadsSchema.NewInstance().(*QueuedDownload); ok {
		download.Status = `idle`
		download.SessionID = sessionID
		download.FileID = fileID
		download.AddedAt = time.Now()
		download.app = self.app

		return download, download.Download(w)
	} else {
		return nil, fmt.Errorf("Failed to create new download instance")
	}
}

// Downloads all files in the queue.  If no files are currently in the queue,
// this function will poll the queue on the interval defined in EmptyPollInterval.
//
// Completed items will be moved to the CompletedItems slice.
//
func (self *DownloadQueue) DownloadAll() {
	for {
		if item := self.CurrentItem(); item != nil {
			self.CurrentDownload = item

			if err := item.Download(); err != nil {
				item.Error = err.Error()
				item.Status = `failed`
			}

			if err := self.app.Database.Downloads.Update(item); err != nil {
				log.Warningf("Failed to update queue item %s: %v", item.ID, err)
			}
		} else {
			self.CurrentDownload = nil
			time.Sleep(EmptyPollInterval)
		}
	}
}

func (self *DownloadQueue) CurrentItem() *QueuedDownload {
	if f, err := db.ParseFilter("status=idle"); err == nil {
		f.Sort = []string{`-priority`}
		f.Limit = 1

		var downloads []*QueuedDownload

		if err := self.app.Database.Downloads.Find(f, &downloads); err == nil && len(downloads) == 1 {
			download := downloads[0]
			download.app = self.app

			return download
		}
	} else {

	}

	return nil
}
