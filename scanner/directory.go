package scanner

import (
	"fmt"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/imdario/mergo"
	"io/ioutil"
	"path"
	"regexp"
	"time"
)

type Directory struct {
	Path        string       `json:"path"`
	Label       string       `json:"label,omitempty"`
	Options     ScanOptions  `json:"options,omitempty"`
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
	if err := mergo.Merge(&self.Options, scanner.Options); err != nil {
		return err
	}

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

					log.Debugf("[%s] Scanning subdirectory %s", self.Label, subdirectory.Path)

					if err := subdirectory.Scan(); err == nil {
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

	// set the metadata.last_scan.<ID> property
	self.scanner.PropertySet(fmt.Sprintf("metadata.last_scan.%s", file.ID()), time.Now().UnixNano())

	// quick scan only tests for a files existence
	if self.Options.QuickScan && self.scanner.RecordExists(file.ID()) {
		return nil, nil
	}

	file.Metadata[`name`] = file.Name
	file.Metadata[`label`] = self.Label

	// load file metadata
	if err := file.LoadMetadata(); err != nil {
		return nil, err
	}

	// persist the file record
	if err := self.scanner.PersistRecord(file.ID(), file.Metadata); err != nil {
		return nil, err
	}

	return file, nil
}
