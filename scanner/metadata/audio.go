package metadata

import (
    "fmt"
)

type AudioLoader struct {}

func (self AudioLoader) LoadMetadata(name string) (map[string]interface{}, error) {
    return nil, fmt.Errorf("%T: NI", self)
}
