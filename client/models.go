package client

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/pivot/dal"
)

var objToSchema = map[string]*dal.Collection{
	`peers`:         db.AuthorizedPeersSchema,
	`directories`:   db.ScannedDirectoriesSchema,
	`subscriptions`: db.SubscriptionsSchema,
	`shares`:        db.SharesSchema,
}

func (self *Client) Retrieve(objType string, id interface{}, into interface{}) error {
	var requestPath string

	if id == nil {
		requestPath = fmt.Sprintf("/api/%s", objType)
	} else {
		requestPath = fmt.Sprintf("/api/%s/%v", objType, id)
	}

	if response, err := self.Request(
		`GET`,
		requestPath,
		nil,
		nil,
		nil,
	); err == nil {
		if response.StatusCode < 400 {
			if into != nil {
				if err := json.NewDecoder(response.Body).Decode(&into); err != nil {
					return err
				}
			}

			return nil
		} else {
			return self.getResponseError(response)
		}
	} else {
		return err
	}
}

func (self *Client) Create(objType string, from interface{}) error {
	return self.createOrUpdate(true, objType, from)
}

func (self *Client) Update(objType string, from interface{}) error {
	return self.createOrUpdate(false, objType, from)
}

func (self *Client) Delete(objType string, id interface{}) error {
	if response, err := self.Request(
		`DELETE`,
		fmt.Sprintf("/api/%s/%v", objType, id),
		nil,
		nil,
		nil,
	); err == nil {
		if response.StatusCode < 400 {
			return nil
		} else {
			return self.getResponseError(response)
		}
	} else {
		return err
	}
}

func (self *Client) createOrUpdate(isCreate bool, objType string, from interface{}) error {
	var requestBody bytes.Buffer

	if from != nil {
		if schema, ok := objToSchema[objType]; ok {
			if record, err := schema.MakeRecord(from); err == nil {
				log.Debugf("Record: %+v %+v", record, from)

				if err := json.NewEncoder(&requestBody).Encode(dal.NewRecordSet(record)); err != nil {
					return err
				}
			} else {
				return err
			}
		} else {
			return fmt.Errorf("Unknown schema for object type '%s'", objType)
		}
	}

	var method string

	if isCreate {
		method = `POST`
	} else {
		method = `PUT`
	}

	if response, err := self.Request(
		method,
		fmt.Sprintf("/api/%s", objType),
		nil,
		nil,
		&requestBody,
	); err == nil {
		if response.StatusCode < 400 {
			return nil
		} else {
			return self.getResponseError(response)
		}
	} else {
		return err
	}
}
