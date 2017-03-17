package byteflood

import (
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/encryption"
	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/byteflood/shares"
	"github.com/ghetzel/go-stockutil/pathutil"
	"github.com/ghodss/yaml"
	"github.com/op/go-logging"
	"io/ioutil"
	"os"
)

var log = logging.MustGetLogger(`byteflood`)
var DefaultPublicKeyPath = `~/.config/byteflood/keys/peer.pub`
var DefaultPrivateKeyPath = `~/.config/byteflood/keys/peer.key`

type Application struct {
	LocalPeer      *peer.LocalPeer `json:"local,omitempty"`
	Database       *db.Database    `json:"database,omitempty"`
	PublicKeyPath  string          `json:"public_key,omitempty"`
	PrivateKeyPath string          `json:"private_key,omitempty"`
	Queue          *DownloadQueue  `json:"queue"`
	API            *API            `json:"api,omitempty"`
	running        chan bool
}

func NewApplicationFromConfig(configFile string) (*Application, error) {
	app := &Application{
		LocalPeer: peer.NewLocalPeer(),
		Database:  db.NewDatabase(),
		running:   make(chan bool),
	}

	app.Queue = NewDownloadQueue(app)
	app.API = NewAPI(app)

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
	if err := self.API.Initialize(); err == nil {
		self.LocalPeer.SetPeerRequestHandler(self.API.GetPeerRequestHandler())
	} else {
		return err
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

	select {
	case err := <-errchan:
		return err
	}
}

func (self *Application) Stop() {
	<-self.LocalPeer.Stop()
}

func (self *Application) Scan(deep bool, labels ...string) error {
	return self.Database.Scan(deep, labels...)
}

func (self *Application) GetShareByName(name string) (*shares.Share, bool) {
	if f, err := db.ParseFilter("name=%s", name); err == nil {
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
