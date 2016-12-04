package peer

import (
	"fmt"
	"github.com/ghetzel/byteflood/util"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/hashicorp/yamux"
	"github.com/op/go-logging"
	"github.com/satori/go.uuid"
	"io"
	"net"
	"time"
)

const (
	DEFAULT_UPNP_DISCOVERY_TIMEOUT = (30 * time.Second)
	DEFAULT_UPNP_MAPPING_DURATION  = (8760 * time.Hour)
	BF_UPNP_SERVICE_NAME           = `byteflood`
)

var log = logging.MustGetLogger(`byteflood.client`)
var logproxy = util.NewLogProxy(`byteflood.client`, `info`)

type Peer struct {
	EnableUpnp           bool
	Address              string
	Port                 int
	UpnpMappingDuration  time.Duration
	UpnpDiscoveryTimeout time.Duration
	id                   uuid.UUID
	defaultGateway       *IGD
	sessions             map[uuid.UUID]*yamux.Session
}

func CreatePeer(id string, addr string, port int) (*Peer, error) {
	if port == 0 {
		if randomSocket, err := net.Listen(`tcp6`, `:0`); err == nil {
			_, p, _ := net.SplitHostPort(randomSocket.Addr().String())

			if v, err := stringutil.ConvertToInteger(p); err == nil {
				port = int(v)
			} else {
				return nil, err
			}

			if err := randomSocket.Close(); err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	var localID uuid.UUID

	if id == `` {
		localID = uuid.NewV4()
	} else {
		if i, err := uuid.FromString(id); err == nil {
			localID = i
		} else {
			return nil, err
		}
	}

	return &Peer{
		Address:              addr,
		Port:                 port,
		EnableUpnp:           true,
		UpnpDiscoveryTimeout: DEFAULT_UPNP_DISCOVERY_TIMEOUT,
		UpnpMappingDuration:  DEFAULT_UPNP_MAPPING_DURATION,
		id:                   localID,
		sessions:             make(map[uuid.UUID]*yamux.Session),
	}, nil
}

func (self *Peer) Listen() error {
	if self.EnableUpnp {
		if self.Port > 0 {
			log.Infof("Setting up UPnP port mapping...")

			if devices, err := DiscoverGateways(self.UpnpDiscoveryTimeout); err == nil {
				// search for Internet Gateway Devices, and talk to the first one
				// (this is the 99% case for home gamers)
				//
				for _, device := range devices {
					// add TCP port mapping
					if err := device.AddPortMapping(`tcp`,
						self.Port, self.Port, BF_UPNP_SERVICE_NAME, self.UpnpMappingDuration); err == nil {
					} else {
						log.Errorf("Failed to add UPnP port mapping for %d/tcp", self.Port)
					}

					// and we shall call this gateway our default gateway
					self.defaultGateway = device
					break
				}
			} else {
				return err
			}
		}
	}

	if listener, err := net.Listen(`tcp`, fmt.Sprintf("%s:%d", self.Address, self.Port)); err == nil {
		log.Infof("Listening on %s:%d", self.Address, self.Port)

		for {
			if conn, err := listener.Accept(); err == nil {
				log.Debugf("Got connection request from %s", conn.RemoteAddr().String())

				id := make([]byte, 16, 16)
				conn.SetDeadline(time.Now().Add(10 * time.Second))

				// read 16 bytes from client, which should be their UUID
				if n, err := conn.Read(id); err == nil && n <= 16 {
					if clientID, err := uuid.FromBytes(id); err == nil {
						if err := self.CreateSession(clientID, conn); err == nil {
							log.Infof("Session created for peer %s", clientID.String())
						} else {
							log.Errorf("Connection error: failed to create session - %v", err)
						}
					} else {
						log.Errorf("Connection error: invalid preamble - %v", err)
					}
				} else {
					log.Errorf("Connection error: incomplete preamble - %v", err)
				}
			} else {
				log.Errorf("Connection error: %v", err)
			}
		}
	} else {
		return err
	}

	return nil
}

func (self *Peer) CreateSession(id uuid.UUID, conn io.ReadWriteCloser) error {
	if _, ok := self.sessions[id]; !ok {
		if session, err := yamux.Server(conn, &yamux.Config{
			AcceptBacklog:          16,
			EnableKeepAlive:        true,
			KeepAliveInterval:      (10 * time.Second),
			ConnectionWriteTimeout: (500 * time.Millisecond),
		}); err == nil {
			self.sessions[id] = session
		} else {
			return err
		}
	}

	return nil
}

func (self *Peer) Close() chan bool {
	done := make(chan bool)

	go func() {
		if self.EnableUpnp && self.Port > 0 && self.defaultGateway != nil {
			log.Infof("Releasing UPnP port %d/tcp", self.Port)
			if err := self.defaultGateway.DeletePortMapping(`tcp`, self.Port); err != nil {
				log.Errorf("Failed to release port: %v", err)
			}
		}

		done <- true
	}()

	return done
}
