package client

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/shares"
	"github.com/ghetzel/go-stockutil/typeutil"
	"github.com/ghetzel/pivot/dal"
)

func (self *Client) GetShares(peerOrSession string, stats bool) (output []*shares.Share, err error) {
	prefix := `shares`

	if peerOrSession != `` {
		prefix = fmt.Sprintf("sessions/%v/proxy/shares", peerOrSession)
	}

	if stats {
		prefix = prefix + `?stats=true`
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

func (self *Client) CreateShare(id string, config *shares.Share) error {
	var share *shares.Share

	v := db.SharesSchema.NewInstance()

	if vS, ok := v.(*shares.Share); ok {
		share = vS
	} else {
		return fmt.Errorf("Failed to instantiate share")
	}

	if !typeutil.IsEmpty(id) {
		share.ID = id
	} else {
		return fmt.Errorf("%q cannot be empty", `id`)
	}

	if config != nil {
		if !typeutil.IsEmpty(config.IconName) {
			share.IconName = config.IconName
		}

		if !typeutil.IsZero(config.BaseFilter) {
			share.BaseFilter = config.BaseFilter
		}

		if !typeutil.IsZero(config.Description) {
			share.Description = config.Description
		}

		if !typeutil.IsZero(config.LongDescription) {
			share.LongDescription = config.LongDescription
		}
	}

	return self.Create(`shares`, share)
}

func (self *Client) RemoveShare(id string) error {
	return self.Delete(`shares`, id)
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
