package peer

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
)

type Server struct {
	addr     string
	peer     *LocalPeer
	server   *http.Server
	listener net.Listener
}

func NewServer(peer *LocalPeer, addr string) *Server {
	if a, p, err := net.SplitHostPort(addr); err == nil {
		if a == `` {
			addr = fmt.Sprintf("127.0.0.1:%s", p)
		}
	}

	return &Server{
		peer: peer,
		addr: addr,
	}
}

func NewServerListener(peer *LocalPeer, listener net.Listener) *Server {
	return &Server{
		peer:     peer,
		listener: listener,
	}
}

func (self *Server) Serve() error {
	mux := http.NewServeMux()

	mux.HandleFunc(`/`, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set(`Content-Type`, `application/json`)
		json.NewEncoder(w).Encode(map[string]interface{}{
			`peer`: map[string]interface{}{
				`id`: self.peer.ID(),
			},
		})
	})

	// mux.HandleFunc(`/peers/`, func(w http.ResponseWriter, req *http.Request) {
	// 	parts := strings.SplitN(req.URL.Path, `/`, 4)

	// 	if len(parts) > 2 {
	// 		remotePeerId := parts[2]

	// 		if remotePeer, ok := self.peer.GetPeer(remotePeerId); ok {
	// 			if hijacker, ok := w.(http.Hijacker); ok {
	// 				if serviceConn, requestRW, err := hijacker.Hijack(); err == nil {
	// 					defer serviceConn.Close()

	// 					// read request from client
	// 					// write request to remotePeer
	// 					if _, err := io.Copy(remotePeer, requestRW); err == nil {
	// 						requestRW.Flush()

	// 						// read response from peer
	// 						// write response to client
	// 						if _, err := io.Copy(requestRW, remotePeer); err == nil {
	// 							requestRW.Flush()
	// 						} else {
	// 							http.Error(w, err.Error(), http.StatusInternalServerError)
	// 						}
	// 					} else {
	// 						http.Error(w, err.Error(), http.StatusInternalServerError)
	// 					}
	// 				} else {
	// 					http.Error(w, err.Error(), http.StatusInternalServerError)
	// 				}
	// 			} else {
	// 				http.Error(w, "underlying server cannot communicate with peers", http.StatusInternalServerError)
	// 			}
	// 		} else {
	// 			http.Error(w, "peer not found", http.StatusNotFound)
	// 		}
	// 	} else {
	// 		http.Error(w, "Not Found", http.StatusNotFound)
	// 	}
	// })

	self.server = &http.Server{
		Addr:    self.addr,
		Handler: mux,
	}

	if self.listener == nil {
		return self.server.ListenAndServe()
	} else {
		return self.server.Serve(self.listener)
	}
}

// Handle requests received from the given remote peer
func (self *Server) HandleRequest(remotePeer *RemotePeer, w io.Writer, data []byte) error {
	buffer := bytes.NewBuffer(data)

	if request, err := http.ReadRequest(bufio.NewReader(buffer)); err == nil {
		if url, err := url.Parse(fmt.Sprintf("http://%s%s", self.addr, request.RequestURI)); err == nil {
			request.URL = url
			request.RequestURI = ``

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
