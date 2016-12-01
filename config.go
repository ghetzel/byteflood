package byteflood

import (
	"github.com/ghetzel/byteflood/scanner"
	"github.com/ghodss/yaml"
	"io/ioutil"
)

type Configuration struct {
	DatabaseURI string              `json:"database,omitempty"`
	Directories []scanner.Directory `json:"directories"`
}

func LoadConfig(filename string) (Configuration, error) {
	config := Configuration{}

	if data, err := ioutil.ReadFile(filename); err == nil {
		if err := yaml.Unmarshal(data, &config); err != nil {
			return config, err
		}
	}

	return config, nil
}
