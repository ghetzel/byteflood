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
	"time"
)

var log = logging.MustGetLogger(`byteflood/scanner`)

const FileFingerprintSize = 16777216

var DefaultCollectionName = `metadata`
var DefaultManagementCollectionName = `byteflood`

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
	Label       string       `json:"label,omitempty"`
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

func (self *Directory) Initialize(scanner *Scanner) error {
	if self.Path == `` {
		return fmt.Errorf("Directory path must be specified.")
	}

	if self.Label == `` {
		self.Label = stringutil.Underscore(path.Base(self.Path))
	}

	self.scanner = scanner

	return nil
}

func (self *Directory) Scan() error {
	if entries, err := ioutil.ReadDir(self.Path); err == nil {
		for _, entry := range entries {
			absPath := path.Join(self.Path, entry.Name())

			// recursive directory handling
			if entry.IsDir() {
				if !self.Options.NoRecurseDirectories {
					subdirectory := NewDirectory(self.scanner, absPath, self.Options)
					subdirectory.Label = self.Label

					if err := subdirectory.Scan(); err == nil {
						log.Debugf("ADD: directory [%s] %s", subdirectory.Label, subdirectory.Path)
						self.Directories = append(self.Directories, subdirectory)
					} else {
						return err
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

	file.Metadata[`name`] = file.Name
	file.Metadata[`label`] = self.Label
	file.Metadata[`last_seen`] = time.Now().UnixNano()

	if self.scanner != nil {
		if err := self.scanner.PersistRecord(file.ID(), file.Metadata); err != nil {
			return nil, err
		}
	}

	return file, nil
}

type Scanner struct {
	Directories          []*Directory
	DatabaseURI          string
	Collection           string
	ManagementCollection string
	db                   backends.Backend
}

func NewScanner() *Scanner {
	backends.BleveIndexerPageSize = 100

	return &Scanner{
		Directories:          make([]*Directory, 0),
		DatabaseURI:          `boltdb:///~/.local/share/byteflood/db`,
		Collection:           DefaultCollectionName,
		ManagementCollection: DefaultManagementCollectionName,
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

func (self *Scanner) AddDirectory(directory *Directory) error {
	if err := directory.Initialize(self); err == nil {
		self.Directories = append(self.Directories, directory)
		return nil
	} else {
		return err
	}
}

func (self *Scanner) PersistRecord(id string, data map[string]interface{}) error {
	return self.db.Insert(self.Collection, dal.NewRecordSet(
		dal.NewRecord(id).SetFields(data),
	))
}

func (self *Scanner) DeleteRecords(ids ...string) error {
	return self.db.Delete(self.Collection, ids)
}

func (self *Scanner) QueryRecords(filterString string) (*dal.RecordSet, error) {
	if index := self.db.WithSearch(); index != nil {
		return index.QueryString(self.Collection, filterString)
	} else {
		return nil, fmt.Errorf("Backend type %T does not support searching", self.db)
	}
}

// Removes records from the database that would not be added by the current Scanner instance.
func (self *Scanner) CleanRecords() error {
	var minLastSeen int64

	if v := self.PropertyGet(`metadata.last_scan`); v != nil {
		if vI, ok := v.(int64); ok {
			minLastSeen = vI
		}
	}

	if minLastSeen <= 0 {
		return fmt.Errorf("Invalid last_scan time")
	}

	if staleRecordSet, err := self.QueryRecords(fmt.Sprintf("last_seen/lt:%d", minLastSeen)); err == nil {
		ids := make([]string, 0)

		for _, record := range staleRecordSet.Records {
			ids = append(ids, record.ID)
		}

		log.Debugf("Cleaning up %d records", len(ids))

		return self.DeleteRecords(ids...)
	} else {
		return err
	}
}

func (self *Scanner) PropertySet(key string, value interface{}) error {
	return self.db.Insert(self.ManagementCollection, dal.NewRecordSet(
		dal.NewRecord(key).Set(`value`, value),
	))
}

func (self *Scanner) PropertyGet(key string, fallback ...interface{}) interface{} {
	if record, err := self.db.Retrieve(self.ManagementCollection, key); err == nil {
		return record.Get(`value`, fallback...)
	} else {
		return fallback[0]
	}
}

func (self *Scanner) Scan() error {
	// get this before performing the scan so that all touched files will necessarily
	// be greater than it
	minLastSeen := time.Now().UnixNano()

	for _, directory := range self.Directories {
		if err := directory.Scan(); err != nil {
			return err
		}
	}

	return self.PropertySet(`metadata.last_scan`, minLastSeen)
}
