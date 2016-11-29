package metadata

import (
    "fmt"
)

type VideoLoader struct {}

func (self VideoLoader) LoadMetadata(name string) (map[string]interface{}, error) {
    return nil, fmt.Errorf("%T: NI", self)
}
