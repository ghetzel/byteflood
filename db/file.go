package db

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/ghetzel/byteflood/db/metadata"
	"github.com/ghetzel/go-stockutil/maputil"
	"github.com/ghetzel/go-stockutil/sliceutil"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/jbenet/go-base58"
	"github.com/spaolacci/murmur3"
	"io"
	"math/big"
	"os"
	"strings"
)

const FileFingerprintSize = 16777216

var MaxChildEntries = 10000

type File struct {
	ID             string                 `json:"id"`
	RelativePath   string                 `json:"name"`
	Parent         string                 `json:"parent,omitempty"`
	Checksum       string                 `json:"checksum,omitempty"`
	Label          string                 `json:"label,omitempty"`
	IsDirectory    bool                   `json:"directory"`
	LastModifiedAt int64                  `json:"last_modified_at,omitempty"`
	Metadata       map[string]interface{} `json:"metadata"`
	info           os.FileInfo            `json:"-"`
	filename       string
	metadataLoaded bool
}

type WalkFunc func(path string, file *File, err error) error // {}

func NewFile(label string, root string, name string) *File {
	normFileName := NormalizeFileName(root, name)

	return &File{
		ID:           FileIdFromName(label, normFileName),
		RelativePath: normFileName,
		Metadata:     make(map[string]interface{}),
		filename:     name,
	}
}

func (self *File) Info() os.FileInfo {
	return self.info
}

func (self *File) LoadMetadata() error {
	if stat, err := os.Stat(self.filename); err == nil {
		self.info = stat
	} else {
		return err
	}

	for _, loader := range metadata.GetLoadersForFile(self.filename) {
		if data, err := loader.LoadMetadata(self.filename); err == nil {
			self.Metadata[self.normalizeLoaderName(loader)] = data
		} else {
			log.Warningf("Problem loading %T for file %q: %v", loader, self.filename, err)
		}
	}

	self.metadataLoaded = true

	return nil
}

func (self *File) String() string {
	if data, err := json.MarshalIndent(self, ``, `  `); err == nil {
		return string(data[:])
	} else {
		return err.Error()
	}
}

func (self *File) Children(filterString ...string) ([]*File, error) {
	filterString = append(filterString, fmt.Sprintf("parent=%s", self.ID))

	if f, err := ParseFilter(sliceutil.CompactString(filterString)); err == nil {
		f.Limit = MaxChildEntries
		f.Sort = []string{`-directory`, `name`}

		files := make([]*File, 0)

		if err := Metadata.Find(f, &files); err == nil {
			// enforce a strict path hierarchy for parent-child relationships
			for _, file := range files {
				if !strings.HasPrefix(file.RelativePath, self.RelativePath+`/`) {
					return nil, fmt.Errorf("child entry falls outside of parent path")
				}
			}

			return files, nil
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}

func (self *File) GenerateChecksum() (string, error) {
	if self.IsDirectory {
		return ``, fmt.Errorf("Cannot generate checksum on directory")
	}

	if fsFile, err := os.Open(self.filename); err == nil {
		hash := sha256.New()

		if _, err := io.Copy(hash, fsFile); err != nil {
			return ``, err
		}

		result := hash.Sum(nil)
		return hex.EncodeToString([]byte(result[:])), nil
	} else {
		return ``, err
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

func (self *File) Walk(walkFn WalkFunc, filterStrings ...string) error {
	if self.IsDirectory {
		if err := walkFn(self.RelativePath, self, nil); err == nil {
			if children, err := self.Children(filterStrings...); err == nil {
				for _, child := range children {
					if err := child.Walk(walkFn, filterStrings...); err != nil {
						return err
					}
				}

				return nil
			} else {
				return err
			}
		} else {
			return err
		}
	} else {
		return walkFn(self.RelativePath, self, nil)
	}
}

func (self *File) GetManifest(fields []string, filterString string) (*Manifest, error) {
	manifest := NewManifest(self.RelativePath)

	if err := self.Walk(func(path string, file *File, err error) error {
		if err == nil {
			var itemType ManifestItemType

			if file.IsDirectory {
				itemType = DirectoryItem
			} else {
				itemType = FileItem
			}

			fieldValues := make([]ManifestValue, len(fields))

			for i, field := range fields {
				switch field {
				case `name`:
					fieldValues[i] = file.RelativePath
				case `label`:
					fieldValues[i] = file.Label
				case `parent`:
					fieldValues[i] = file.Parent
				case `checksum`:
					fieldValues[i] = file.Checksum
				default:
					fieldValues[i] = file.Get(field)
				}

				manifest.Fields = append(manifest.Fields, field)
			}

			manifest.Add(ManifestItem{
				ID:           file.ID,
				Type:         itemType,
				RelativePath: path,
				Values:       fieldValues,
			})

			return nil
		} else {
			return err
		}
	}, filterString); err != nil {
		return nil, err
	}

	return manifest, nil
}

func FileIdFromName(label string, name string) string {
	uid := fmt.Sprintf("%s:%s", label, name)
	hash64 := murmur3.Sum64([]byte(uid[:]))
	return base58.Encode(big.NewInt(int64(hash64)).Bytes())
}

func NormalizeFileName(root string, name string) string {
	prefix := strings.TrimSuffix(root, `/`)
	name = strings.TrimPrefix(name, prefix)
	name = `/` + strings.TrimPrefix(name, `/`)

	return name
}
