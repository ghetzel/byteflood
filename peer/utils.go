package peer

import (
	"fmt"
	"github.com/ghetzel/go-stockutil/stringutil"
	"io"
	"net"
	"strings"
)

const (
	BF_ERR_ALREADY_CONNECTED = `already connected`
	BF_ERR_LOOPBACK_CONN     = `cannot connect to myself`
	BF_ERR_UNKNOWN_PEER      = `unknown peer`
)

type PeerEventType int

const (
	PeerConnected PeerEventType = iota
	PeerDisconnected
)

type PeerEvent struct {
	Type PeerEventType
	Peer *RemotePeer
}

type readCounter struct {
	Reader    io.Reader
	BytesRead uint64
}

func (self *readCounter) Read(p []byte) (int, error) {
	if self.Reader != nil {
		n, err := self.Reader.Read(p)
		self.BytesRead += uint64(n)
		return n, err
	} else {
		return 0, fmt.Errorf("Reader not set")
	}
}

type writeCounter struct {
	Writer       io.Writer
	BytesWritten uint64
}

func (self *writeCounter) Write(p []byte) (int, error) {
	if self.Writer != nil {
		n, err := self.Writer.Write(p)
		self.BytesWritten += uint64(n)
		return n, err
	} else {
		return 0, fmt.Errorf("Writer not set")
	}
}

func min(a, b int) int {
	if a <= b {
		return a
	}
	return b
}

func ephemeralPort() (int, error) {
	if randomSocket, err := net.Listen(`tcp6`, `:0`); err == nil {
		_, p, _ := net.SplitHostPort(randomSocket.Addr().String())
		port, _ := stringutil.ConvertToInteger(p)

		if err := randomSocket.Close(); err != nil {
			return -1, err
		}

		return int(port), nil
	} else {
		return -1, err
	}
}

func IsAlreadyConnectedErr(err error) bool {
	if err.Error() == BF_ERR_ALREADY_CONNECTED {
		return true
	}

	return false
}

func IsLoopbackConnectionErr(err error) bool {
	if err.Error() == BF_ERR_LOOPBACK_CONN {
		return true
	}

	return false
}

func IsUnknownPeerErr(err error) bool {
	if err.Error() == BF_ERR_UNKNOWN_PEER {
		return true
	}

	return false
}

func IsLookupFailure(err error) bool {
	if strings.HasSuffix(err.Error(), `: no such host`) {
		return true
	}

	return false
}

func SplitAddrCIDR(address string) (string, string) {
	parts := strings.SplitN(address, `/`, 2)

	if len(parts) == 1 {
		return parts[0], ``
	} else {
		return parts[0], parts[1]
	}
}
