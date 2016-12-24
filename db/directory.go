package db

import (
	"fmt"
	"github.com/ghetzel/go-stockutil/stringutil"
	"io/ioutil"
	"path"
	"regexp"
	"strings"
	"time"
)

type Directory struct {
	Path                 string       `json:"path"`
	Label                string       `json:"label,omitempty"`
	RootPath             string       `json:"root_path,omitempty"`
	FilePattern          string       `json:"patterns,omitempty"`
	NoRecurseDirectories bool         `json:"no_recurse,omitempty"`
	FileMinimumSize      int          `json:"file_min_size,omitempty"`
	QuickScan            bool         `json:"quick_scan,omitempty"`
	Files                []*File      `json:"-"`
	Directories          []*Directory `json:"-"`
	db                   *Database
}

func NewDirectory(db *Database, path string) *Directory {
	return &Directory{
		Path:        path,
		Files:       make([]*File, 0),
		Directories: make([]*Directory, 0),
		db:          db,
	}
}

func (self *Directory) Initialize(db *Database) error {
	if self.Path == `` {
		return fmt.Errorf("Directory path must be specified.")
	}

	if self.Label == `` {
		self.Label = stringutil.Underscore(path.Base(self.Path))
	}

	if self.RootPath == `` {
		self.RootPath = self.Path
	}

	self.db = db

	return nil
}

func (self *Directory) Scan() error {
	if entries, err := ioutil.ReadDir(self.Path); err == nil {
		for _, entry := range entries {
			absPath := path.Join(self.Path, entry.Name())

			// recursive directory handling
			if entry.IsDir() {
				if !self.NoRecurseDirectories {
					if _, err := self.indexFile(absPath, true); err == nil {
						subdirectory := NewDirectory(self.db, absPath)

						subdirectory.Label = self.Label
						subdirectory.RootPath = self.RootPath
						subdirectory.FilePattern = self.FilePattern
						subdirectory.NoRecurseDirectories = self.NoRecurseDirectories
						subdirectory.FileMinimumSize = self.FileMinimumSize
						subdirectory.QuickScan = self.QuickScan

						log.Debugf("[%s] Scanning subdirectory %s", self.Label, subdirectory.Path)

						if err := subdirectory.Scan(); err == nil {
							self.Directories = append(self.Directories, subdirectory)
						} else {
							return err
						}
					} else {
						return err
					}
				}
			} else {
				// if we've specified a minimum file size, and this file is less than that,
				// then skip it
				if self.FileMinimumSize > 0 && entry.Size() < int64(self.FileMinimumSize) {
					continue
				}

				// scan the file as a sharable asset
				if file, err := self.indexFile(absPath, false); err == nil {
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

func (self *Directory) normalizeFileName(name string) string {
	prefix := strings.TrimSuffix(self.RootPath, `/`)
	name = strings.TrimPrefix(name, prefix)
	name = `/` + strings.TrimPrefix(name, `/`)

	return name
}

func (self *Directory) indexFile(name string, isDir bool) (*File, error) {
	if !isDir {
		// file pattern matching
		if self.FilePattern != `` {
			if rx, err := regexp.Compile(self.FilePattern); err == nil {
				if !rx.MatchString(name) {
					return nil, nil
				}
			} else {
				return nil, err
			}
		}
	}

	// get file implementation
	file := NewFile(name)

	// set the metadata.last_scan.<ID> property
	self.db.PropertySet(fmt.Sprintf("metadata.last_scan.%s", file.ID()), time.Now().UnixNano())

	// quick scan only tests for a files existence
	if self.QuickScan && self.db.RecordExists(file.ID()) {
		return nil, nil
	}

	file.Metadata[`name`] = self.normalizeFileName(file.Name)
	file.Metadata[`parent`] = path.Dir(self.normalizeFileName(file.Name))
	file.Metadata[`label`] = self.Label
	file.Metadata[`directory`] = isDir

	// load file metadata
	if err := file.LoadMetadata(); err != nil {
		return nil, err
	}

	// persist the file record
	if err := self.db.PersistRecord(file.ID(), file.Metadata); err != nil {
		return nil, err
	}

	return file, nil
}
