package client

import (
	"encoding/json"
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/shares"
	"github.com/ghetzel/pivot/dal"
	"strings"
)

func (self *Client) GetShares(peerOrSession string) (output []*shares.Share, err error) {
	prefix := `shares`

	if peerOrSession != `` {
		prefix = fmt.Sprintf("sessions/%v/proxy/shares", peerOrSession)
	}

	err = self.Retrieve(prefix, nil, &output)
	return
}

func (self *Client) GetShare(shareID string, peerOrSession string) (output *shares.Share, err error) {
	prefix := `shares`

	if peerOrSession != `` {
		prefix = fmt.Sprintf("sessions/%v/proxy/shares", peerOrSession)
	}

	err = self.Retrieve(prefix, shareID, &output)
	return
}

func (self *Client) BrowseShare(shareID string, parent string, peerOrSession string) (output *dal.RecordSet, err error) {
	prefix := `shares`

	if peerOrSession != `` {
		prefix = fmt.Sprintf("sessions/%v/proxy/shares", peerOrSession)
	}

	err = self.Retrieve(
		prefix,
		fmt.Sprintf("%v/browse/%s", shareID, parent),
		&output,
	)
	return
}

func (self *Client) QueryShare(
	shareID string,
	filterSpec map[string]interface{},
	limit int,
	offset int,
	sort []string,
	peerOrSession string,
) (*dal.RecordSet, error) {
	if f, err := db.ParseFilter(filterSpec); err == nil {
		if limit == 0 {
			limit = 500
		}

		var rs *dal.RecordSet
		var requestPath string

		if peerOrSession == `` {
			requestPath = fmt.Sprintf(
				"/api/shares/%v/query/%v",
				shareID,
				f.String(),
			)
		} else {
			requestPath = fmt.Sprintf(
				"/api/sessions/%v/proxy/shares/%v/query/%v",
				peerOrSession,
				shareID,
				f.String(),
			)
		}

		if response, err := self.Request(
			`GET`,
			requestPath,
			map[string]string{
				`limit`:  fmt.Sprintf("%v", limit),
				`offset`: fmt.Sprintf("%v", offset),
				`sort`:   strings.Join(sort, `,`),
			},
			nil,
			nil,
		); err == nil {
			if response.StatusCode < 400 {
				if err := json.NewDecoder(response.Body).Decode(&rs); err != nil {
					return nil, err
				}

				return rs, nil
			} else {
				return nil, self.getResponseError(response)
			}
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}
