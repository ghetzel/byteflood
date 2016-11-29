package metadata

import (
	"fmt"
)

type AudioLoader struct {
	Loader
}

func (self AudioLoader) CanHandle(_ string) bool {
	return false
}

func (self AudioLoader) LoadMetadata(name string) (map[string]interface{}, error) {
	return nil, fmt.Errorf("%T: NI", self)
}
