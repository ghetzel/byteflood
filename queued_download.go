package byteflood

import (
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/satori/go.uuid"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

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
	app             *Application
	db              *db.Database
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
	remotePeer, ok := self.app.LocalPeer.GetSession(self.SessionID)

	if !ok {
		remotePeer, ok = self.app.LocalPeer.GetSession(self.PeerName)
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
				log.Debugf("[%s] Removing transfer", transfer.ID)
				remotePeer.RemoveInboundTransfer(transfer.ID)
			}()

			log.Debugf("[%s] Starting transfer", transfer.ID)

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
					if err := transfer.Wait(func(id uuid.UUID, bytesReceived uint64, expectedSize uint64) {
						if expectedSize > 0 {
							if bytesReceived >= expectedSize {
								self.Progress = 1.0
							} else {
								self.Progress = float64(bytesReceived) / float64(expectedSize)
							}
						}

						self.Status = `downloading`
						self.Size = bytesReceived

						if self.lastByteSize > 0 {
							self.Rate = (bytesReceived - self.lastByteSize)
						}

						self.lastByteSize = bytesReceived
					}); err == nil {
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

func (self *QueuedDownload) SetDatabase(conn *db.Database) {
	self.db = conn
}