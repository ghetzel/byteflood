package peer

import (
	"github.com/ghetzel/byteflood/util"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/op/go-logging"
	"github.com/syncthing/syncthing/lib/nat"
	"github.com/syncthing/syncthing/lib/upnp"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DEFAULT_STATS_INTERVAL         = (3 * time.Second)
	DEFAULT_IMPORT_INTERVAL        = (30 * time.Second)
	DEFAULT_UPNP_DISCOVERY_TIMEOUT = (30 * time.Second)
	DEFAULT_UPNP_MAPPING_DURATION  = (8760 * time.Hour)
	BF_UPNP_SERVICE_NAME           = `byteflood`
)

var log = logging.MustGetLogger(`byteflood.client`)
var logproxy = util.NewLogProxy(`byteflood.client`, `info`)

type Peer struct {
	EnableUpnp           bool
	Address              string
	RootDirectory        string
	StatsFile            string
	StatsInterval        time.Duration
	ImportInterval       time.Duration
	UpnpMappingDuration  time.Duration
	UpnpDiscoveryTimeout time.Duration
	defaultGateway       *upnp.IGD
	upnpTcpPort          int
	upnpUdpPort          int
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
		Address:              listenAddr,
		RootDirectory:        rootDir,
		EnableUpnp:           true,
		StatsInterval:        DEFAULT_STATS_INTERVAL,
		ImportInterval:       DEFAULT_IMPORT_INTERVAL,
		UpnpDiscoveryTimeout: DEFAULT_UPNP_DISCOVERY_TIMEOUT,
		UpnpMappingDuration:  DEFAULT_UPNP_MAPPING_DURATION,
	}, nil
}

func (self *Peer) ImportDirectory(name string) error {
	return filepath.Walk(name, func(entryPath string, info os.FileInfo, err error) error {
		// if we want to make this file available...
		// 		load file metadata
		//
		return nil
	})
}

func (self *Peer) Run() error {
	if self.EnableUpnp {
		parts := strings.Split(self.Address, `:`)

		if len(parts) > 0 {
			if v, err := stringutil.ConvertToInteger(parts[len(parts)-1]); err == nil {
				// search for Internet Gateway Devices, and talk to the first one
				// (this is the 99% case for home gamers)
				//
				for _, device := range upnp.Discover(0, self.UpnpDiscoveryTimeout) {

					// add TCP port mapping
					if port, err := device.AddPortMapping(nat.TCP,
						int(v), int(v), BF_UPNP_SERVICE_NAME, self.UpnpMappingDuration); err == nil {
						self.upnpTcpPort = port
						log.Infof("Added UPnP port mapping for tcp:%d", port)
					} else {
						log.Errorf("Failed to add UPnP port mapping for tcp:%d", port)
					}

					// add UDP port mapping
					if port, err := device.AddPortMapping(nat.UDP,
						int(v), int(v), BF_UPNP_SERVICE_NAME, self.UpnpMappingDuration); err == nil {
						self.upnpUdpPort = port
						log.Infof("Added UPnP port mapping for udp:%d", port)
					} else {
						log.Errorf("Failed to add UPnP port mapping for udp:%d", port)
					}

					// and we shall call this gateway our default gateway
					self.defaultGateway = device.(*upnp.IGD)
					break
				}
			} else {
				return err
			}

			log.Infof("Listening on %s:%d/tcp", parts[0], self.upnpTcpPort)
			log.Infof("Listening on %s:%d/udp", parts[0], self.upnpUdpPort)
		}
	}

	errchan := make(chan error)

	go self.startPeriodicImport()

	select {
	case err := <-errchan:
		return err
	}

	return nil
}

func (self *Peer) Close() chan bool {
	done := make(chan bool)

	go func() {
		if self.upnpTcpPort > 0 {
			log.Infof("Releasing UPnP port %d/tcp", self.upnpTcpPort)
			if err := self.defaultGateway.DeletePortMapping(nat.TCP, self.upnpTcpPort); err != nil {
				log.Errorf("Failed to release port: %v", err)
			}
		}

		if self.upnpUdpPort > 0 {
			log.Infof("Releasing UPnP port %d/udp", self.upnpUdpPort)
			if err := self.defaultGateway.DeletePortMapping(nat.UDP, self.upnpUdpPort); err != nil {
				log.Errorf("Failed to release port: %v", err)
			}
		}

		done <- true
	}()

	return done
}

func (self *Peer) startPeriodicImport() {
	if self.ImportInterval > 0 {
		log.Debugf("Rechecking %s for new files every %s",
			self.RootDirectory, self.ImportInterval)

		for {
			select {
			case <-time.After(self.ImportInterval):
				self.ImportDirectory(self.RootDirectory)
			}
		}
	}
}
