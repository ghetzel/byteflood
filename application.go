package byteflood

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"time"

	"github.com/fatih/set"
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

// Create a new, unconfigured application instance.
func NewApplication() *Application {
	app := &Application{
		Database: db.NewDatabase(),
		running:  make(chan bool),
	}

	app.LocalPeer = peer.NewLocalPeer()
	app.Queue = NewDownloadQueue(app)
	app.API = NewAPI(app)

	return app
}

// Create a new application instance with a given configuration file path.
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

// Initialize the application and perform basic configuration checks.
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

	// create database base directory (if necessary)
	if v, err := pathutil.ExpandUser(self.Database.BaseDirectory); err == nil {
		// if the directory does not exist, create it...
		if _, err := os.Stat(v); os.IsNotExist(err) {
			log.Noticef("Creating directory %v", v)

			if err := os.MkdirAll(v, 0755); err != nil {
				return err
			}
		}
	} else {
		return err
	}

	// automatically generate keys if neither keypath exists
	if _, err := os.Stat(self.PublicKeyPath); os.IsNotExist(err) {
		if _, err := os.Stat(self.PrivateKeyPath); os.IsNotExist(err) {
			if err := os.MkdirAll(path.Dir(self.PublicKeyPath), 0700); err != nil {
				return err
			}

			if err := os.MkdirAll(path.Dir(self.PrivateKeyPath), 0700); err != nil {
				return err
			}

			if err := encryption.GenerateKeypair(self.PublicKeyPath, self.PrivateKeyPath); err != nil {
				return err
			}
		}
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

	// setup stats emission and persistence
	if self.Stats.Path == `` {
		self.Stats.Path = path.Join(self.Database.BaseDirectory, `stats`)
	}

	return nil
}

func (self *Application) Run() error {
	// setup stats collection and reporting
	if err := stats.Initialize(self.Stats.Path, maputil.Append(self.Stats.Tags, map[string]interface{}{
		`hostname`: self.LocalPeer.Hostname,
		`username`: self.LocalPeer.User,
	})); err != nil {
		return err
	}

	// listen for and respond to peer events at the application level
	go self.monitorPeerEvents()

	// listen for downloaded files and trigger local database scans as appropriate
	go self.monitorQueueCompletedFiles(15 * time.Second)

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

// Gracefully stop the application.
func (self *Application) Stop() {
	<-self.LocalPeer.Stop()
	stats.Cleanup()
}

// Perform a scan of all or some of the scanned directories configured in the database.
func (self *Application) Scan(deep bool, labels ...string) error {
	backends.BleveBatchFlushCount = 25
	defer func() {
		backends.BleveBatchFlushCount = 1
		self.collectShareStats(time.Duration(0))
	}()

	return self.Database.Scan(deep, labels...)
}

// Synchronize all subscriptions or only a subset based on whether a given list of peers is currently connected.
func (self *Application) Sync(peerNames ...string) error {
	var subscriptions []*Subscription
	var connectedPeers []string

	for _, name := range peerNames {
		if _, ok := self.LocalPeer.GetSession(name); ok {
			connectedPeers = append(connectedPeers, name)
		}
	}

	// get all subscriptions
	if err := db.Subscriptions.All(&subscriptions); err == nil {
		// for each subscription...
		for _, subscription := range subscriptions {
			// expand the SourceGroup into a list of peers
			if subscriptionPeers, err := peer.ExpandPeerGroup(subscription.SourceGroup); err == nil {
				// if any of the peers in that group are currently connected...
				if len(peerNames) == 0 || sliceutil.ContainsAnyString(connectedPeers, subscriptionPeers...) {
					log.Debugf("Sync subscription %v", subscription)

					// sync the subscription, as we have a source for the data
					if err := subscription.Sync(self); err != nil {
						log.Warningf("Sync failed for %v: %v", subscription.ID, err)
					}
				} else {
					log.Debugf(
						"Skipping subscription %v sync: no peers available to service this subscription (%+v not in %+v [%v])",
						subscription.ID,
						connectedPeers,
						subscriptionPeers,
						subscription.SourceGroup,
					)
				}
			} else {
				log.Errorf("Failed to retrieve peers in group %q: %v", subscription.SourceGroup, err)
				continue
			}
		}

		return nil
	} else {
		return err
	}
}

// Retrieve a share by name.
func (self *Application) GetShareByName(name string) (*shares.Share, bool) {
	if f, err := db.ParseFilter(map[string]interface{}{
		`name`: name,
	}); err == nil {
		var shares []*shares.Share

		if err := db.Shares.Find(f, &shares); err == nil {
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
	go self.collectQueueStats(2 * time.Second)
	go self.collectPeerStats(10 * time.Second)
	go self.collectShareStats(5 * time.Minute)
	go self.collectDatabaseStats(5 * time.Minute)

	select {}
}

func (self *Application) collectQueueStats(interval time.Duration) {
	for {
		if interval == 0 {
			return
		} else {
			time.Sleep(interval)
		}
	}
}

func (self *Application) collectPeerStats(interval time.Duration) {
	for {
		if interval == 0 {
			return
		} else {
			time.Sleep(interval)
		}
	}
}

func (self *Application) collectShareStats(interval time.Duration) {
	for {
		var shares []*shares.Share

		if err := db.Shares.All(&shares); err == nil {
			for _, share := range shares {
				if err := share.RefreshShareStats(); err != nil {
					log.Warningf("Failed to refresh stats for share %s: %v", share.ID, err)
				}
			}
		} else {
			log.Warningf("Failed to refresh share stats: %v", err)
		}

		if interval == 0 {
			return
		} else {
			time.Sleep(interval)
		}
	}
}

func (self *Application) collectDatabaseStats(interval time.Duration) {
	for {
		var dirs []*db.Directory

		if err := db.ScannedDirectories.All(&dirs); err == nil {
			for _, dir := range dirs {
				if err := dir.RefreshStats(); err != nil {
					log.Warningf("Failed to refresh stats for directory %s: %v", dir.ID, err)
				}
			}
		} else {
			log.Warningf("Failed to refresh directory stats: %v", err)
		}

		if interval == 0 {
			return
		} else {
			time.Sleep(interval)
		}
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

func (self *Application) monitorQueueCompletedFiles(interval time.Duration) {
	fileset := set.New()

	// this routine accumulates all of the files completed in the past 15 seconds,
	// figures out which (if any) Scanned Directories were affected, and scans them
	go func() {
		for {
			paths := set.StringSlice(fileset)
			fileset.Clear()
			scanset := set.New()

			for _, filePath := range paths {
				for _, dir := range self.Database.GetDirectoriesByFile(filePath) {
					scanset.Add(dir.ID)
				}
			}

			if affectedLabels := set.StringSlice(scanset); len(affectedLabels) > 0 {
				// scan all affected labels
				self.Database.Scan(false, affectedLabels...)
			}

			time.Sleep(interval)
		}
	}()

	for filePath := range self.Queue.CompletedFiles() {
		fileset.Add(filePath)
	}
}
