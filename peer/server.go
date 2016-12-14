package peer

import (
	"bufio"
	"bytes"
	"github.com/urfave/negroni"
	"io"
	"net/http"
	"strings"
)

type Server struct {
	addr   string
	peer   *LocalPeer
	server *negroni.Negroni
}

func NewServer(peer *LocalPeer, addr string) *Server {
	return &Server{
		addr: addr,
		peer: peer,
	}
}

func (self *Server) Serve() error {
	mux := http.NewServeMux()

	mux.HandleFunc(`/`, func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc(`/peers/`, func(w http.ResponseWriter, req *http.Request) {
		parts := strings.SplitN(req.URL.Path, `/`, 4)

		if len(parts) > 2 {
			remotePeerId := parts[2]

			if remotePeer, ok := self.peer.GetPeer(remotePeerId); ok {
				if hijacker, ok := w.(http.Hijacker); ok {
					if serviceConn, requestRW, err := hijacker.Hijack(); err == nil {
						defer serviceConn.Close()

						// read request from client
						// write request to remotePeer
						if _, err := io.Copy(remotePeer, requestRW); err == nil {
							requestRW.Flush()

							// read response from peer
							// write response to client
							if _, err := io.Copy(requestRW, remotePeer); err == nil {
								requestRW.Flush()
							} else {
								http.Error(w, err.Error(), http.StatusInternalServerError)
							}
						} else {
							http.Error(w, err.Error(), http.StatusInternalServerError)
						}
					} else {
						http.Error(w, err.Error(), http.StatusInternalServerError)
					}
				} else {
					http.Error(w, "underlying server cannot communicate with peers", http.StatusInternalServerError)
				}
			} else {
				http.Error(w, "peer not found", http.StatusNotFound)
			}
		} else {
			http.Error(w, "Not Found", http.StatusNotFound)
		}
	})

	self.server = negroni.New()
	self.server.UseHandler(mux)
	go self.server.Run(self.addr)

	return nil
}

func (self *Server) ParseRequest(data []byte) (*http.Request, error) {
	return http.ReadRequest(bufio.NewReader(bytes.NewBuffer(data)))
}
