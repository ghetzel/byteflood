package byteflood

import (
	"github.com/ghodss/yaml"
	"io/ioutil"
)

type Configuration struct {
	PieceLength     int      `json:"piece_length,omitempty"`
	AnnounceList    []string `json:"announce"`
	ScanPattern     string   `json:"pattern,omitempty"`
	ScanTags        []string `json:"tags,omitempty"`
	DirectoryPrefix string   `json:"prefix,omitempty"`
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
