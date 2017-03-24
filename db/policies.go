package db

import (
	"fmt"
)

type SyncPolicy struct {
	ID     string   `json:"id"`
	Fields []string `json:"fields"`
}

func (self *SyncPolicy) Compare(field string, value interface{}, other interface{}) bool {
	// TODO: provide some kind of comparator other than ==
	if fmt.Sprintf("%v", value) == fmt.Sprintf("%v", other) {
		return true
	}

	return false
}

var ChecksumPolicy = SyncPolicy{
	Fields: []string{`checksum`},
}
