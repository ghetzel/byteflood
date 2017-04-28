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
	peer     *LocalPeer
	server   *http.Server
	listener net.Listener
}

func NewPeerServer(peer *LocalPeer) *PeerServer {
	return &PeerServer{
		peer: peer,
	}
}

func NewPeerServerListener(peer *LocalPeer, listener net.Listener) *PeerServer {
	return &PeerServer{
		peer:     peer,
		listener: listener,
	}
}

func (self *PeerServer) Serve(addr string, handler http.Handler) error {
	if a, p, err := net.SplitHostPort(addr); err == nil {
		if a == `` {
			addr = fmt.Sprintf("127.0.0.1:%s", p)
		}
	}

	if handler == nil {
		handler = http.DefaultServeMux
		log.Warningf("Peer Server is not registered to a running application.  Remote peers will not have access to the remote command API")
	}

	self.server = &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	if self.listener == nil {
		return self.server.ListenAndServe()
	} else {
		return self.server.Serve(self.listener)
	}
}

// Handle requests received from the given remote peer
func (self *PeerServer) HandleRequest(remotePeer *RemotePeer, w io.Writer, data []byte) error {
	if self.server != nil {
		buffer := bytes.NewBuffer(data)

		// read the request as it came from the remote peer
		if request, err := http.ReadRequest(bufio.NewReader(buffer)); err == nil {
			// rewrite the URL to point it at the local running PeerServer
			if url, err := url.Parse(fmt.Sprintf("http://%s%s", self.server.Addr, request.RequestURI)); err == nil {
				request.URL = url
				request.RequestURI = `` // client complains if this isn't blank

				// this is safe to use as an authoritative peer identifier in request handlers
				// that are implementing authentication/authorization becase we're setting it
				// here, which is on the receiving end of the request (as opposed to this header coming
				// from the client and us trusting it implicitly as correct and not forged)
				request.Header.Set(`X-Byteflood-Session`, remotePeer.SessionID())

				if response, err := http.DefaultClient.Do(request); err == nil {
					// log.Infof(
					// 	"ServiceRequest [%s]: %s %s: %s (%d bytes)",
					// 	remotePeer.String(),
					// 	request.Method,
					// 	request.URL.Path,
					// 	response.Status,
					// 	response.ContentLength,
					// )

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
	} else {
		return fmt.Errorf("server not initialized")
	}
}
