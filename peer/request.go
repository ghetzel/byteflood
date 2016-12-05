package peer

import (
	"bytes"
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
