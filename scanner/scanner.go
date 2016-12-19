package scanner

import (
	"encoding/json"
	"fmt"
	"github.com/ghetzel/byteflood/scanner/metadata"
	"github.com/ghetzel/go-stockutil/maputil"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/ghetzel/pivot"
	"github.com/ghetzel/pivot/backends"
	"github.com/ghetzel/pivot/dal"
	"github.com/jbenet/go-base58"
	"github.com/op/go-logging"
	"github.com/spaolacci/murmur3"
	"io/ioutil"
	"math/big"
	"path"
	"regexp"
	"strings"
)

var log = logging.MustGetLogger(`byteflood/scanner`)

const FileFingerprintSize = 16777216

var DefaultCollectionName = `metadata`

type File struct {
	Name     string
	Metadata map[string]interface{}
}

type ScanOptions struct {
	FilePattern          string `json:"patterns,omitempty"`
	NoRecurseDirectories bool   `json:"no_recurse,omitempty"`
	FileMinimumSize      int    `json:"file_min_size,omitempty"`
}

func DefaultScanOptions() ScanOptions {
	return ScanOptions{
		NoRecurseDirectories: false,
	}
}

func NewFile(name string) *File {
	return &File{
		Name:     name,
		Metadata: make(map[string]interface{}),
	}
}

func (self *File) LoadMetadata() error {
	for _, loader := range metadata.GetLoadersForFile(self.Name) {
		if data, err := loader.LoadMetadata(self.Name); err == nil {
			self.Metadata[self.normalizeLoaderName(loader)] = data
		} else {
			log.Warningf("Problem loading %T for file %q: %v", loader, self.Name, err)
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

func (self *File) ID() string {
	uid := path.Clean(self.Name)
	hash64 := murmur3.Sum64([]byte(uid[:]))
	return base58.Encode(big.NewInt(int64(hash64)).Bytes())
}

// func (self *File) getFingerprintData() ([]byte, error) {
// 	rv := bytes.NewBuffer()

// 	if file, err := os.Open(self.Name); err == nil {
// 		fmt.Fprintf(rv, "%s:%d:", self.Name, self.Get(`file.size`, -1))

// 		if _, err := io.CopyN(rv, file, FileFingerprintSize); err == nil {
// 			return rv.Bytes(), nil
// 		}else{
// 			return nil, err
// 		}
// 	}else{
// 		return nil, err
// 	}
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

type Directory struct {
	Path        string       `json:"path"`
	Label       string       `json:"label"`
	Options     ScanOptions  `json:"options"`
	Files       []*File      `json:"-"`
	Directories []*Directory `json:"-"`
	scanner     *Scanner
}

func NewDirectory(scanner *Scanner, path string, options ScanOptions) *Directory {
	return &Directory{
		Path:        path,
		Options:     options,
		Files:       make([]*File, 0),
		Directories: make([]*Directory, 0),
		scanner:     scanner,
	}
}

func (self *Directory) Scan() error {
	if entries, err := ioutil.ReadDir(self.Path); err == nil {
		for _, entry := range entries {
			absPath := path.Join(self.Path, entry.Name())

			// recursive directory handling
			if entry.IsDir() {
				if !self.Options.NoRecurseDirectories {
					subdirectory := NewDirectory(self.scanner, absPath, self.Options)

					if err := subdirectory.Scan(); err != nil {
						return err
					} else {
						log.Debugf("ADD: directory %s", subdirectory.Path)
						self.Directories = append(self.Directories, subdirectory)
					}
				}
			} else {
				// if we've specified a minimum file size, and this file is less than that,
				// then skip it
				if self.Options.FileMinimumSize > 0 && entry.Size() < int64(self.Options.FileMinimumSize) {
					continue
				}

				// scan the file as a sharable asset
				if file, err := self.scanFile(absPath); err == nil {
					if file != nil {
						self.Files = append(self.Files, file)
					}
				} else {
					return err
				}
			}
		}
	} else {
		return err
	}

	return nil
}

func (self *Directory) scanFile(name string) (*File, error) {
	// file pattern matching
	if self.Options.FilePattern != `` {
		if rx, err := regexp.Compile(self.Options.FilePattern); err == nil {
			if !rx.MatchString(name) {
				return nil, nil
			}
		} else {
			return nil, err
		}
	}

	// get file implementation
	file := NewFile(name)

	// load file metadata
	if err := file.LoadMetadata(); err != nil {
		return nil, err
	}

	if self.scanner != nil {
		if err := self.scanner.PersistRecord(file.ID(), file.Metadata); err != nil {
			return nil, err
		}
	}

	return file, nil
}

type Scanner struct {
	Directories []*Directory
	DatabaseURI string
	Collection  string
	db          backends.Backend
}

func NewScanner() *Scanner {
	return &Scanner{
		Directories: make([]*Directory, 0),
		DatabaseURI: `boltdb:///~/.local/share/byteflood/db`,
		Collection:  DefaultCollectionName,
	}
}

func (self *Scanner) Initialize() error {
	if db, err := pivot.NewDatabase(self.DatabaseURI); err == nil {
		self.db = db
	} else {
		return err
	}

	return nil
}

func (self *Scanner) AddDirectory(path string, options ScanOptions) {
	self.Directories = append(self.Directories, NewDirectory(self, path, options))
}

func (self *Scanner) PersistRecord(id string, data map[string]interface{}) error {
	return self.db.Insert(self.Collection, dal.NewRecordSet(
		dal.NewRecord(id).SetFields(data),
	))
}

func (self *Scanner) QueryRecords(filterString string) (*dal.RecordSet, error) {
	if index := self.db.WithSearch(); index != nil {
		return index.QueryString(self.Collection, filterString)
	} else {
		return nil, fmt.Errorf("Backend type %T does not support searching", self.db)
	}
}

func (self *Scanner) Scan() error {
	for _, directory := range self.Directories {
		if err := directory.Scan(); err != nil {
			return err
		}
	}

	return nil
}
