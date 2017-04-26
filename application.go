package byteflood

import (
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/encryption"
	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/byteflood/shares"
	"github.com/ghetzel/byteflood/stats"
	"github.com/ghetzel/go-stockutil/maputil"
	"github.com/ghetzel/go-stockutil/pathutil"
	"github.com/ghetzel/go-stockutil/sliceutil"
	"github.com/ghetzel/pivot/backends"
	"github.com/ghetzel/pivot/dal"
	"github.com/ghodss/yaml"
	"github.com/op/go-logging"
	"io/ioutil"
	"os"
	"os/user"
	"path"
	"time"
)

var log = logging.MustGetLogger(`byteflood`)
var DefaultPublicKeyPath = `~/.config/byteflood/keys/peer.pub`
var DefaultPrivateKeyPath = `~/.config/byteflood/keys/peer.key`

type Stats struct {
	Path       string                 `json:"path,omitempty"`
	Tags       map[string]interface{} `json:"tags,omitempty"`
	StatsdHost string                 `json:"statsd_host,omitempty"`
}

type Application struct {
	LocalPeer      *peer.LocalPeer `json:"local,omitempty"`
	Database       *db.Database    `json:"database,omitempty"`
	Stats          Stats           `json:"stats,omitempty"`
	PublicKeyPath  string          `json:"public_key,omitempty"`
	PrivateKeyPath string          `json:"private_key,omitempty"`
	Queue          *DownloadQueue  `json:"queue"`
	API            *API            `json:"api,omitempty"`
	running        chan bool
}

func NewApplication() *Application {
	app := &Application{
		Database: db.NewDatabase(),
		running:  make(chan bool),
	}

	app.LocalPeer = peer.NewLocalPeer(app.Database)
	app.Queue = NewDownloadQueue(app)
	app.API = NewAPI(app)

	return app
}

func NewApplicationFromConfig(configFile string) (*Application, error) {
	app := NewApplication()

	if configFilePath, err := pathutil.ExpandUser(configFile); err == nil {
		if file, err := os.Open(configFilePath); err == nil {
			if data, err := ioutil.ReadAll(file); err == nil {
				if err := yaml.Unmarshal(data, app); err != nil {
					return nil, err
				}
			} else {
				return nil, err
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	} else {
		return nil, err
	}

	return app, nil
}

func (self *Application) Initialize() error {
	// setup defaults and expand key paths
	if self.PublicKeyPath == `` {
		self.PublicKeyPath = DefaultPublicKeyPath
	}

	if self.PrivateKeyPath == `` {
		self.PrivateKeyPath = DefaultPrivateKeyPath
	}

	if v, err := pathutil.ExpandUser(self.PublicKeyPath); err == nil {
		self.PublicKeyPath = v
	} else {
		return err
	}

	if v, err := pathutil.ExpandUser(self.PrivateKeyPath); err == nil {
		self.PrivateKeyPath = v
	} else {
		return err
	}

	// initialize and open database
	// --------------------------------------------------------------------------------------------
	if err := self.Database.Initialize(); err != nil {
		return err
	}

	// register types to schemata,set model initializers and perform test instantiation of instances
	for schema, recordInstance := range map[*dal.Collection]interface{}{
		db.AuthorizedPeersSchema:    peer.AuthorizedPeer{},
		db.DownloadsSchema:          QueuedDownload{},
		db.MetadataSchema:           db.Entry{},
		db.ScannedDirectoriesSchema: db.Directory{},
		db.SharesSchema:             shares.Share{},
		db.SubscriptionsSchema:      Subscription{},
		db.SystemSchema:             db.Property{},
	} {
		schema.SetRecordType(recordInstance)
		schema.SetInitializer(self.Database.Initializer)
		schema.NewInstance()
	}

	// load keys and initialize LocalPeer
	// --------------------------------------------------------------------------------------------
	if publicKey, privateKey, err := encryption.LoadKeyfiles(
		self.PublicKeyPath,
		self.PrivateKeyPath,
	); err == nil {
		self.LocalPeer.PublicKey = publicKey
		self.LocalPeer.PrivateKey = privateKey

		if len(self.LocalPeer.PublicKey) == 0 {
			return fmt.Errorf("Public key is empty")
		}

		if len(self.LocalPeer.PrivateKey) == 0 {
			return fmt.Errorf("Private key is empty")
		}

		if err := self.LocalPeer.Initialize(); err != nil {
			return err
		}
	} else {
		return err
	}

	// initialize local API server
	// --------------------------------------------------------------------------------------------
	if err := self.API.Initialize(); err == nil {
		self.LocalPeer.SetPeerRequestHandler(self.API.GetPeerRequestHandler())
	} else {
		return err
	}

	// listen for and respond to peer events at the application level
	go self.monitorPeerEvents()

	// setup stats emission and persistence
	if self.Stats.Path == `` {
		self.Stats.Path = path.Join(self.Database.BaseDirectory, `stats`)
	}

	return nil
}

func (self *Application) Run() error {
	hostname, _ := os.Hostname()
	username := ``

	if user, err := user.Current(); err == nil {
		username = user.Username
	}

	if err := stats.Initialize(self.Stats.Path, maputil.Append(self.Stats.Tags, map[string]interface{}{
		`hostname`: hostname,
		`username`: username,
	})); err != nil {
		return err
	}

	errchan := make(chan error)

	// start local peer
	go func() {
		errchan <- self.LocalPeer.Run()
	}()

	// start local API server
	go func() {
		errchan <- self.API.Serve()
	}()

	// start processing the download queue
	go func() {
		self.Queue.DownloadAll()
	}()

	defer stats.Cleanup()

	// setup periodic share stats calculaton
	go self.startPeriodicMonitoring()

	stats.Increment(`byteflood.app.started`)

	select {
	case err := <-errchan:
		return err
	}
}

func (self *Application) Stop() {
	<-self.LocalPeer.Stop()
	stats.Cleanup()
}

func (self *Application) Scan(deep bool, labels ...string) error {
	backends.BleveBatchFlushCount = 25
	defer func() {
		backends.BleveBatchFlushCount = 1
	}()

	return self.Database.Scan(deep, labels...)
}

func (self *Application) Sync(peerNames ...string) error {
	var subscriptions []*Subscription
	var connectedPeers []string

	for _, name := range peerNames {
		if _, ok := self.LocalPeer.GetSession(name); ok {
			connectedPeers = append(connectedPeers, name)
		}
	}

	if err := self.Database.Subscriptions.All(&subscriptions); err == nil {
		for _, subscription := range subscriptions {
			subscriptionPeers := peer.ExpandPeerGroup(self.Database, subscription.SourceGroup)

			if len(peerNames) == 0 || sliceutil.ContainsAnyString(connectedPeers, subscriptionPeers...) {
				log.Debugf("Scanning subscription %v...", subscription.ID)

				if err := subscription.Sync(self); err != nil {
					log.Warningf("Sync failed for %v: %v", subscription.ID, err)
				}
			} else {
				log.Debugf(
					"Skipping subscription %v sync: no peers available to service this subscription (%+v not in %+v)",
					subscription.ID,
					connectedPeers,
					subscriptionPeers,
				)
			}
		}

		return nil
	} else {
		return err
	}
}

func (self *Application) GetShareByName(name string) (*shares.Share, bool) {
	if f, err := db.ParseFilter(map[string]interface{}{
		`name`: name,
	}); err == nil {
		var shares []*shares.Share

		if err := self.Database.Shares.Find(f, &shares); err == nil {
			if len(shares) == 1 {
				return shares[0], true
			} else if len(shares) > 1 {
				return nil, false
			}
		} else {
			return nil, false
		}
	} else {
		return nil, false
	}

	return nil, false
}

func (self *Application) startPeriodicMonitoring() {
	go self.collectQueueStats()
	go self.collectPeerStats()
	go self.collectShareStats()
	go self.collectDatabaseStats()

	select {}
}

func (self *Application) collectQueueStats() {
	for {
		time.Sleep(2 * time.Second)
	}
}

func (self *Application) collectPeerStats() {
	for {
		time.Sleep(10 * time.Second)
	}
}

func (self *Application) collectShareStats() {
	for {
		var shares []*shares.Share

		if err := self.Database.Shares.All(&shares); err == nil {
			for _, share := range shares {
				share.SetDatabase(self.Database)

				if err := share.RefreshShareStats(); err != nil {
					log.Warningf("Failed to refresh stats for share %s: %v", share.ID, err)
				}
			}
		} else {
			log.Warningf("Failed to refresh share stats: %v", err)
		}

		time.Sleep(5 * time.Minute)
	}
}

func (self *Application) collectDatabaseStats() {
	for {
		var dirs []*db.Directory

		if err := self.Database.ScannedDirectories.All(&dirs); err == nil {
			for _, dir := range dirs {
				dir.SetDatabase(self.Database)

				if err := dir.RefreshStats(); err != nil {
					log.Warningf("Failed to refresh stats for directory %s: %v", dir.ID, err)
				}
			}
		} else {
			log.Warningf("Failed to refresh directory stats: %v", err)
		}

		time.Sleep(5 * time.Minute)
	}
}

func (self *Application) monitorPeerEvents() {
	for event := range self.LocalPeer.PeerEvents() {
		switch event.Type {
		case peer.PeerConnected:
			self.Sync(event.Peer.Name)
		}
	}
}
