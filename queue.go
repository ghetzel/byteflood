package byteflood

import (
	"encoding/json"
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/ghetzel/pivot/dal"
	"io"
	"io/ioutil"
	"os"
	"path"
	"time"
)

var EmptyPollInterval = 3 * time.Second

// A QueuedDownload represents the transfer if a single file object from an actively-connected
// RemotePeer (identified by its SessionID.)  Downloads move through several discrete states,
// starting with "idle", which means it is sitting in a queue waiting to start.
//
// Once the download commences, it will enter the "waiting" state once file metadata has been
// successfully read from the remote peer.
//
// When data is being actively received, the download will be in the "downloading" state.  Upon
// completion, the download will either be "completed" (meaning the full file was received and
// the checksum was verified), or "failed", meaning that some error occurred.
//
// Any errors will be available in the Error field if they occur.
//
type QueuedDownload struct {
	ID              int       `json:"id"`
	Status          string    `json:"status"`
	Priority        int64     `json:"priority"`
	SessionID       string    `json:"session_id"`
	FileID          string    `json:"file_Id"`
	FileName        string    `json:"filename"`
	Destination     string    `json:"destination"`
	Size            uint64    `json:"size"`
	PeerName        string    `json:"peer_name"`
	Error           string    `json:"error,omitempty"`
	AddedAt         time.Time `json:"added_at"`
	Progress        float64   `json:"progress"`
	Rate            uint64    `json:"rate"`
	destinationFile io.Reader
	application     *Application
	lastByteSize    uint64
}

func (self *QueuedDownload) Read(p []byte) (int, error) {
	if self.destinationFile != nil {
		return self.destinationFile.Read(p)
	}

	return 0, fmt.Errorf("file not downloaded")
}

func (self *QueuedDownload) Download() error {
	// validate download details
	if self.ID <= 0 {
		return fmt.Errorf("download ID must be a positive integer")
	}

	if self.FileName == `` {
		return fmt.Errorf("download name required")
	}

	if self.Size == 0 {
		return fmt.Errorf("download size required")
	}

	if stat, err := os.Stat(self.Destination); err == nil {
		if !stat.IsDir() {
			return fmt.Errorf("download destination must be a directory")
		}
	} else {
		return err
	}

	// get peer
	if remotePeer, ok := self.application.LocalPeer.GetSession(self.SessionID); ok {
		self.Status = `waiting`

		// create temporary destination
		if err := os.Mkdir(self.Destination, 0755); err == nil || os.IsExist(err) {
			tmpfile := path.Join(self.Destination, fmt.Sprintf(".byteflood-%v.part", self.ID))

			// open the destination file

			if file, err := os.Create(tmpfile); err == nil {
				// make our side of the connection aware of the file transfer
				transfer := remotePeer.CreateInboundTransfer(self.Size)
				transfer.SetWriter(file)

				// no matter what, we're done with this transfer when this function returns
				defer func() {
					remotePeer.RemoveInboundTransfer(transfer.ID)
				}()

				go func(item *QueuedDownload, t *peer.Transfer) {
					for {
						if t.IsFinished() {
							return
						}

						if t.BytesReceived >= t.ExpectedSize {
							item.Progress = 1.0
						} else {
							item.Progress = float64(t.BytesReceived) / float64(t.ExpectedSize)
						}

						item.Status = `downloading`
						item.Size = t.BytesReceived

						time.Sleep(time.Second)

						if item.lastByteSize > 0 {
							item.Rate = (t.BytesReceived - item.lastByteSize)
						}

						item.lastByteSize = t.BytesReceived
					}
				}(self, transfer)

				// tell the remote side to start sending data
				if response, err := remotePeer.ServiceRequest(
					`POST`,
					fmt.Sprintf("/transfers/%s/%s", transfer.ID, self.FileID),
					nil,
					nil,
				); err == nil {
					// if all goes well, block until the download succeeds or fails
					if response.StatusCode < 400 {
						// wait for the transfer to complete
						if err := transfer.Wait(); err == nil {
							self.Progress = 1.0
							self.Status = `completed`
							self.Size = transfer.BytesReceived

							destFile := path.Join(self.Destination, self.FileName)

							if err := os.Rename(tmpfile, destFile); err == nil {
								// reopen the downloaded file as readable
								if readFile, err := os.Open(destFile); err == nil {
									self.destinationFile = readFile
								} else {
									return err
								}
							} else {
								return err
							}

							return nil
						} else {
							return err
						}
					} else {
						body, _ := ioutil.ReadAll(response.Body)
						return fmt.Errorf("%s: %v", response.Status, string(body[:]))
					}
				} else {
					return err
				}
			} else {
				return err
			}
		} else {
			return err
		}
	} else {
		return fmt.Errorf("Could not locate session %s", self.SessionID)
	}
}

type DownloadQueue struct {
	application *Application
}

func NewDownloadQueue(app *Application) *DownloadQueue {
	return &DownloadQueue{
		application: app,
	}
}

// Appends a file to the download queue.
//
func (self *DownloadQueue) Add(sessionID string, entryID string) error {
	// get peer
	if remotePeer, ok := self.application.LocalPeer.GetSession(sessionID); ok {
		// get file record from peer
		if response, err := remotePeer.ServiceRequest(`GET`, fmt.Sprintf("/db/view/%s", entryID), nil, nil); err == nil {
			record := dal.NewRecord(entryID)

			// parse and load record
			if err := json.NewDecoder(response.Body).Decode(record); err == nil {
				if self.recordIsDirectory(record) {
					return self.enqueueDirectory(remotePeer, entryID)
				} else {
					return self.enqueueFile(remotePeer, record)
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

func (self *DownloadQueue) enqueueDirectory(remotePeer *peer.RemotePeer, directoryID string) error {
	// get file record from peer
	if response, err := remotePeer.ServiceRequest(`GET`, fmt.Sprintf("/db/list/_id?q=parent=%s", directoryID), nil, nil); err == nil {
		valueset := make(map[string][]interface{})

		// parse and load record
		if err := json.NewDecoder(response.Body).Decode(&valueset); err == nil {
			if values, ok := valueset[`_id`]; ok {
				for _, id := range values {
					if err := self.Add(remotePeer.SessionID(), fmt.Sprintf("%v", id)); err != nil {
						log.Errorf("Failed to enqueue %s: %v", directoryID, err)
					}
				}

				return nil
			} else {
				return fmt.Errorf("Could not load ID list for parent entry %s", directoryID)
			}
		} else {
			return err
		}
	} else {
		return err
	}
}

func (self *DownloadQueue) enqueueFile(remotePeer *peer.RemotePeer, record *dal.Record) error {
	now := time.Now()

	var filename string
	var size uint64

	if v := record.Get(`name`); v != nil {
		if vv, err := stringutil.ToString(v); err == nil {
			filename = vv
		} else {
			return err
		}
	} else {
		return fmt.Errorf("File record does not contain a 'name' field")
	}

	if v := record.Get(`file.size`); v != nil {
		if vv, err := stringutil.ConvertToInteger(v); err == nil {
			size = uint64(vv)
		} else {
			return err
		}
	} else {
		return fmt.Errorf("File record does not contain a 'file.size' field")
	}

	if err := db.Downloads.Create(&QueuedDownload{
		Status:      `idle`,
		SessionID:   remotePeer.SessionID(),
		PeerName:    remotePeer.Name,
		FileID:      fmt.Sprintf("%v", record.ID),
		Priority:    now.UnixNano(),
		FileName:    filename,
		Destination: fmt.Sprintf("/tmp/%s", remotePeer.ID),
		Size:        size,
		AddedAt:     now,
	}); err == nil {
		return err
	}

	return nil
}

func (self *DownloadQueue) recordIsDirectory(record *dal.Record) bool {
	if directory, ok := record.Fields[`directory`]; ok {
		if v, ok := directory.(bool); ok && v {
			return true
		}
	}

	return false
}

// Downloads the given file ID from a named peer or session ID.  This function will block waiting
// for the download to finish.  The QueuedDownload that is returned is an io.Reader referencing the
// downloaded data.
//
func (self *DownloadQueue) Download(sessionID string, fileID string) (*QueuedDownload, error) {
	download := &QueuedDownload{
		Status:      `idle`,
		SessionID:   sessionID,
		FileID:      fileID,
		AddedAt:     time.Now(),
		application: self.application,
	}

	return download, download.Download()
}

// Downloads all files in the queue.  If no files are currently in the queue,
// this function will poll the queue on the interval defined in EmptyPollInterval.
//
// Completed items will be moved to the CompletedItems slice.
//
func (self *DownloadQueue) DownloadAll() {
	for {
		if item := self.CurrentItem(); item != nil {
			if err := item.Download(); err != nil {
				item.Error = err.Error()
				item.Status = `failed`

				if err := db.Downloads.Update(item); err != nil {
					log.Warningf("Failed to update queue item %s: %v", item.ID, err)
				}
			} else {
				if err := db.Downloads.Delete(item.ID); err != nil {
					log.Warningf("Failed to remove queue item %s: %v", item.ID, err)
				}
			}
		} else {
			time.Sleep(EmptyPollInterval)
		}
	}
}

func (self *DownloadQueue) CurrentItem() *QueuedDownload {
	if f, err := db.ParseFilter("status=idle"); err == nil {
		f.Sort = []string{`-priority`}
		f.Limit = 1

		var downloads []*QueuedDownload

		if err := db.Downloads.Find(f, &downloads); err == nil && len(downloads) == 1 {
			return downloads[0]
		}
	} else {

	}

	return nil
}
