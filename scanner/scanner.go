package scanner

import (
	"fmt"
	"github.com/ghetzel/byteflood/scanner/metadata"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/op/go-logging"
	"io/ioutil"
	"path"
	"regexp"
	"strings"
)

var log = logging.MustGetLogger(`byteflood/scanner`)

type File struct {
	Name     string
	Metadata map[string]interface{}
}

type ScanOptions struct {
	FilePattern        string `json:"patterns,omitempty"`
	RecurseDirectories bool   `json:"recurse,omitempty"`
	FileMinimumSize    int    `json:"file_min_size,omitempty"`
}

func DefaultScanOptions() ScanOptions {
	return ScanOptions{
		RecurseDirectories: true,
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
			return err
		}
	}

	return nil
}

func (self *File) normalizeLoaderName(loader metadata.Loader) string {
	name := stringutil.Underscore(strings.Replace(fmt.Sprintf("%T", loader), `Loader`, ``, -1))
	return name
}

type Directory struct {
	Scanner     *Scanner
	Path        string       `json:"path"`
	Options     ScanOptions  `json:"options"`
	Files       []*File      `json:"-"`
	Directories []*Directory `json:"-"`
}

func NewDirectory(scanner *Scanner, path string, options ScanOptions) *Directory {
	return &Directory{
		Scanner:     scanner,
		Path:        path,
		Options:     options,
		Files:       make([]*File, 0),
		Directories: make([]*Directory, 0),
	}
}

func (self *Directory) Scan() error {
	if entries, err := ioutil.ReadDir(self.Path); err == nil {
		for _, entry := range entries {
			absPath := path.Join(self.Path, entry.Name())

			// recursive directory handling
			if entry.IsDir() {
				if self.Options.RecurseDirectories {
					subdirectory := NewDirectory(self.Scanner, absPath, self.Options)

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

	self.Files = append(self.Files, file)

	log.Debugf("Got file %+v", file)

	return file, nil
}

type Scanner struct {
	Directories []*Directory
}

func NewScanner() *Scanner {
	return &Scanner{
		Directories: make([]*Directory, 0),
	}
}

func (self *Scanner) AddDirectory(path string, options ScanOptions) {
	self.Directories = append(self.Directories, NewDirectory(self, path, options))
}

func (self *Scanner) PersistRecord(id string, data map[string]interface{}) error {
	return fmt.Errorf("PersistRecord: NI")
}

func (self *Scanner) Scan() error {
	for _, directory := range self.Directories {
		if err := directory.Scan(); err != nil {
			return err
		}
	}

	return nil
}
