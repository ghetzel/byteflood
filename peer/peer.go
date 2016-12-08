package peer

import (
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"github.com/ghetzel/byteflood/util"
	"github.com/ghetzel/go-stockutil/pathutil"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/op/go-logging"
	"github.com/satori/go.uuid"
	"golang.org/x/crypto/nacl/box"
	"io/ioutil"
	"net"
	"os"
	"time"
)

const (
	DEFAULT_PEER_SERVER_PORT       = 0
	DEFAULT_UPNP_DISCOVERY_TIMEOUT = (30 * time.Second)
	DEFAULT_UPNP_MAPPING_DURATION  = (8760 * time.Hour)
	BF_UPNP_SERVICE_NAME           = `byteflood`
)

var log = logging.MustGetLogger(`byteflood.client`)
var logproxy = util.NewLogProxy(`byteflood.client`, `info`)

type Peer interface {
	ID() []byte
	UUID() uuid.UUID
	GetPublicKey() []byte
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
	publicKey            []byte
	privateKey           []byte
}

func LoadKeyfiles(publicKeyPath string, privateKeyPath string) ([]byte, []byte, error) {
	var publicKey []byte
	var privateKey []byte

	if path, err := pathutil.ExpandUser(publicKeyPath); err == nil {
		if data, err := pemDecodeFileName(path); err == nil {
			log.Infof("Loaded public key at %s", path)
			publicKey = data
		} else {
			return nil, nil, err
		}
	} else {
		return nil, nil, err
	}

	if path, err := pathutil.ExpandUser(privateKeyPath); err == nil {
		if data, err := pemDecodeFileName(path); err == nil {
			log.Infof("Loaded private key at %s", path)
			privateKey = data
		} else {
			return nil, nil, err
		}
	} else {
		return nil, nil, err
	}

	return publicKey, privateKey, nil
}

func pemDecodeFileName(filename string) ([]byte, error) {
	if file, err := os.Open(filename); err == nil {
		if data, err := ioutil.ReadAll(file); err == nil {
			if block, _ := pem.Decode(data); block != nil {
				return block.Bytes, nil
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

func GenerateKeypair(publicKeyPath string, privateKeyPath string) error {
	if publicKeyFile, err := os.OpenFile(
		publicKeyPath,
		(os.O_WRONLY | os.O_CREATE | os.O_EXCL),
		0644,
	); err == nil {
		if privateKeyFile, err := os.OpenFile(
			privateKeyPath,
			(os.O_WRONLY | os.O_CREATE | os.O_EXCL),
			0600,
		); err == nil {
			var genError error

			// actually generate keys
			if publicKey, privateKey, err := box.GenerateKey(rand.Reader); err == nil {
				headers := map[string]string{
					`Cryptosystem`:   `NaCl cryptobox-compatible`,
					`Encryption`:     `XSalsa20 stream cipher`,
					`KeyExchange`:    `Diffie-Hellman ECDH (Curve25519)`,
					`Authentication`: `Poly1305 MAC`,
				}

				// encode and write public key
				if err := pem.Encode(publicKeyFile, &pem.Block{
					Type:    `NACL CRYPTOBOX PUBLIC KEY`,
					Headers: headers,
					Bytes:   []byte(publicKey[:]),
				}); err == nil {
					// encode and write private key
					if err := pem.Encode(privateKeyFile, &pem.Block{
						Type:    `NACL CRYPTOBOX PRIVATE KEY`,
						Headers: headers,
						Bytes:   []byte(privateKey[:]),
					}); err != nil {
						genError = err
					}
				} else {
					genError = err
				}
			} else {
				genError = err
			}

			// an error occurred during key generation, remove the files
			if genError != nil {
				defer os.Remove(privateKeyFile.Name())
				defer os.Remove(publicKeyFile.Name())
			}

			return genError
		} else {
			defer os.Remove(publicKeyFile.Name())
			return err
		}
	} else {
		return err
	}
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
		EnableUpnp:           false,
		UpnpDiscoveryTimeout: DEFAULT_UPNP_DISCOVERY_TIMEOUT,
		UpnpMappingDuration:  DEFAULT_UPNP_MAPPING_DURATION,
		id:                   localID,
		sessions:             make(map[uuid.UUID]*RemotePeer),
		publicKey:            publicKey,
		privateKey:           privateKey,
	}, nil
}

func (self *LocalPeer) ID() []byte {
	return self.id.Bytes()
}

func (self *LocalPeer) UUID() uuid.UUID {
	return self.id
}

func (self *LocalPeer) GetPublicKey() []byte {
	return self.publicKey
}

func (self *LocalPeer) Listen() error {
	if self.Port == 0 {
		if randomSocket, err := net.Listen(`tcp6`, `:0`); err == nil {
			_, p, _ := net.SplitHostPort(randomSocket.Addr().String())

			if v, err := stringutil.ConvertToInteger(p); err == nil {
				self.Port = int(v)
			} else {
				return err
			}

			if err := randomSocket.Close(); err != nil {
				return err
			}
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

	if listener, err := net.Listen(`tcp`, fmt.Sprintf("%s:%d", self.Address, self.Port)); err == nil {
		log.Infof("Listening on %s:%d", self.Address, self.Port)

		for {
			// accept new connections
			if conn, err := listener.Accept(); err == nil {
				log.Debugf("Got connection request from %s", conn.RemoteAddr().String())

				// verify that we want to proceed with this connection...
				if self.PermitConnectionFrom(conn) {
					if remotePeer, err := self.RegisterPeer(conn, true); err == nil {
						go remotePeer.Start(self)
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

	return nil
}

func (self *LocalPeer) PermitConnectionFrom(conn net.Conn) bool {
	return true
}

func (self *LocalPeer) RegisterPeer(conn net.Conn, remoteInitiated bool) (*RemotePeer, error) {
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

	if remotePeeringRequest != nil {
		if remotePeer, err := NewRemotePeerFromRequest(remotePeeringRequest, conn); err == nil {
			var sharedKey [32]byte
			var publicKey [32]byte
			var privateKey [32]byte

			copy(publicKey[:], remotePeeringRequest.PublicKey[:32])
			copy(privateKey[:], self.privateKey[:32])

			box.Precompute(
				&sharedKey,
				&publicKey,
				&privateKey,
			)

			remotePeer.sharedKey = sharedKey
			remotePeer.secureReady = true

			self.sessions[remotePeer.UUID()] = remotePeer
			log.Debugf("Remote peer %s registered", remotePeer.String())
			return remotePeer, nil
		} else {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("Could not read remote peering request")
	}
}

func (self *LocalPeer) RemovePeer(id uuid.UUID) {
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
	var returnErr error

	// open a TCP socket to the given addr/port
	if conn, err := net.Dial(`tcp`, fmt.Sprintf("%s:%d", addr, port)); err == nil {
		if remotePeer, err := self.RegisterPeer(conn, false); err == nil {
			return remotePeer, nil
		} else {
			returnErr = err
		}

		// successful connections will skip this, everything else will close
		if err := conn.Close(); err != nil {
			log.Errorf("Failed to close connection %s: %v", conn.RemoteAddr().String(), err)
		}

		return nil, returnErr
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
