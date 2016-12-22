package peer

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
)

type PeerServer struct {
	addr     string
	peer     *LocalPeer
	server   *http.Server
	listener net.Listener
	handler  http.Handler
}

func NewPeerServer(peer *LocalPeer, addr string, handler http.Handler) *PeerServer {
	if a, p, err := net.SplitHostPort(addr); err == nil {
		if a == `` {
			addr = fmt.Sprintf("127.0.0.1:%s", p)
		}
	}

	return &PeerServer{
		peer:    peer,
		addr:    addr,
		handler: handler,
	}
}

func NewPeerServerListener(peer *LocalPeer, listener net.Listener) *PeerServer {
	return &PeerServer{
		peer:     peer,
		listener: listener,
	}
}

func (self *PeerServer) Serve() error {
	if self.handler == nil {
		self.handler = http.DefaultServeMux
		log.Warningf("Peer Server is not registered to a running application.  Remote peers will not have access to the remote command API")
	}

	self.server = &http.Server{
		Addr:    self.addr,
		Handler: self.handler,
	}

	if self.listener == nil {
		return self.server.ListenAndServe()
	} else {
		return self.server.Serve(self.listener)
	}
}

// Handle requests received from the given remote peer
func (self *PeerServer) HandleRequest(remotePeer *RemotePeer, w io.Writer, data []byte) error {
	buffer := bytes.NewBuffer(data)

	// read the request as it came from the remote peer
	if request, err := http.ReadRequest(bufio.NewReader(buffer)); err == nil {
		log.Debugf("[%s] Got request %+v", remotePeer.String(), request)

		// rewrite the URL to point it at the local running PeerServer
		if url, err := url.Parse(fmt.Sprintf("http://%s%s", self.addr, request.RequestURI)); err == nil {
			request.URL = url
			request.RequestURI = `` // client complains if this isn't blank

			log.Debugf("[%s] Got request %s %s, rewrote to %s", remotePeer.String(), request.Method, request.URL.Path, request.URL.String())

			if response, err := http.DefaultClient.Do(request); err == nil {
				return response.Write(w)
			} else {
				return err
			}
		} else {
			return err
		}
	} else {
		return err
	}
}
