package metadata

import (
    "fmt"
)

type FileLoader struct {}

func (self FileLoader) CanHandle(_ string) bool {
    return true
}

func (self FileLoader) LoadMetadata(name string) (map[string]interface{}, error) {
    return nil, fmt.Errorf("%T: NI", self)
}
