package peer

import (
	"bytes"
	"fmt"
	"github.com/vmihailenco/msgpack"
	"io"
)

var PeeringRequestMaxInitialRead = 32768

type PeeringRequest struct {
	ID        []byte
	PublicKey []byte
}

func NewPeeringRequest(id []byte, publicKey []byte) *PeeringRequest {
	return &PeeringRequest{
		ID:        id,
		PublicKey: publicKey,
	}
}

func GenerateAndWritePeeringRequest(w io.Writer, peer Peer) error {
	peeringRequest := NewPeeringRequest(peer.UUID().Bytes(), peer.GetPublicKey())
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
	if self.ID == nil {
		return fmt.Errorf("Invalid peer ID")
	}

	if self.PublicKey == nil {
		return fmt.Errorf("Invalid peer public key")
	}

	if len(self.PublicKey) != 32 {
		return fmt.Errorf("Public Key must be 32 bytes long")
	}

	return nil
}
