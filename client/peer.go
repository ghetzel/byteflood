package client

import (
    "github.com/ghetzel/byteflood/peer"
    "fmt"
    "encoding/json"
)

func (self *Client) GetSession(idOrName string) (*peer.RemotePeer, error) {
    if response, err := self.Request(
        `GET`,
        fmt.Sprintf("/api/sessions/%s", idOrName),
        nil,
        nil,
        nil,
    ); err == nil {
        if response.StatusCode < 400 {
            var peer peer.RemotePeer

            if err := json.NewDecoder(response.Body).Decode(&peer); err == nil {
                return &peer, nil
            }else{
                return nil, err
            }
        }else{
            return nil, self.getResponseError(response)
        }
    }else{
        return nil, err
    }
}
