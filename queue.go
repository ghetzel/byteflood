package byteflood

import (
	"encoding/json"
	"fmt"
	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/go-stockutil/maputil"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/ghetzel/pivot/dal"
	"github.com/oleiade/lane"
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
	Status          string    `json:"status"`
	SessionID       string    `json:"session_id"`
	FileID          string    `json:"file_id"`
	FileName        string    `json:"name"`
	Destination     string    `json:"destination"`
	Progress        float64   `json:"progress"`
	Rate            uint64    `json:"rate"`
	Size            uint64    `json:"size"`
	PeerName        string    `json:"peer"`
	Error           string    `json:"error,omitempty"`
	AddedAt         time.Time `json:"added_at"`
	FileCount       int       `json:"file_count"`
	TotalSize       uint64    `json:"total_size"`
	inherits        *QueuedDownload
	application     *Application
	destinationFile io.Reader
	lastByteSize    uint64
}

func (self *QueuedDownload) Read(p []byte) (int, error) {
	if self.destinationFile != nil {
		return self.destinationFile.Read(p)
	}

	return 0, fmt.Errorf("file not downloaded")
}

func (self *QueuedDownload) Download() error {
	self.FileName = self.FileID

	// get peer
	if remotePeer, ok := self.application.LocalPeer.GetPeer(self.SessionID); ok {
		self.PeerName = remotePeer.Name

		// get file record from peer
		if response, err := remotePeer.ServiceRequest(`GET`, fmt.Sprintf("/db/view/%s", self.FileID), nil, nil); err == nil {
			record := dal.NewRecord(self.FileID)

			// parse and load record
			if err := json.NewDecoder(response.Body).Decode(record); err == nil {
				if self.recordIsDirectory(record) {
					return self.downloadDirectory(remotePeer, record)
				} else {
					return self.downloadSingleFile(remotePeer, record)
				}

			} else {
				return err
			}
		} else {
			return err
		}
	} else {
		return fmt.Errorf("session %s not found", self.SessionID)
	}
}

func (self *QueuedDownload) recordIsDirectory(record *dal.Record) bool {
	if directory, ok := record.Fields[`directory`]; ok {
		if v, ok := directory.(bool); ok && v {
			return true
		}
	}

	return false
}

func (self *QueuedDownload) downloadDirectory(remotePeer *peer.RemotePeer, record *dal.Record) error {
	// get file record from peer
	if response, err := remotePeer.ServiceRequest(`GET`, fmt.Sprintf("/db/list/_id?q=parent=%s", self.FileID), nil, nil); err == nil {
		valueset := make(map[string][]interface{})

		// parse and load record
		if err := json.NewDecoder(response.Body).Decode(&valueset); err == nil {
			if values, ok := valueset[`_id`]; ok {
				for _, id := range values {
					download := self.application.Queue.Add(self.SessionID, fmt.Sprintf("%v", id))

					if self.inherits == nil {
						download.inherits = self
					} else {
						download.inherits = self.inherits
					}
				}

				return nil
			} else {
				return fmt.Errorf("Could not load ID list for parent entry %s", self.FileID)
			}
		} else {
			return err
		}
	} else {
		return err
	}
}

func (self *QueuedDownload) downloadSingleFile(remotePeer *peer.RemotePeer, record *dal.Record) error {
	v := maputil.DeepGet(record.Fields, []string{`file`, `size`}, -1)

	if name, ok := record.Fields[`name`]; ok {
		self.FileName, _ = stringutil.ToString(name)
	} else {
		self.FileName = self.FileID
	}

	// get file size
	if size, err := stringutil.ConvertToInteger(v); err == nil {
		if size < 0 {
			return fmt.Errorf("size unknown")
		}

		if self.inherits != nil {
			self.inherits.FileCount += 1
			self.inherits.TotalSize += uint64(size)
		}

		self.Status = `waiting`

		peerRoot := fmt.Sprintf("/tmp/%s", remotePeer.ID())

		// create temporary destination
		if err := os.Mkdir(peerRoot, 0755); err == nil || os.IsExist(err) {
			// open the destination file
			if file, err := os.Create(path.Join(peerRoot, self.FileID)); err == nil {
				self.Destination = file.Name()

				// make our side of the connection aware of the file transfer
				transfer := remotePeer.CreateInboundTransfer(uint64(size))
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

							if self.inherits != nil {
								self.inherits.FileCount -= 1
								self.inherits.TotalSize -= self.Size
							}

							// reopen the downloaded file as readable
							if readFile, err := os.Open(self.Destination); err == nil {
								self.destinationFile = readFile
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
		return err
	}
}

type DownloadQueue struct {
	ActiveTransfers []*QueuedDownload `json:"active"`
	Size            int               `json:"size"`
	CompletedItems  []*QueuedDownload `json:"completed"`
	downloadQueue   *lane.PQueue
	application     *Application
}

func NewDownloadQueue(app *Application) *DownloadQueue {
	return &DownloadQueue{
		downloadQueue:  lane.NewPQueue(lane.MINPQ),
		CompletedItems: make([]*QueuedDownload, 0),
		application:    app,
	}
}

// Appends a file to the download queue.
//
func (self *DownloadQueue) Add(sessionID string, fileID string) *QueuedDownload {
	now := time.Now()

	download := &QueuedDownload{
		Status:      `idle`,
		SessionID:   sessionID,
		FileID:      fileID,
		AddedAt:     now,
		application: self.application,
	}

	self.downloadQueue.Push(download, int(now.UnixNano()))
	return download
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
		self.Size = self.downloadQueue.Size()

		if item := self.CurrentItem(); item != nil {
			if item.inherits == nil {
				self.ActiveTransfers = []*QueuedDownload{item}
			}

			if err := item.Download(); err != nil {
				item.Error = err.Error()
				item.Status = `failed`
				self.Add(item.SessionID, item.FileID)
			} else {
				self.CompletedItems = append(self.CompletedItems, item)
			}

			self.downloadQueue.Pop()
		} else {
			self.ActiveTransfers = nil
			time.Sleep(EmptyPollInterval)
		}
	}
}

func (self *DownloadQueue) CurrentItem() *QueuedDownload {
	if v, _ := self.downloadQueue.Head(); v != nil {
		download, ok := v.(*QueuedDownload)

		if ok {
			return download
		}
	}

	return nil
}
