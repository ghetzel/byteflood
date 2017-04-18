package byteflood

import (
	"encoding/json"
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/byteflood/stats"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/ghetzel/pivot/dal"
	"github.com/orcaman/concurrent-map"
	"io"
	"path/filepath"
	"sync"
	"time"
)

var EmptyPollInterval = 3 * time.Second
var ConcurrentDownloads = 3

type DownloadQueue struct {
	ExitOnEmpty      bool               `json:"exit_on_empty,omitempty"`
	ActiveDownloads  cmap.ConcurrentMap `json:"active_downloads"`
	app              *Application
	workerPool       chan *QueuedDownload
	waitForEmpty     chan bool
	customDownloader DownloadFunc
	waitForComplete  sync.WaitGroup
}

func NewDownloadQueue(app *Application) *DownloadQueue {
	return &DownloadQueue{
		ActiveDownloads: cmap.New(),
		app:             app,
		waitForEmpty:    make(chan bool),
		workerPool:      make(chan *QueuedDownload, ConcurrentDownloads),
	}
}

// Appends a file to the download queue.
//
func (self *DownloadQueue) Add(sessionID string, shareID string, entryID string, destination string) error {
	// get peer
	if remotePeer, ok := self.app.LocalPeer.GetSession(sessionID); ok {
		// get file record from peer
		if response, err := remotePeer.ServiceRequest(`GET`, fmt.Sprintf("/shares/%s/view/%s", shareID, entryID), nil, nil); err == nil {
			entry := db.NewEntry(self.app.Database, ``, ``, ``)

			// parse and load record
			if err := json.NewDecoder(response.Body).Decode(entry); err == nil {
				if entry.IsDirectory {
					return self.enqueueDirectory(remotePeer, shareID, entryID, destination)
				} else {
					return self.enqueueFile(remotePeer, shareID, entry, destination)
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

func (self *DownloadQueue) Enqueue(download *QueuedDownload) error {
	log.Debugf("Adding %+v", download)
	return self.app.Database.Downloads.Create(download)
}

func (self *DownloadQueue) enqueueDirectory(remotePeer *peer.RemotePeer, shareID string, directoryID string, destination string) error {
	// get file record from peer
	if response, err := remotePeer.ServiceRequest(`GET`, fmt.Sprintf("/shares/%s/browse/%s", shareID, directoryID), nil, nil); err == nil {
		recordset := dal.NewRecordSet()

		// parse and load record
		if err := json.NewDecoder(response.Body).Decode(recordset); err == nil {
			for _, record := range recordset.Records {
				if err := self.Add(remotePeer.SessionID(), shareID, fmt.Sprintf("%v", record.ID), destination); err != nil {
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

func (self *DownloadQueue) enqueueFile(remotePeer *peer.RemotePeer, shareID string, entry *db.Entry, destination string) error {
	now := time.Now()

	var size uint64

	if entry.RelativePath == `` {
		return fmt.Errorf("File record does not contain a 'name' field")
	}

	if absPath, err := filepath.Abs(destination); err == nil {
		destination = absPath
	} else {
		return err
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
		download.DestinationPath = destination
		download.Size = size
		download.AddedAt = now

		return self.Enqueue(download)
	} else {
		return fmt.Errorf("Failed to create new download instance")
	}
}

// Downloads the given file ID from a named peer or session ID.  This function will block waiting
// for the download to finish.  The QueuedDownload that is returned is an io.Reader referencing the
// downloaded data.
//
func (self *DownloadQueue) Download(w io.Writer, sessionID string, shareID string, fileID string) (*QueuedDownload, error) {
	if download, ok := db.DownloadsSchema.NewInstance().(*QueuedDownload); ok {
		download.Status = `idle`
		download.SessionID = sessionID
		download.ShareID = shareID
		download.FileID = fileID
		download.AddedAt = time.Now()
		download.app = self.app

		return download, download.Download(w)
	} else {
		return nil, fmt.Errorf("Failed to create new download instance")
	}
}

func (self *DownloadQueue) WaitForEmpty() {
	<-self.waitForEmpty
	return
}

func (self *DownloadQueue) downloadWorker(workerID int) {
	for download := range self.workerPool {
		id := fmt.Sprintf("%v", download.ID)

		if self.customDownloader != nil {
			download.customDownloader = self.customDownloader
		}

		log.Debugf("Downloading %v", download)
		self.ActiveDownloads.Set(id, download)

		stats.Increment(`byteflood.queue.downloads.started`, map[string]interface{}{
			`worker`: workerID,
			`peer`:   download.PeerName,
			`share`:  download.ShareID,
		})

		if err := download.Download(); err != nil {
			log.Errorf("Stopping download %v: %v", download, err)
			download.Stop(err)

			stats.Increment(`byteflood.queue.downloads.completed`, map[string]interface{}{
				`worker`: workerID,
				`peer`:   download.PeerName,
				`share`:  download.ShareID,
				`error`:  true,
			})
		} else {
			stats.Increment(`byteflood.queue.downloads.completed`, map[string]interface{}{
				`worker`: workerID,
				`peer`:   download.PeerName,
				`share`:  download.ShareID,
				`error`:  false,
			})
		}

		if err := self.app.Database.Downloads.Update(download); err == nil {
			log.Warningf("Failed to update queue download: %v", err)
		}

		self.ActiveDownloads.Remove(id)
		self.waitForComplete.Done()
	}
}

// Downloads all files in the queue.  If no files are currently in the queue,
// this function will poll the queue on the interval defined in EmptyPollInterval.
//
// Completed items will be moved to the CompletedItems slice.
//
func (self *DownloadQueue) DownloadAll() {
	for i := 0; i < ConcurrentDownloads; i++ {
		log.Debugf("Starting download worker %d", i)
		go self.downloadWorker(i)
	}

	for {
		if download := self.NextDownload(); download != nil {
			self.waitForComplete.Add(1)
			self.workerPool <- download
		} else {
			select {
			case self.waitForEmpty <- true:
			default:
			}

			if self.ExitOnEmpty {
				self.waitForComplete.Wait()
				return
			}

			time.Sleep(EmptyPollInterval)
		}
	}
}

func (self *DownloadQueue) NextDownload() *QueuedDownload {
	if f, err := db.ParseFilter(map[string]interface{}{
		`status`: `idle`,
	}); err == nil {
		f.Sort = []string{`-priority`}
		f.Limit = 1

		var downloads []*QueuedDownload

		if err := self.app.Database.Downloads.Find(f, &downloads); err == nil {
			if len(downloads) > 0 {
				download := downloads[0]
				download.SetStatus(`pending`)

				if err := self.app.Database.Downloads.Update(download); err == nil {
					download.SetApplication(self.app)

					return download
				} else {
					log.Errorf("Error retrieving current download: %v", err)
				}
			}
		} else {
			log.Errorf("Error retrieving current download: %v", err)
		}
	} else {
		log.Errorf("Error retrieving current download: %v", err)
	}

	return nil
}

func (self *DownloadQueue) Clear(statuses ...string) error {
	idsToRemove := make([]interface{}, 0)

	if len(statuses) == 0 {
		statuses = []string{
			`idle`,
			`pending`,
			`completed`,
		}
	}

	for _, status := range statuses {
		if f, err := db.ParseFilter(map[string]interface{}{
			`status`: status,
		}); err == nil {
			f.Fields = []string{`id`}

			if err := self.app.Database.Downloads.FindFunc(f, QueuedDownload{}, func(i interface{}, err error) {
				if err == nil {
					if download, ok := i.(*QueuedDownload); ok {
						idsToRemove = append(idsToRemove, download.ID)
					}
				} else {
					log.Debugf("Error removing download: %v", err)
				}
			}); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	if l := len(idsToRemove); l > 0 {
		log.Debugf("Removing %v downloads", l)
		self.app.Database.Downloads.Delete(idsToRemove...)
	}

	return nil
}
