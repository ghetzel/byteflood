package peer

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"github.com/jbenet/go-base58"
	"gopkg.in/vmihailenco/msgpack.v2"
	"io"
)

var PeeringRequestMaxInitialRead = 32768

type PeeringRequest struct {
	PublicKey []byte
	SessionID []byte
}

func NewPeeringRequest(publicKey []byte) *PeeringRequest {
	// generate a random session ID
	var sessionId [8]byte
	rand.Read(sessionId[:])

	return &PeeringRequest{
		PublicKey: publicKey,
		SessionID: []byte(sessionId[:]),
	}
}

func GenerateAndWritePeeringRequest(w io.Writer, peer Peer, sessionId []byte) error {
	peeringRequest := NewPeeringRequest(peer.GetPublicKey())

	if sessionId != nil {
		copy(peeringRequest.SessionID, sessionId)
	}

	_, err := peeringRequest.WriteTo(w)
	return err
}

func ParsePeeringRequest(r io.Reader) (*PeeringRequest, error) {
	peeringRequest := PeeringRequest{}

	if err := msgpack.NewDecoder(r).Decode(&peeringRequest); err == nil {
		return &peeringRequest, nil
	} else {
		return nil, err
	}
}

func (self *PeeringRequest) WriteTo(w io.Writer) (int, error) {
	data := bytes.NewBuffer(nil)

	if err := msgpack.NewEncoder(data).Encode(self); err == nil {
		return w.Write(data.Bytes())
	} else {
		return -1, err
	}
}

func (self *PeeringRequest) Validate() error {
	if self.PublicKey == nil {
		return fmt.Errorf("Invalid peer public key")
	}

	if len(self.PublicKey) != 32 {
		return fmt.Errorf("Public Key must be 32 bytes long")
	}

	return nil
}

func (self *PeeringRequest) String() string {
	return base58.Encode(self.SessionID)
}
