package db

import (
	"encoding/json"
	"fmt"
	"github.com/ghetzel/byteflood/db/metadata"
	"github.com/ghetzel/go-stockutil/maputil"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/jbenet/go-base58"
	"github.com/spaolacci/murmur3"
	"math/big"
	"path"
	"strings"
)

const FileFingerprintSize = 16777216

type File struct {
	ID             string                 `json:"id"`
	RelativePath   string                 `json:"name"`
	Parent         string                 `json:"parent,omitempty"`
	Label          string                 `json:"label,omitempty"`
	IsDirectory    bool                   `json:"directory"`
	LastModifiedAt int64                  `json:"last_modified_at,omitempty"`
	Metadata       map[string]interface{} `json:"metadata"`
	filename       string
}

func NewFile(name string) *File {
	return &File{
		ID:       FileIdFromName(name),
		Metadata: make(map[string]interface{}),
		filename: name,
	}
}

func (self *File) LoadMetadata() error {
	for _, loader := range metadata.GetLoadersForFile(self.filename) {
		if data, err := loader.LoadMetadata(self.filename); err == nil {
			self.Metadata[self.normalizeLoaderName(loader)] = data
		} else {
			log.Warningf("Problem loading %T for file %q: %v", loader, self.filename, err)
		}
	}

	return nil
}

func (self *File) String() string {
	if data, err := json.MarshalIndent(self, ``, `  `); err == nil {
		return string(data[:])
	} else {
		return err.Error()
	}
}

// func (self *File) getFingerprintData() ([]byte, error) {
//  rv := bytes.NewBuffer()

//  if file, err := os.Open(self.filename); err == nil {
//      fmt.Fprintf(rv, "%s:%d:", self.filename, self.Get(`file.size`, -1))

//      if _, err := io.CopyN(rv, file, FileFingerprintSize); err == nil {
//          return rv.Bytes(), nil
//      }else{
//          return nil, err
//      }
//  }else{
//      return nil, err
//  }
// }

func (self *File) Get(key string, fallback ...interface{}) interface{} {
	if len(fallback) == 0 {
		fallback = append(fallback, nil)
	}

	return maputil.DeepGet(self.Metadata, strings.Split(key, `.`), fallback[0])
}

func (self *File) normalizeLoaderName(loader metadata.Loader) string {
	name := fmt.Sprintf("%T", loader)
	name = strings.TrimPrefix(name, `metadata.`)
	name = strings.TrimSuffix(name, `Loader`)

	return stringutil.Underscore(name)
}

func FileIdFromName(name string) string {
	uid := path.Clean(name)
	hash64 := murmur3.Sum64([]byte(uid[:]))
	return base58.Encode(big.NewInt(int64(hash64)).Bytes())
}
