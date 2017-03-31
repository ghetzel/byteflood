package peer

import (
	"bytes"
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/encryption"
	"github.com/ghetzel/byteflood/util"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/jbenet/go-base58"
	"github.com/op/go-logging"
	"github.com/orcaman/concurrent-map"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	DEFAULT_PEER_SERVER_ADDRESS    = `:0`
	DEFAULT_UPNP_DISCOVERY_TIMEOUT = (30 * time.Second)
	DEFAULT_UPNP_MAPPING_DURATION  = (8760 * time.Hour)
	BF_UPNP_SERVICE_NAME           = `byteflood`
	BF_ANON_PEER_NAME              = `anonymous`
)

var log = logging.MustGetLogger(`byteflood/peer`)
var logproxy = util.NewLogProxy(`byteflood/peer`, `info`)

var PeerMonitorRetryMultiplier = 2
var PeerMonitorRetryMultiplierMax = 512
var PeerMonitorRetryMax = -1

type Peer interface {
	ID() string
	String() string
	GetPublicKey() []byte
}

type LocalPeer struct {
	Peer                 `json:"-"`
	EnableUpnp           bool          `json:"upnp,omitempty"`
	Address              string        `json:"address,omitempty"`
	Autoconnect          bool          `json:"autoconnect"`
	AutoconnectPeers     []string      `json:"autoconnect_peers,omitempty"`
	PublicKey            []byte        `json:"-"`
	PrivateKey           []byte        `json:"-"`
	UpnpMappingDuration  time.Duration `json:"upnp_mapping_duration,omitempty"`
	UpnpDiscoveryTimeout time.Duration `json:"upnp_discovery_timeout,omitempty"`
	UploadCap            int           `json:"upload_cap,omitempty"`
	DownloadCap          int           `json:"download_cap,omitempty"`
	autoReceiveMessages  bool
	port                 int
	upnpPortMapping      *PortMapping
	sessions             cmap.ConcurrentMap
	listening            chan bool
	peerServer           *PeerServer
	peerRequestHandler   http.Handler
	db                   *db.Database
}

func NewLocalPeer(db *db.Database) *LocalPeer {
	return &LocalPeer{
		Address:              DEFAULT_PEER_SERVER_ADDRESS,
		Autoconnect:          true,
		EnableUpnp:           false,
		UpnpDiscoveryTimeout: DEFAULT_UPNP_DISCOVERY_TIMEOUT,
		UpnpMappingDuration:  DEFAULT_UPNP_MAPPING_DURATION,
		autoReceiveMessages:  true,
		sessions:             cmap.New(),
		listening:            make(chan bool),
		db:                   db,
	}
}

func (self *LocalPeer) Initialize() error {
	// get ephemeral port for remote connections
	if a, p, err := net.SplitHostPort(self.Address); err == nil {
		if p == `0` {
			if eport, err := ephemeralPort(); err == nil {
				self.Address = fmt.Sprintf("%s:%d", a, eport)
			} else {
				return err
			}
		}
	} else {
		return err
	}

	// create peer server instance
	self.peerServer = NewPeerServer(self)

	// load all authorized peers and populate AutoconnectPeers with their known addresses
	if err := self.db.AuthorizedPeers.Each(AuthorizedPeer{}, func(v interface{}) {
		if peer, ok := v.(*AuthorizedPeer); ok {
			if peer.Addresses != `` {
				self.AutoconnectPeers = append(self.AutoconnectPeers, peer.GetAddresses()...)
			}
		}
	}); err != nil {
		return err
	}

	return nil
}

func (self *LocalPeer) ID() string {
	return base58.Encode(self.PublicKey[:])
}

func (self *LocalPeer) String() string {
	return self.ID()
}

func (self *LocalPeer) GetPublicKey() []byte {
	return self.PublicKey
}

func (self *LocalPeer) WaitListen() <-chan bool {
	return self.listening
}

func (self *LocalPeer) SetPeerRequestHandler(handler http.Handler) {
	self.peerRequestHandler = handler
}

func (self *LocalPeer) AddAuthorizedPeer(peerID string, name string) error {
	peer := &AuthorizedPeer{
		ID:       peerID,
		PeerName: name,
	}

	if !self.db.AuthorizedPeers.Exists(peerID) {
		return self.db.AuthorizedPeers.Create(peer)
	} else {
		return fmt.Errorf("Peer %s already exists", peerID)
	}
}

func (self *LocalPeer) Run() error {
	log.Infof("Local Peer ID: %s", self.ID())

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
	if self.EnableUpnp {
		if _, p, err := net.SplitHostPort(self.Address); err == nil {
			if v, err := stringutil.ConvertToInteger(p); err == nil {
				port := int(v)

				if port > 0 {
					log.Infof("Setting up UPnP port mapping...")

					if devices, err := DiscoverGateways(self.UpnpDiscoveryTimeout); err == nil {
						// search for Internet Gateway Devices, and talk to the first one
						// (this is the 99% case for home gamers)
						//
						for _, device := range devices {
							// add TCP port mapping
							if mapping, err := device.AddPortMapping(`tcp`, port, port, BF_UPNP_SERVICE_NAME, self.UpnpMappingDuration); err == nil {
								log.Debugf("Adding port mapping: %s", mapping.String())

								// and we shall call this gateway our default gateway
								self.upnpPortMapping = mapping
								break
							} else {
								log.Errorf("Failed to add UPnP port mapping for %d/tcp", port)
							}

						}

						if self.upnpPortMapping == nil {
							if len(devices) == 0 {
								return fmt.Errorf("Failed to map port %d/tcp: no UPnP gateways discovered", port)
							} else {
								return fmt.Errorf("Failed to map port %d/tcp via any discovered gateway", port)
							}
						}
					} else {
						return err
					}
				}
			} else {
				return err
			}
		} else {
			return err
		}
	}

	if addr, err := net.ResolveTCPAddr(`tcp`, self.Address); err == nil {
		if listener, err := net.ListenTCP(`tcp`, addr); err == nil {
			log.Infof("Listening on %s", self.Address)

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

func (self *LocalPeer) IsLoopbackConnection(destination string) bool {
	if _, localPort, err := net.SplitHostPort(self.Address); err == nil {
		if localNets, err := net.InterfaceAddrs(); err == nil {
			for _, localNet := range localNets {
				addr, _ := SplitAddrCIDR(localNet.String())
				localAddr := fmt.Sprintf("%s:%s", addr, localPort)

				if destination == localAddr {
					return true
				}
			}
		}
	}

	return false
}

func (self *LocalPeer) PermitConnectionFrom(conn net.Conn) bool {
	return true
}

func (self *LocalPeer) RunPeerServer() error {
	if p, err := ephemeralPort(); err == nil {
		// run peer server (long running)
		return self.peerServer.Serve(fmt.Sprintf("127.0.0.1:%d", p), self.peerRequestHandler)
	} else {
		return err
	}
}

func (self *LocalPeer) GetPeers() []*RemotePeer {
	peers := make([]*RemotePeer, 0)

	self.sessions.IterCb(func(id string, v interface{}) {
		if remotePeer, ok := v.(*RemotePeer); ok {
			peers = append(peers, remotePeer)
		}
	})

	return peers
}

func (self *LocalPeer) GetPeersByKey(publicKey []byte) []*RemotePeer {
	peers := make([]*RemotePeer, 0)

	for _, remotePeer := range self.GetPeers() {
		if bytes.Compare(publicKey, remotePeer.GetPublicKey()) == 0 {
			peers = append(peers, remotePeer)
		}
	}

	return peers
}

func (self *LocalPeer) GetPeersInGroup(nameOrGroup string) ([]*RemotePeer, error) {
	peers := make([]*RemotePeer, 0)
	var authPeersInGroup []*AuthorizedPeer
	query := make(map[string]interface{})

	// peer groups are given to us as @-prefixed strings
	if strings.HasPrefix(nameOrGroup, `@`) {
		query[`group`] = strings.TrimPrefix(nameOrGroup, `@`)
	} else {
		query[`name`] = nameOrGroup
	}

	if f, err := db.ParseFilter(query); err == nil {
		if err := self.db.AuthorizedPeers.Find(f, &authPeersInGroup); err == nil {
			for _, remotePeer := range self.GetPeers() {
				for _, authPeer := range authPeersInGroup {
					if remotePeer.ID == authPeer.ID {
						peers = append(peers, remotePeer)
						break
					}
				}
			}
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}

	return peers, nil
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
			if err := GenerateAndWritePeeringRequest(conn, self, pReq.SessionID); err == nil {
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
		if err := GenerateAndWritePeeringRequest(conn, self, nil); err == nil {
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
		log.Debugf("Peering request exchange completed: session is %v", remotePeeringRequest)

		if remotePeer, err := NewRemotePeerFromRequest(remotePeeringRequest, conn); err == nil {
			// if a record exists for this peer ID, their name will be added to the remotePeer instance
			if err := self.db.AuthorizedPeers.Get(remotePeer.ID, remotePeer); err != nil {
				log.Errorf("rejecting unknown peer %s (%s)", remotePeer.ID, remotePeer.String())
				return nil, err
			}

			// setup stream encryption & decryption
			remotePeer.Encryption = encryption.NewCryptoboxEncryption(
				remotePeeringRequest.PublicKey[:32],
				self.PrivateKey[:32],
				nil,
				nil,
			)

			self.sessions.Set(remotePeer.SessionID(), remotePeer)

			// setup download rate limiting
			if self.DownloadCap > 0 {
				remotePeer.SetReadRateLimit(self.DownloadCap, self.DownloadCap)
				log.Debugf("  Downloads capped at %d Bps", self.DownloadCap)
			}

			// setup upload rate limiting
			if self.UploadCap > 0 {
				remotePeer.SetWriteRateLimit(self.UploadCap, self.UploadCap)
				log.Debugf("  Uploads capped at %d Bps", self.UploadCap)
			}

			// start automatically receiving messages from this peer if we're supposed to
			if self.autoReceiveMessages {
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

func (self *LocalPeer) GetSessions() []*RemotePeer {
	peers := make([]*RemotePeer, 0)

	self.sessions.IterCb(func(id string, v interface{}) {
		if peer, ok := v.(*RemotePeer); ok {
			peers = append(peers, peer)
		}
	})

	return peers
}

func (self *LocalPeer) GetSession(sessionOrName string) (*RemotePeer, bool) {
	if v, ok := self.sessions.Get(sessionOrName); ok {
		if peer, ok := v.(*RemotePeer); ok {
			return peer, true
		}
	}

	remotePeers := make([]*RemotePeer, 0)

	self.sessions.IterCb(func(id string, v interface{}) {
		if peer, ok := v.(*RemotePeer); ok {
			if peer.Name == sessionOrName {
				remotePeers = append(remotePeers, peer)
			}
		}
	})

	if len(remotePeers) == 1 {
		return remotePeers[0], true
	}

	return nil, false
}

func (self *LocalPeer) RemovePeer(sessionId string) error {
	if v, ok := self.sessions.Get(sessionId); ok {
		if remotePeer, ok := v.(*RemotePeer); ok {
			log.Errorf("Disconnected from %s", remotePeer.String())

			err := remotePeer.Disconnect()

			if err != nil {
				log.Errorf("Error disconnecting from peer: %v", err)
			}

			self.sessions.Remove(sessionId)

			return err
		} else {
			return fmt.Errorf("corrupt session '%s'", sessionId)
		}
	} else {
		return fmt.Errorf("unknown session '%s'", sessionId)
	}
}

func (self *LocalPeer) ConnectTo(address string) (*RemotePeer, error) {
	// make sure we're not trying to connect to ourself
	if self.IsLoopbackConnection(address) {
		return nil, fmt.Errorf(BF_ERR_LOOPBACK_CONN)
	}

	log.Debugf("Connecting to %s", address)

	// open a TCP socket to the given addr/port
	if raddr, err := net.ResolveTCPAddr(`tcp`, address); err == nil {
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

func (self *LocalPeer) ConnectToAndMonitor(address string) {
	if strings.TrimSpace(address) == `` {
		return
	}

	retryWaitSec := 1
	retries := 0

	for retries = 0; PeerMonitorRetryMax < 0 || retries < PeerMonitorRetryMax; retries++ {
		if remotePeer, err := self.ConnectTo(address); err == nil {
			retryWaitSec = 1
			retries = 0

			for {
				if err := remotePeer.Heartbeat(); err != nil {
					log.Warningf("[%s] Heartbeat to peer failed: %v", remotePeer.String(), err)
					break
				}

				time.Sleep(remotePeer.HeartbeatInterval)
			}

			// heartbeat failed, disconnect from peer
			remotePeer.Disconnect()

		} else if IsUnknownPeerErr(err) {
			log.Warning(err)
			return
		} else if IsAlreadyConnectedErr(err) {
			log.Warning(err)
			return
		} else if IsLoopbackConnectionErr(err) {
			log.Warning(err)
			return
		}

		time.Sleep(time.Duration(retryWaitSec) * time.Second)

		// exponential backoff up to a maximum delay
		if v := (retryWaitSec * PeerMonitorRetryMultiplier); v <= PeerMonitorRetryMultiplierMax {
			retryWaitSec *= PeerMonitorRetryMultiplier
		}
	}

	log.Errorf("Connection to peer at %s has failed after, %d attempts", address, retries)
}

func (self *LocalPeer) ConnectToAll() error {
	if self.Autoconnect {
		for _, peerAddr := range self.AutoconnectPeers {
			go self.ConnectToAndMonitor(peerAddr)
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
