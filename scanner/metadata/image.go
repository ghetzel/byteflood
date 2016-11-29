package metadata

import (
    "fmt"
)

type ImageLoader struct {}

func (self ImageLoader) LoadMetadata(name string) (map[string]interface{}, error) {
    return nil, fmt.Errorf("%T: NI", self)
}
