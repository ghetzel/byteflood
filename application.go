package byteflood

import (
	"github.com/ghetzel/byteflood/encryption"
	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/byteflood/scanner"
)

type Application struct {
	PeerAddress string
	config      Configuration
	localPeer   *peer.LocalPeer
	scanner     *scanner.Scanner
	api         *API
}

func CreateApplication(config Configuration) (*Application, error) {
	app := &Application{
		config: config,
	}

	// load keys and create LocalPeer
	if publicKey, privateKey, err := encryption.LoadKeyfiles(
		config.PublicKey,
		config.PrivateKey,
	); err == nil {
		if localPeer, err := peer.CreatePeer(
			publicKey,
			privateKey,
		); err == nil {
			app.localPeer = localPeer

			// set local peer-specific configuration
			app.localPeer.DownloadBytesPerSecond = config.DownloadCap
			app.localPeer.UploadBytesPerSecond = config.UploadCap
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}

	// create local API server
	app.api = NewAPI(app)
	app.api.Address = config.ApiAddress
	app.localPeer.SetPeerRequestHandler(app.api.peerRequestHandler())

	return app, nil
}

func (self *Application) Scanner() *scanner.Scanner {
	if self.scanner == nil {
		if err := self.setupScanner(); err != nil {
			panic(err.Error())
		}
	}

	return self.scanner
}

func (self *Application) LocalPeer() *peer.LocalPeer {
	if self.localPeer == nil {
		panic("invalid local peer")
	}

	return self.localPeer
}

func (self *Application) setupScanner() error {
	if self.scanner == nil {
		self.scanner = scanner.NewScanner(self.config.ScanOptions)
		self.scanner.DatabaseURI = self.config.DatabaseURI

		// initialize scanner and open metadata database
		if err := self.Scanner().Initialize(); err == nil {
			// add all configured directories to scanner
			for _, directory := range self.config.Directories {
				if err := self.Scanner().AddDirectory(&directory); err != nil {
					return err
				}
			}
		} else {
			return err
		}
	}

	return nil
}

func (self *Application) Run() error {
	if err := self.setupScanner(); err != nil {
		return err
	}

	errchan := make(chan error)

	// start local peer
	go func() {
		errchan <- self.localPeer.Run()
	}()

	// start local API server
	go func() {
		errchan <- self.api.Serve()
	}()

	select {
	case err := <-errchan:
		return err
	}
}

func (self *Application) Stop() {
	<-self.localPeer.Stop()
}

func (self *Application) ScanAll() error {
	if err := self.setupScanner(); err != nil {
		return err
	}

	return self.Scanner().Scan()
}
