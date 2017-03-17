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
	"path/filepath"
	"strings"
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
	PeerName        string    `json:"peer_name"`
	SessionID       string    `json:"session_id"`
	ShareID         string    `json:"share_id"`
	FileID          string    `json:"file_id"`
	Name            string    `json:"name"`
	DestinationPath string    `json:"destination"`
	Size            uint64    `json:"size"`
	AddedAt         time.Time `json:"added_at,omitempty"`
	Error           string    `json:"error,omitempty"`
	Progress        float64   `json:"progress"`
	Rate            uint64    `json:"rate"`
	application     *Application
	tempFile        *os.File
	destinationFile io.Reader
	lastByteSize    uint64
}

func (self *QueuedDownload) Read(p []byte) (int, error) {
	if self.destinationFile != nil {
		return self.destinationFile.Read(p)
	}

	return 0, fmt.Errorf("file not downloaded")
}

func (self *QueuedDownload) Download(writers ...io.Writer) error {
	// get peer by session id, fallback to peer name
	remotePeer, ok := self.application.LocalPeer.GetSession(self.SessionID)

	if !ok {
		remotePeer, ok = self.application.LocalPeer.GetSession(self.PeerName)
	}

	if ok {
		var destWriter io.Writer
		var destFile string

		self.Status = `waiting`

		// a given writer supercedes the destination path
		if len(writers) > 0 {
			destWriter = writers[0]
		} else {
			// validate download details
			if self.ID <= 0 {
				return fmt.Errorf("Download ID must be a positive integer")
			}

			if self.Name == `` {
				return fmt.Errorf("Download name required")
			}

			if self.Size == 0 {
				return fmt.Errorf("Download size required")
			}

			if stat, err := os.Stat(self.DestinationPath); err == nil {
				if !stat.IsDir() {
					return fmt.Errorf("Download destination must be a directory")
				}
			} else if os.IsNotExist(err) {
				if err := os.MkdirAll(self.DestinationPath, 0700); err != nil {
					return err
				}
			} else {
				return err
			}

			destFile = path.Join(self.DestinationPath, self.Name)

			// expand the destination path to an absolute one and verify that it's valid/safe
			if d, err := self.VerifyPath(destFile); err == nil {
				destFile = d
			} else {
				return err
			}

			destDir := path.Dir(destFile)

			// create destination parent directory
			if err := os.MkdirAll(destDir, 0755); err == nil || os.IsExist(err) {
				// open the destination file
				if file, err := ioutil.TempFile(destDir, fmt.Sprintf("byteflood-%v_", self.ID)); err == nil {
					self.tempFile = file
					destWriter = file
				} else {
					return err
				}
			} else {
				return err
			}
		}

		if destWriter != nil {
			// make our side of the connection aware of the file transfer
			transfer := remotePeer.CreateInboundTransfer(self.Size)
			transfer.SetWriter(destWriter)

			// no matter what, we're done with this transfer when this function returns
			defer func() {
				remotePeer.RemoveInboundTransfer(transfer.ID)
			}()

			// poll transfer and update stats and status
			go func(item *QueuedDownload, t *peer.Transfer) {
				for {
					if t.IsFinished() {
						log.Debugf("[%v] Transfer finished", t.ID)
						return
					}

					if t.ExpectedSize > 0 {
						if t.BytesReceived >= t.ExpectedSize {
							item.Progress = 1.0
						} else {
							item.Progress = float64(t.BytesReceived) / float64(t.ExpectedSize)
						}
					}

					item.Status = `downloading`
					item.Size = t.BytesReceived

					time.Sleep(time.Second)

					if item.lastByteSize > 0 {
						item.Rate = (t.BytesReceived - item.lastByteSize)
					}

					item.lastByteSize = t.BytesReceived

					log.Debugf("[%v] Progress: %g, %d bytes", t.ID, item.Progress, item.lastByteSize)
				}
			}(self, transfer)

			// ask the remote side to start sending data
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

			// post-download step for files being saved to the filesystem
			if self.tempFile != nil {
				// move the temporary download file to the final filename
				if err := os.Rename(self.tempFile.Name(), destFile); err == nil {
					// reopen the downloaded file as readable
					if readFile, err := os.Open(destFile); err == nil {
						self.destinationFile = readFile
					} else {
						return err
					}
				} else {
					return err
				}
			}

			return nil
		} else {
			return fmt.Errorf("No valid destination provided")
		}
	} else {
		return fmt.Errorf("Could not locate session %s or peer %s", self.SessionID, self.PeerName)
	}
}

func (self *QueuedDownload) VerifyPath(name string) (string, error) {
	// fully resolve the given path into an absolute path
	if absPath, err := filepath.Abs(name); err == nil {
		// the absolute path must fall under the destination directory
		if strings.HasPrefix(name, strings.TrimSuffix(self.DestinationPath, `/`)+`/`) {
			return absPath, nil
		} else {
			return ``, fmt.Errorf("absolute path resolves to path outside of the destination directory")
		}
	} else {
		return ``, err
	}
}

type DownloadQueue struct {
	CurrentDownload *QueuedDownload `json:"current_download"`
	application     *Application
}

func NewDownloadQueue(app *Application) *DownloadQueue {
	return &DownloadQueue{
		application: app,
	}
}

// Appends a file to the download queue.
//
func (self *DownloadQueue) Add(sessionID string, shareID string, entryID string) error {
	// get peer
	if remotePeer, ok := self.application.LocalPeer.GetSession(sessionID); ok {
		// get file record from peer
		if response, err := remotePeer.ServiceRequest(`GET`, fmt.Sprintf("/shares/%s/view/%s", shareID, entryID), nil, nil); err == nil {
			file := new(db.File)

			// parse and load record
			if err := json.NewDecoder(response.Body).Decode(file); err == nil {
				if file.IsDirectory {
					return self.enqueueDirectory(remotePeer, shareID, entryID)
				} else {
					return self.enqueueFile(remotePeer, shareID, file)
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

func (self *DownloadQueue) enqueueFile(remotePeer *peer.RemotePeer, shareID string, file *db.File) error {
	now := time.Now()

	var size uint64

	if file.RelativePath == `` {
		return fmt.Errorf("File record does not contain a 'name' field")
	}

	if v := file.Get(`file.size`); v != nil {
		if vv, err := stringutil.ConvertToInteger(v); err == nil {
			size = uint64(vv)
		} else {
			return err
		}
	} else {
		return fmt.Errorf("File record does not contain a 'file.size' field")
	}

	if err := db.Downloads.Create(&QueuedDownload{
		Status:          `idle`,
		SessionID:       remotePeer.SessionID(),
		PeerName:        remotePeer.Name,
		ShareID:         shareID,
		FileID:          file.ID,
		Priority:        now.UnixNano(),
		Name:            file.RelativePath,
		DestinationPath: fmt.Sprintf("/tmp/%s", remotePeer.ID),
		Size:            size,
		AddedAt:         now,
	}); err == nil {
		return err
	}

	return nil
}

// Downloads the given file ID from a named peer or session ID.  This function will block waiting
// for the download to finish.  The QueuedDownload that is returned is an io.Reader referencing the
// downloaded data.
//
func (self *DownloadQueue) Download(w io.Writer, sessionID string, fileID string) (*QueuedDownload, error) {
	download := &QueuedDownload{
		Status:      `idle`,
		SessionID:   sessionID,
		FileID:      fileID,
		AddedAt:     time.Now(),
		application: self.application,
	}

	return download, download.Download(w)
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

			if err := db.Downloads.Update(item); err != nil {
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

		if err := db.Downloads.Find(f, &downloads); err == nil && len(downloads) == 1 {
			download := downloads[0]
			download.application = self.application

			return download
		}
	} else {

	}

	return nil
}
