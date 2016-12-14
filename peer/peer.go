package peer

import (
	"fmt"
	"github.com/ghetzel/byteflood/encryption"
	"github.com/ghetzel/byteflood/util"
	"github.com/op/go-logging"
	"github.com/satori/go.uuid"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	DEFAULT_SERVICE_ADDR           = `:0`
	DEFAULT_PEER_SERVER_PORT       = 0
	DEFAULT_UPNP_DISCOVERY_TIMEOUT = (30 * time.Second)
	DEFAULT_UPNP_MAPPING_DURATION  = (8760 * time.Hour)
	BF_UPNP_SERVICE_NAME           = `byteflood`
)

var log = logging.MustGetLogger(`byteflood.peer`)
var logproxy = util.NewLogProxy(`byteflood.peer`, `info`)

type Peer interface {
	ID() string
	UUID() uuid.UUID
	String() string
	GetPublicKey() []byte
}

type LocalPeer struct {
	Peer
	EnableUpnp             bool
	Address                string
	ServiceAddress         string
	Port                   int
	UpnpMappingDuration    time.Duration
	UpnpDiscoveryTimeout   time.Duration
	UploadBytesPerSecond   int
	DownloadBytesPerSecond int
	AutoReceiveMessages    bool
	id                     uuid.UUID
	upnpPortMapping        *PortMapping
	sessions               map[string]*RemotePeer
	listening              chan bool
	publicKey              []byte
	privateKey             []byte
	serviceProxy           *Server
}

func CreatePeer(id string, publicKey []byte, privateKey []byte) (*LocalPeer, error) {
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

	log.Infof("Local Peer ID: %s", localID.String())

	return &LocalPeer{
		Port:                 DEFAULT_PEER_SERVER_PORT,
		ServiceAddress:       DEFAULT_SERVICE_ADDR,
		EnableUpnp:           false,
		UpnpDiscoveryTimeout: DEFAULT_UPNP_DISCOVERY_TIMEOUT,
		UpnpMappingDuration:  DEFAULT_UPNP_MAPPING_DURATION,
		AutoReceiveMessages:  true,
		id:                   localID,
		sessions:             make(map[string]*RemotePeer),
		listening:            make(chan bool),
		publicKey:            publicKey,
		privateKey:           privateKey,
	}, nil
}

func (self *LocalPeer) ID() string {
	return self.id.String()
}

func (self *LocalPeer) String() string {
	return self.ID()
}

func (self *LocalPeer) UUID() uuid.UUID {
	return self.id
}

func (self *LocalPeer) GetPublicKey() []byte {
	return self.publicKey
}

func (self *LocalPeer) WaitListen() <-chan bool {
	return self.listening
}

func (self *LocalPeer) Listen() error {
	// start the service proxy
	if err := self.StartServiceProxy(); err != nil {
		return err
	}

	if self.Port == 0 {
		if p, err := ephemeralPort(); err == nil {
			self.Port = p
		} else {
			return err
		}
	}

	if self.EnableUpnp && self.Port > 0 {
		log.Infof("Setting up UPnP port mapping...")

		if devices, err := DiscoverGateways(self.UpnpDiscoveryTimeout); err == nil {
			// search for Internet Gateway Devices, and talk to the first one
			// (this is the 99% case for home gamers)
			//
			for _, device := range devices {
				// add TCP port mapping
				if mapping, err := device.AddPortMapping(`tcp`, self.Port, self.Port, BF_UPNP_SERVICE_NAME, self.UpnpMappingDuration); err == nil {
					log.Debugf("Adding port mapping: %s", mapping.String())

					// and we shall call this gateway our default gateway
					self.upnpPortMapping = mapping
					break
				} else {
					log.Errorf("Failed to add UPnP port mapping for %d/tcp", self.Port)
				}

			}

			if self.upnpPortMapping == nil {
				if len(devices) == 0 {
					return fmt.Errorf("Failed to map port %d/tcp: no UPnP gateways discovered", self.Port)
				} else {
					return fmt.Errorf("Failed to map port %d/tcp via any discovered gateway", self.Port)
				}
			}
		} else {
			return err
		}
	}

	if addr, err := net.ResolveTCPAddr(`tcp`, fmt.Sprintf("%s:%d", self.Address, self.Port)); err == nil {
		if listener, err := net.ListenTCP(`tcp`, addr); err == nil {
			log.Infof("Listening on %s:%d", self.Address, self.Port)

			// tell anyone who cares that we're ready to start accepting connections now
			select {
			case self.listening <- true:
				break
			default:
				break
			}

			for {
				// accept new connections
				if conn, err := listener.AcceptTCP(); err == nil {
					log.Debugf("Got connection request from %s", conn.RemoteAddr().String())

					// verify that we want to proceed with this connection...
					if self.PermitConnectionFrom(conn) {
						// perform peer handshake and registration
						if _, err := self.RegisterPeer(conn, true); err == nil {
							// this skips the fail-closed disconnect below
							continue
						} else {
							log.Errorf("Connection from %s failed: %v", conn.RemoteAddr().String(), err)
						}
					} else {
						log.Warningf("Connection from %s is prohibited", conn.RemoteAddr().String())
					}

					// successful connections will skip this, everything else will close
					log.Errorf("Closing connection to %s", conn.RemoteAddr().String())

					if err := conn.Close(); err != nil {
						log.Errorf("Failed to close connection %s: %v", conn.RemoteAddr().String(), err)
					}
				} else {
					log.Errorf("Connection error: %v", err)
				}
			}
		} else {
			return err
		}
	} else {
		return err
	}

	return nil
}

func (self *LocalPeer) PermitConnectionFrom(conn net.Conn) bool {
	return true
}

func (self *LocalPeer) StartServiceProxy() error {
	// local API/proxy service
	if strings.HasSuffix(self.ServiceAddress, `:0`) {
		if p, err := ephemeralPort(); err == nil {
			self.ServiceAddress = fmt.Sprintf(
				"%s:%d",
				strings.TrimSuffix(self.ServiceAddress, `:0`),
				p,
			)
		} else {
			return err
		}
	}

	self.serviceProxy = NewServer(self, self.ServiceAddress)
	if err := self.serviceProxy.Serve(); err != nil {
		return err
	}

	return nil
}

func (self *LocalPeer) Server() *Server {
	return self.serviceProxy
}

func (self *LocalPeer) ServiceRequest(remotePeer *RemotePeer, request *http.Request) (*http.Response, error) {
	request.URL.Host = self.ServiceAddress
	request.Header.Set(`X-Byteflood-PeerID`, remotePeer.ID())

	// TODO: only permit certain paths (e.g.: remote peers should not be able to query all of my peers)

	log.Debugf("[%s] Proxying request: %s %s", remotePeer.ID(), request.Method, request.URL.String())
	return http.DefaultClient.Do(request)
}

func (self *LocalPeer) RegisterPeer(conn *net.TCPConn, remoteInitiated bool) (*RemotePeer, error) {
	var remotePeeringRequest *PeeringRequest

	// if the remote side initiated the request, they have already written the peering
	// request and we need to read that and reply.
	//
	if remoteInitiated {
		// read, then write

		log.Debugf("Got new peering request, parsing...")
		if pReq, err := ParsePeeringRequest(conn); err == nil {
			log.Debugf("Replying with our peering request...")
			if err := GenerateAndWritePeeringRequest(conn, self); err == nil {
				remotePeeringRequest = pReq
			} else {
				return nil, err
			}
		} else {
			return nil, err
		}
	} else {
		// write, then read
		log.Debugf("Sending out peering request...")
		if err := GenerateAndWritePeeringRequest(conn, self); err == nil {
			log.Debugf("Reading reply peering request...")
			if pReq, err := ParsePeeringRequest(conn); err == nil {
				remotePeeringRequest = pReq
			} else {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	// Common to both sides of the connection...
	// -----------------------------------------

	if remotePeeringRequest != nil {
		if remotePeer, err := NewRemotePeerFromRequest(remotePeeringRequest, conn); err == nil {
			// setup encryption and decryption
			//

			remotePeer.Encrypter = encryption.NewEncrypter(
				remotePeeringRequest.PublicKey[:32],
				self.privateKey[:32],
				nil,
			)

			remotePeer.Decrypter = encryption.NewDecrypter(
				remotePeeringRequest.PublicKey[:32],
				self.privateKey[:32],
				nil,
			)

			self.sessions[remotePeer.String()] = remotePeer

			// setup download rate limiting
			if self.DownloadBytesPerSecond > 0 {
				remotePeer.SetReadRateLimit(self.DownloadBytesPerSecond, self.DownloadBytesPerSecond)
				log.Debugf("  Downloads capped at %d Bps", self.DownloadBytesPerSecond)
			}

			// setup upload rate limiting
			if self.UploadBytesPerSecond > 0 {
				remotePeer.SetWriteRateLimit(self.UploadBytesPerSecond, self.UploadBytesPerSecond)
				log.Debugf("  Uploads capped at %d Bps", self.UploadBytesPerSecond)
			}

			// start automatically receiving messages from this peer if we're supposed to
			if self.AutoReceiveMessages {
				go remotePeer.ReceiveMessages(self)
			}

			log.Debugf("Remote peer %s registered", remotePeer.String())
			return remotePeer, nil
		} else {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("Could not read remote peering request")
	}
}

func (self *LocalPeer) GetPeer(id string) (*RemotePeer, bool) {
	remotePeer, ok := self.sessions[id]
	return remotePeer, ok
}

func (self *LocalPeer) RemovePeer(id string) {
	if remotePeer, ok := self.sessions[id]; ok {
		log.Errorf("Removing peer %s", remotePeer.String())

		if err := remotePeer.Disconnect(); err != nil {
			log.Errorf("Error disconnecting from peer: %v", err)
		}

		delete(self.sessions, id)

		log.Debugf("%d peers registered", len(self.sessions))
	}
}

func (self *LocalPeer) ConnectTo(addr string, port int) (*RemotePeer, error) {
	// open a TCP socket to the given addr/port
	if raddr, err := net.ResolveTCPAddr(`tcp`, fmt.Sprintf("%s:%d", addr, port)); err == nil {
		if conn, err := net.DialTCP(`tcp`, nil, raddr); err == nil {
			// perform peer handshake and registration
			if remotePeer, err := self.RegisterPeer(conn, false); err == nil {
				return remotePeer, nil
			} else {
				if err := conn.Close(); err != nil {
					log.Errorf("Failed to close connection %s: %v", conn.RemoteAddr().String(), err)
				}

				return nil, err
			}
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}

func (self *LocalPeer) Close() chan bool {
	done := make(chan bool)

	go func() {
		if self.EnableUpnp && self.upnpPortMapping != nil {
			log.Infof("Releasing UPnP mapping %s", self.upnpPortMapping.String())

			if err := self.upnpPortMapping.Delete(); err != nil {
				log.Errorf("Failed to release port: %v", err)
			}
		}

		done <- true
	}()

	return done
}
