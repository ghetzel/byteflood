package byteflood

import (
    "time"
    "github.com/ghetzel/byteflood/peer"
    "github.com/oleiade/lane"
)

var EmptyPollInterval = 10 * time.Second

type QueuedDownload struct {
    PeerID string `json:"peer_id"`
    FileID string `json:"file_id"`
    Destination string `json:"destination"`
    AddedAt time.Time `json:"added_at"`
    transfer *peer.Transfer
}

type DownloadQueue struct {
    pq *lane.PQueue
}

func NewDownloadQueue() *DownloadQueue {
    return &DownloadQueue{
        pq: lane.NewPQueue(lane.MINPQ),
    }
}

func (self *DownloadQueue) Add(fileID string, transfer *peer.Transfer) {
    now := time.Now()

    self.pq.Push(QueuedDownload{
        FileID: fileID,
        PeerID: transfer.Peer.ID(),
        AddedAt: now,
        transfer: transfer,
    }, int(now.UnixNano()))
}

func (self *DownloadQueue) DownloadAll() {
    for {
        if self.pq.Empty() {
            time.Sleep(EmptyPollInterval)
        }
    }
}
