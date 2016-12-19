package byteflood

import (
	"fmt"
	"github.com/ghetzel/byteflood/scanner"
	"github.com/ghetzel/go-stockutil/pathutil"
	"github.com/ghodss/yaml"
	"github.com/imdario/mergo"
	"github.com/mohae/deepcopy"
	"github.com/op/go-logging"
	"io/ioutil"
	"os"
	"path"
)

var log = logging.MustGetLogger(`byteflood`)

var DefaultConfig = Configuration{
	PublicKey:   `~/.config/byteflood/keys/peer.pub`,
	PrivateKey:  `~/.config/byteflood/keys/peer.key`,
	DatabaseURI: `boltdb:///~/.local/share/byteflood/db`,
	Directories: make([]scanner.Directory, 0),
	Peers:       make(map[string][]string),
}

type Configuration struct {
	PublicKey   string              `json:"public_key,omitempty"`
	PrivateKey  string              `json:"private_key,omitempty"`
	DatabaseURI string              `json:"database,omitempty"`
	Directories []scanner.Directory `json:"directories,omitempty"`
	Peers       map[string][]string `json:"peers,omitempty"`
}

func (self *Configuration) String() string {
	data, _ := yaml.Marshal(self)
	return string(data[:])
}

func cloneDefaultConfig() (*Configuration, error) {
	configI := deepcopy.Copy(DefaultConfig)

	if config, ok := configI.(Configuration); ok {
		return &config, nil
	} else {
		return nil, fmt.Errorf("Failed to load configuration from defaults")
	}
}

func LoadConfig(filename string) (*Configuration, error) {
	config := Configuration{}

	if expandedFilename, err := pathutil.ExpandUser(filename); err == nil {
		if data, err := ioutil.ReadFile(expandedFilename); err == nil {
			if err := yaml.Unmarshal(data, &config); err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}

	return &config, nil

}

func MergeConfig(destination *Configuration, filename string) error {
	if newConfig, err := LoadConfig(filename); err == nil {
		return mergo.MergeWithOverwrite(destination, newConfig)
	} else {
		return err
	}
}

func LoadConfigDefaults(filename string) (*Configuration, error) {
	if config, err := cloneDefaultConfig(); err == nil {
		if err := MergeConfig(config, filename); err == nil {
			return config, nil
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}

func MergeConfigDirectory(destination *Configuration, dirname string) error {
	if destination == nil {
		if defaultConfig, err := cloneDefaultConfig(); err == nil {
			destination = defaultConfig
		} else {
			return err
		}
	}

	if expandedDirName, err := pathutil.ExpandUser(dirname); err == nil {
		if files, err := ioutil.ReadDir(expandedDirName); err == nil {
			for _, file := range files {
				if !file.IsDir() && path.Ext(file.Name()) == `.yml` {
					if err := MergeConfig(destination, file.Name()); err != nil {
						return err
					}
				}
			}

			return nil
		} else if os.IsNotExist(err) {
			return nil
		} else {
			return err
		}
	} else {
		return err
	}
}
