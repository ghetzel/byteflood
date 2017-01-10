package db

import (
	"github.com/fatih/structs"
)

type IModel interface {
	Load() error
	Save() error
	Delete() error
}

type Model struct {
	IModel
}

func (self Model) Load() error {
	for _, field := range structs.New(self).Fields() {

	}

	log.Debugf("load: %+v", )
	return nil
}

func (self Model) Save() error {
	log.Debugf("save: %+v", structs.New(self).Fields())
	return nil
}

func (self Model) Delete() error {
	log.Debugf("delete: %+v", structs.New(self).Fields())
	return nil
}
