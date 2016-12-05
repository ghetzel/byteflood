package peer

import (
	"fmt"
	"github.com/ghetzel/byteflood/util"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/hashicorp/yamux"
	"github.com/op/go-logging"
	"github.com/satori/go.uuid"
	"github.com/vmihailenco/msgpack"
	"io"
	"net"
	"bytes"
	"time"
)

const (
	DEFAULT_UPNP_DISCOVERY_TIMEOUT = (30 * time.Second)
	DEFAULT_UPNP_MAPPING_DURATION  = (8760 * time.Hour)
	BF_UPNP_SERVICE_NAME           = `byteflood`
)

var log = logging.MustGetLogger(`byteflood.client`)
var logproxy = util.NewLogProxy(`byteflood.client`, `info`)
var PeeringRequestMaxInitialRead = 32768

type PeeringRequest struct {
	ID        []byte
	PublicKey []byte
}

func (self *PeeringRequest) WriteTo(w io.Writer) (int, error) {
	if data, err := msgpack.Marshal(peeringRequest); err == nil {
		return w.Write(data)
	}else{
		return -1, err
	}
}

func ParsePeeringRequest(r io.Reader) (*PeeringRequest, error) {
    peeringRequest := PeeringRequest{}

    if err := msgpack.NewDecoder(r).Decode(&peeringRequest); err == nil {
        return peeringRequest, nil
    }else{
        return nil, err
    }
}

type Peer interface {
	ID() []byte
	GetPeeringRequest() PeeringRequest
}

type LocalPeer struct {
	Peer
	EnableUpnp           bool
	Address              string
	Port                 int
	UpnpMappingDuration  time.Duration
	UpnpDiscoveryTimeout time.Duration
	id                   uuid.UUID
	upnpPortMapping      *PortMapping
	sessions             map[uuid.UUID]*RemotePeer
}

func CreatePeer(id string, addr string, port int) (*LocalPeer, error) {
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

	return &LocalPeer{
		Address:              addr,
		Port:                 port,
		EnableUpnp:           true,
		UpnpDiscoveryTimeout: DEFAULT_UPNP_DISCOVERY_TIMEOUT,
		UpnpMappingDuration:  DEFAULT_UPNP_MAPPING_DURATION,
		id:                   localID,
		sessions:             make(map[uuid.UUID]*RemotePeer),
	}, nil
}

func (self *LocalPeer) ID() []byte {
	return self.id.Bytes()
}

func (self *LocalPeer) Listen() error {
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
				return fmt.Errorf("Failed to map port %d/tcp via any discovered gateway", self.Port)
			}
		} else {
			return err
		}
	}

	if listener, err := net.Listen(`tcp`, fmt.Sprintf("%s:%d", self.Address, self.Port)); err == nil {
		log.Infof("Listening on %s:%d", self.Address, self.Port)

		for {
			// accept new connections
			if conn, err := listener.Accept(); err == nil {
				log.Debugf("Got connection request from %s", conn.RemoteAddr().String())

				// verify that we want to proceed with this connection...
				if self.PermitConnectionFrom(conn) {
					payload := make([]byte, 0, PeeringRequestMaxInitialRead)
					conn.SetDeadline(time.Now().Add(10 * time.Second))

					// read up to PeeringRequestMaxInitialRead bytes and attempt to decode this as a peering request
					if n, err := conn.Read(payload); err == nil && n <= PeeringRequestMaxInitialRead {
						buffer := bytes.NewBuffer(payload)

						if peeringRequest, err := ParsePeeringRequest(buffer); err == nil {
							if remotePeer, err := self.CreateSession(conn, &peeringRequest); err == nil {
								log.Infof("Session created for peer %s", remotePeer.String())

								// this skips the fallback close
								continue
							} else {
								log.Errorf("Connection error: failed to create session - %v", err)
							}
						} else {
							log.Errorf("Connection error: corrupt join request - %v", err)
						}
					} else {
						log.Errorf("Connection error: incomplete preamble - %v", err)
					}
				} else {
					log.Warningf("Connection from %s is prohibited", conn.RemoteAddr().String())
				}

				// successful connections will skip this, everything else will close
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

	return nil
}

func (self *LocalPeer) PermitConnectionFrom(conn net.Conn) bool {
	return true
}

func (self *LocalPeer) CreateSession(conn io.ReadWriteCloser, peeringRequest *PeeringRequest) (*RemotePeer, error) {
	if id, err := uuid.FromBytes(peeringRequest.ID); err == nil {
		if rp, ok := self.sessions[id]; !ok {
			if session, err := yamux.Server(conn, &yamux.Config{
				AcceptBacklog:          16,
				EnableKeepAlive:        true,
				KeepAliveInterval:      (10 * time.Second),
				ConnectionWriteTimeout: (500 * time.Millisecond),
			}); err == nil {
				remote := &RemotePeer{
					PublicKey: peeringRequest.PublicKey,
					id:        id,
					session:   session,
				}

				self.sessions[id] = remote

				return remote, nil
			} else {
				return nil, err
			}
		} else {
			return rp, nil
		}
	} else {
		return nil, err
	}
}

func (self *LocalPeer) ConnectTo(addr string, port int) (*RemotePeer, error) {
	var returnErr error

	// open a TCP socket to the given addr/port
	if conn, err := net.Dial(`tcp`, fmt.Sprintf("%s:%d", addr, port)); err == nil {
		// generate a new peering request
		peeringRequest := NewPeeringRequest(self.ID(), self.GetPublicKey())

		// write the request to the peer we're connecting to (who is expecting it)
		if _, err := peeringRequest.WriteTo(conn); err == nil {
			// read acknowledgement from peer
			if ackRequest, err := ParsePeeringRequest(conn); err == nil {
				// register the remote peer
				if remotePeer, err := self.CreateSession(conn, ackRequest); err == nil {
					log.Infof("Connected to peer %s", remotePeer.String())
				}else{
					returnErr = err
				}
			} else {
				returnErr = err
			}
		}else{
			returnErr = err
		}
	} else {
		returnErr = err
	}

	defer conn.Close()
	return returnErr
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
