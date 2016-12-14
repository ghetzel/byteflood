package peer

import (
	"github.com/ghetzel/go-stockutil/stringutil"
	"net"
)

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
