package peer

import (
	"fmt"
	"github.com/ghetzel/byteflood/encryption"
	"github.com/ghetzel/byteflood/util"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/jbenet/go-base58"
	"github.com/op/go-logging"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	DEFAULT_PEER_SERVER_PORT       = 0
	DEFAULT_UPNP_DISCOVERY_TIMEOUT = (30 * time.Second)
	DEFAULT_UPNP_MAPPING_DURATION  = (8760 * time.Hour)
	BF_UPNP_SERVICE_NAME           = `byteflood`
)

var log = logging.MustGetLogger(`byteflood.peer`)
var logproxy = util.NewLogProxy(`byteflood.peer`, `info`)
var PeerMonitorRetryMultiplier = 2
var PeerMonitorRetryMultiplierMax = 512
var PeerMonitorRetryMax = -1

type Peer interface {
	ID() string
	String() string
	GetPublicKey() []byte
}

type LocalPeer struct {
	Peer
	EnableUpnp             bool
	Address                string
	Port                   int
	PeerAddresses          []string
	UpnpMappingDuration    time.Duration
	UpnpDiscoveryTimeout   time.Duration
	UploadBytesPerSecond   int
	DownloadBytesPerSecond int
	AutoReceiveMessages    bool
	upnpPortMapping        *PortMapping
	sessions               map[string]*RemotePeer
	sessionLock            sync.Mutex
	listening              chan bool
	publicKey              []byte
	privateKey             []byte
	peerServer             *PeerServer
	peerRequestHandler     http.Handler
}

func CreatePeer(publicKey []byte, privateKey []byte) (*LocalPeer, error) {
	return &LocalPeer{
		Port:                 DEFAULT_PEER_SERVER_PORT,
		EnableUpnp:           false,
		UpnpDiscoveryTimeout: DEFAULT_UPNP_DISCOVERY_TIMEOUT,
		UpnpMappingDuration:  DEFAULT_UPNP_MAPPING_DURATION,
		AutoReceiveMessages:  true,
		sessions:             make(map[string]*RemotePeer),
		listening:            make(chan bool),
		publicKey:            publicKey,
		privateKey:           privateKey,
	}, nil
}

func (self *LocalPeer) ID() string {
	return base58.Encode(self.publicKey[:])
}

func (self *LocalPeer) String() string {
	return self.ID()
}

func (self *LocalPeer) GetPublicKey() []byte {
	return self.publicKey
}

func (self *LocalPeer) WaitListen() <-chan bool {
	return self.listening
}

func (self *LocalPeer) SetPeerRequestHandler(handler http.Handler) {
	self.peerRequestHandler = handler
}

func (self *LocalPeer) Run() error {
	log.Infof("Local Peer ID: %s", self.ID())

	if self.Port == 0 {
		if p, err := ephemeralPort(); err == nil {
			self.Port = p
		} else {
			return err
		}
	}

	errchan := make(chan error)

	// start the peer server (HTTP-over-cryptotransport)
	go func() {
		errchan <- self.RunPeerServer()
	}()

	// start listening for new connections
	go func() {
		errchan <- self.RunPeerListener()
	}()

	// reach out and connect to our peers
	go func() {
		if err := self.ConnectToAll(); err != nil {
			errchan <- err
		}
	}()

	select {
	case err := <-errchan:
		return err
	}
}

func (self *LocalPeer) RunPeerListener() error {
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

func (self *LocalPeer) RunPeerServer() error {
	if p, err := ephemeralPort(); err == nil {
		self.peerServer = NewPeerServer(self, fmt.Sprintf("127.0.0.1:%d", p), self.peerRequestHandler)

		// run peer server (long running)
		return self.peerServer.Serve()
	} else {
		return err
	}
}

func (self *LocalPeer) PeerServer() *PeerServer {
	return self.peerServer
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
		log.Debugf("Sending peering request to %s", conn.RemoteAddr().String())
		if err := GenerateAndWritePeeringRequest(conn, self); err == nil {
			log.Debugf("Reading peering request reply from %s", conn.RemoteAddr().String())
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

			self.sessionLock.Lock()
			self.sessions[remotePeer.String()] = remotePeer
			self.sessionLock.Unlock()

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

			return remotePeer, nil
		} else {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("Could not read remote peering request")
	}
}

func (self *LocalPeer) GetPeers() []*RemotePeer {
	peers := make([]*RemotePeer, 0)

	for _, peer := range self.sessions {
		peers = append(peers, peer)
	}

	return peers
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
	log.Debugf("Connecting to %s:%d", addr, port)

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

func (self *LocalPeer) ConnectToAndMonitor(addr string, port int) {
	retryWaitSec := 1
	retries := 0

	for retries = 0; PeerMonitorRetryMax < 0 || retries < PeerMonitorRetryMax; retries++ {
		if remotePeer, err := self.ConnectTo(addr, port); err == nil {
			retryWaitSec = 1
			retries = 0

			for {
				if err := remotePeer.Heartbeat(); err != nil {
					log.Warningf("[%s] Heartbeat to peer failed: %v", remotePeer.ID(), err)
					break
				}

				time.Sleep(remotePeer.HeartbeatInterval)
			}
		}

		time.Sleep(time.Duration(retryWaitSec) * time.Second)

		// exponential backoff up to a maximum delay
		if v := (retryWaitSec * PeerMonitorRetryMultiplier); v <= PeerMonitorRetryMultiplierMax {
			retryWaitSec *= PeerMonitorRetryMultiplier
		}
	}

	log.Errorf("Connection to peer at %s:%d has failed after, %d attempts", addr, port, retries)
}

func (self *LocalPeer) ConnectToAll() error {
	for _, peerAddr := range self.PeerAddresses {
		if addr, port, err := net.SplitHostPort(peerAddr); err == nil {
			if v, err := stringutil.ConvertToInteger(port); err == nil {
				go self.ConnectToAndMonitor(addr, int(v))
			} else {
				return err
			}
		} else {
			return err
		}
	}

	return nil
}

func (self *LocalPeer) Stop() chan bool {
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
