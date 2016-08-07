package byteflood

import (
	"fmt"
	"github.com/urfave/negroni"
)

type Server struct {
	Address string
	Port    int
	server  *negroni.Negroni
}

func NewServer(address string, port int) *Server {
	return &Server{
		Address: address,
		Port:    port,
	}
}

func (self *Server) ListenAndServe() error {
	self.server = negroni.New()

	self.server.Run(fmt.Sprintf("%s:%d", self.Address, self.Port))
	return nil
}
