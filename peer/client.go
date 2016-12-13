package peer

// import (
//     "net/http"
//     "io"
// )

// type Client struct {
//     http.RoundTripper
//     peer *RemotePeer
// }

// func NewClient(conn io.ReadWriteCloser) *Server {
//     return &Client{
//         conn: conn,
//     }
// }

// func (self *Client) RoundTrip(request *http.Request) (*http.Response, error) {
//     // serialize request

//     // write it (checked) to the connection

//     // finalize the transfer

//     // read the (checked) response

// }
