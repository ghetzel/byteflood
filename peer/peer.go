package peer

import (
	"fmt"
	"github.com/anacrolix/missinggo/slices"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/ghetzel/byteflood/util"
	"github.com/ghetzel/go-stockutil/pathutil"
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

const (
	DEFAULT_STATS_INTERVAL   = (3 * time.Second)
	DEFAULT_IMPORT_INTERVAL  = (30 * time.Second)
	DEFAULT_TRANSFER_DB_PATH = `~/.config/byteflood/transfers`
)

var log = logging.MustGetLogger(`byteflood.client`)
var logproxy = util.NewLogProxy(`byteflood.client`, `info`)

type Peer struct {
	EnableUPnP     bool
	Address        string
	RootDirectory  string
	StatsFile      string
	StatsInterval  time.Duration
	ImportInterval time.Duration
	DatabasePath   string
	client         *torrent.Client
	config         *torrent.Config
	activeTorrents map[string]*torrent.Torrent
	upnpMapping    *upnp.Upnp
}

func CreatePeer(rootDir string, listenAddr string) (*Peer, error) {
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

	return &Peer{
		Address:        listenAddr,
		RootDirectory:  rootDir,
		EnableUPnP:     true,
		StatsInterval:  DEFAULT_STATS_INTERVAL,
		ImportInterval: DEFAULT_IMPORT_INTERVAL,
		DatabasePath:   DEFAULT_TRANSFER_DB_PATH,
		activeTorrents: make(map[string]*torrent.Torrent),
	}, nil
}

func (self *Peer) ImportDirectory(name string) error {
	return filepath.Walk(name, func(entryPath string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && path.Ext(entryPath) == `.torrent` {
			// we implement this directly instead of using client.AddTorrentFromFile because that function
			// throws away the "isNew" value, which we want to suppress logging and making this idempotent
			//
			if metaInfo, err := metainfo.LoadFromFile(entryPath); err == nil {
				if torrent, isNew, err := self.client.AddTorrentSpec(torrent.TorrentSpecFromMetaInfo(metaInfo)); err == nil {
					var nodeList []string
					slices.MakeInto(&nodeList, metaInfo.Nodes)
					self.client.AddDHTNodes(nodeList)

					if isNew {
						log.Noticef("Added %s: hash=%s", entryPath, torrent.InfoHash().HexString())
					}
				} else {
					log.Errorf("Failed to add %s: %v", entryPath, err)
				}
			} else {
				log.Errorf("Failed to add %s: %v", entryPath, err)
			}
		}

		return nil
	})
}

func (self *Peer) Run() error {
	if err := self.setupTorrentClient(); err != nil {
		return err
	}

	if self.EnableUPnP {
		parts := strings.Split(self.config.ListenAddr, `:`)

		if len(parts) > 0 {
			if v, err := stringutil.ConvertToInteger(parts[len(parts)-1]); err == nil {
				self.upnpMapping = new(upnp.Upnp)

				if err := self.upnpMapping.AddPortMapping(int(v), int(v), `tcp`); err == nil {
					log.Infof("Added UPnP port mapping for tcp:%d", v)
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

			go func(t *torrent.Torrent) {
				log.Infof("[%s] Starting torrent '%s'", t.InfoHash().HexString()[0:12], t.Name())
				t.DownloadAll()

				for {
					// stats := thisTorrent.Stats().ConnStats
					// var state string

					// if thisTorrent.Seeding() {
					// 	state = `Seeding`
					// } else {
					// 	state = `Downloading`
					// }

					// log.Debugf("[%s] %s: Downloaded %d, Uploaded %d",
					// 	thisInfohash, state, stats.DataBytesRead, stats.DataBytesWritten)

					time.Sleep(5 * time.Second)
				}
			}(knownTorrent)
		}
	}

	go self.startPeriodicImport()

	select {
	case err := <-errchan:
		return err
	}

	return nil
}

func (self *Peer) Close() {
	log.Infof("Shutting down client")
	self.client.Close()

	if self.upnpMapping != nil {
		log.Infof("Releasing UPnP port")
		self.upnpMapping.Reclaim()
	}
}

func (self *Peer) startPeriodicImport() {
	if self.ImportInterval > 0 {
		log.Debugf("Rechecking %s for new torrents every %s",
			self.config.DataDir, self.ImportInterval)

		for {
			select {
			case <-time.After(self.ImportInterval):
				self.ImportDirectory(self.config.DataDir)
			}
		}
	}
}

func (self *Peer) setupTorrentClient() error {
	if dbPath, err := pathutil.ExpandUser(self.DatabasePath); err == nil {
		if stat, err := os.Stat(dbPath); err != nil || !stat.IsDir() {
			return fmt.Errorf("Directory %q must exist to store transfer data", dbPath)
		}

		torrentConfig := &torrent.Config{
			DataDir:        self.RootDirectory,
			Seed:           true,
			ListenAddr:     self.Address,
			DefaultStorage: storage.NewBoltDB(dbPath),
		}

		if torrentClient, err := torrent.NewClient(torrentConfig); err == nil {
			self.client = torrentClient
			self.config = torrentConfig

			// import the directory that was specified
			if err := self.ImportDirectory(self.RootDirectory); err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		return err
	}

	return nil
}
