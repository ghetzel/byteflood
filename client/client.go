package client

import (
	"github.com/anacrolix/torrent"
	"github.com/ghetzel/byteflood/util"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/op/go-logging"
	"github.com/prestonTao/upnp"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

var log = logging.MustGetLogger(`byteflood.client`)
var logproxy = util.NewLogProxy(`byteflood.client`, `info`)

type Client struct {
	EnableUPnP     bool
	StatsFile      string
	StatsInterval  time.Duration
	client         *torrent.Client
	config         *torrent.Config
	activeTorrents map[string]*torrent.Torrent
	upnpMapping    *upnp.Upnp
}

func CreateClient(rootDir string, listenAddr string) (*Client, error) {
	if listenAddr == `` {
		if randomSocket, err := net.Listen(`tcp6`, `:0`); err == nil {
			listenAddr = randomSocket.Addr().String()

			if err := randomSocket.Close(); err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	torrentConfig := &torrent.Config{
		DataDir:    rootDir,
		Seed:       true,
		ListenAddr: listenAddr,
	}

	if torrentClient, err := torrent.NewClient(torrentConfig); err == nil {
		client := &Client{
			client:         torrentClient,
			config:         torrentConfig,
			activeTorrents: make(map[string]*torrent.Torrent),
			EnableUPnP:     true,
			StatsInterval:  (3 * time.Second),
		}

		// import the directory that was specified
		if err := client.ImportDirectory(rootDir); err != nil {
			return nil, err
		}

		return client, nil
	} else {
		return nil, err
	}
}

func (self *Client) ImportDirectory(name string) error {
	return filepath.Walk(name, func(entryPath string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && path.Ext(entryPath) == `.torrent` {
			if torrent, err := self.client.AddTorrentFromFile(entryPath); err == nil {
				log.Noticef("Added %s: hash=%s", entryPath, torrent.InfoHash().HexString())
			} else {
				log.Errorf("Failed to add %s: %v", entryPath, err)
			}
		}

		return nil
	})
}

func (self *Client) Run() error {
	if self.EnableUPnP {
		parts := strings.Split(self.config.ListenAddr, `:`)

		if len(parts) > 0 {
			if v, err := stringutil.ConvertToInteger(parts[len(parts)-1]); err == nil {
				self.upnpMapping = new(upnp.Upnp)

				if err := self.upnpMapping.AddPortMapping(int(v), int(v), `tcp`); err == nil {
					log.Infof("Added UPnP port mapping for tcp:%d", v)
					defer self.upnpMapping.Reclaim()
				} else {
					return err
				}
			} else {
				return err
			}
		}
	}

	if self.StatsFile != `` {
		go func() {
			for {
				if file, err := os.Create(self.StatsFile); err == nil {
					self.client.WriteStatus(file)
					<-time.After(self.StatsInterval)
				} else {
					log.Errorf("Unable to write to stats file: %v", err)
				}
			}
		}()
	}

	log.Infof("Starting Peer ID %x", self.client.PeerID())
	log.Infof("Listening on %s", self.config.ListenAddr)
	errchan := make(chan error)

	for _, knownTorrent := range self.client.Torrents() {
		if knownTorrent.Info() == nil {
			log.Infof("%s: Waiting for info...", knownTorrent.Name())
			<-knownTorrent.GotInfo()
		}

		infohash := knownTorrent.InfoHash().HexString()

		if _, ok := self.activeTorrents[infohash]; !ok {
			self.activeTorrents[infohash] = knownTorrent

			go func(thisTorrent *torrent.Torrent) {
				thisInfohash := thisTorrent.InfoHash().HexString()

				log.Infof("[%s] Starting torrent '%s'", thisInfohash, thisTorrent.Name())
				thisTorrent.DownloadAll()

				for {
					stats := thisTorrent.Stats().ConnStats
					var state string

					if thisTorrent.Seeding() {
						state = `Seeding`
					} else {
						state = `Downloading`
					}

					log.Debugf("[%s] %s: Downloaded %d, Uploaded %d",
						thisInfohash, state, stats.DataBytesRead, stats.DataBytesWritten)

					time.Sleep(5 * time.Second)
				}
			}(knownTorrent)
		}
	}

	select {
	case err := <-errchan:
		return err
	}

	return nil
}

func (self *Client) Close() {
	log.Infof("Shutting down client")
	self.client.Close()

	if self.upnpMapping != nil {
		self.upnpMapping.Reclaim()
	}
}
