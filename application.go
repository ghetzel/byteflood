package byteflood

import (
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/encryption"
	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/byteflood/shares"
	"github.com/ghetzel/go-stockutil/pathutil"
	"github.com/ghetzel/go-stockutil/sliceutil"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/ghodss/yaml"
	"github.com/op/go-logging"
	"io/ioutil"
	"os"
)

var log = logging.MustGetLogger(`byteflood`)
var DefaultPublicKeyPath = `~/.config/byteflood/keys/peer.pub`
var DefaultPrivateKeyPath = `~/.config/byteflood/keys/peer.key`

type Application struct {
	LocalPeer      *peer.LocalPeer     `json:"local,omitempty"`
	Database       *db.Database        `json:"database,omitempty"`
	PublicKeyPath  string              `json:"public_key,omitempty"`
	PrivateKeyPath string              `json:"private_key,omitempty"`
	KnownPeers     map[string][]string `json:"known_peers,omitempty"`
	Shares         []*shares.Share     `json:"shares,omitempty"`
	Queue          *DownloadQueue      `json:"queue"`
	API            *API                `json:"api,omitempty"`
	running        chan bool
}

func NewApplicationFromConfig(configFile string) (*Application, error) {
	app := &Application{
		LocalPeer: peer.NewLocalPeer(),
		Database:  db.NewDatabase(),
		running:   make(chan bool),
	}

	app.Queue = NewDownloadQueue(app)

	if configFilePath, err := pathutil.ExpandUser(configFile); err == nil {
		if file, err := os.Open(configFilePath); err == nil {
			if data, err := ioutil.ReadAll(file); err == nil {
				if err := yaml.Unmarshal(data, app); err == nil {
					return app, nil
				} else {
					return nil, err
				}
			} else {
				return nil, err
			}
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
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

	// load keys and initialize LocalPeer
	// --------------------------------------------------------------------------------------------
	if publicKey, privateKey, err := encryption.LoadKeyfiles(
		self.PublicKeyPath,
		self.PrivateKeyPath,
	); err == nil {
		self.LocalPeer.PublicKey = publicKey
		self.LocalPeer.PrivateKey = privateKey

		if err := self.LocalPeer.Initialize(); err != nil {
			return err
		}
	} else {
		return err
	}

	// initialize local API server
	// --------------------------------------------------------------------------------------------
	if self.API == nil {
		self.API = NewAPI()
	}

	self.API.application = self

	if err := self.API.Initialize(); err == nil {
		self.LocalPeer.SetPeerRequestHandler(self.API.GetPeerRequestHandler())
	} else {
		return err
	}

	// initialize all shares
	sharesInitialized := make([]string, 0)

	for _, share := range self.Shares {
		name := stringutil.Underscore(share.Name)

		if !sliceutil.ContainsString(sharesInitialized, name) {
			share.SetMetabase(self.Database)

			if err := share.Initialize(); err != nil {
				return err
			}

			sharesInitialized = append(sharesInitialized, name)
		} else {
			return fmt.Errorf("a share named %q already exists", share.Name)
		}
	}

	return nil
}

func (self *Application) Run() error {
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

	log.Debugf("APP:\n%+v\n", self.String())

	select {
	case err := <-errchan:
		return err
	}
}

func (self *Application) Stop() {
	<-self.LocalPeer.Stop()
}

func (self *Application) Scan(labels ...string) error {
	return self.Database.Scan(labels...)
}

func (self *Application) String() string {
	if data, err := yaml.Marshal(self); err == nil {
		return string(data[:])
	} else {
		return fmt.Sprintf("INVALID: %v", err)
	}
}

func (self *Application) GetShareByName(name string) (*shares.Share, bool) {
	for _, share := range self.Shares {
		if stringutil.Underscore(share.Name) == stringutil.Underscore(name) {
			return share, true
		}
	}

	return nil, false
}
